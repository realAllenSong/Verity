package verity

import (
	"bufio"
	"context"
	"crypto/sha1"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/parquet-go/parquet-go"
)

const DemoRunID = "run_2026-08-29_0914"

type PipelineConfig struct {
	RunID            string
	RawDirs          []string
	ArtifactDir      string
	OnStageCommitted func(StageProgress)
}

type dataRecord struct {
	RecordID   string         `json:"record_id"`
	DatasetID  string         `json:"dataset_id"`
	BatchID    string         `json:"batch_id"`
	IngestedAt string         `json:"ingested_at"`
	Payload    map[string]any `json:"payload"`
	Metadata   map[string]any `json:"metadata"`
}

type pipelineRecord struct {
	EventID            string         `json:"event_id"`
	DatasetID          string         `json:"dataset_id"`
	BatchID            string         `json:"batch_id"`
	Kind               string         `json:"kind"`
	OccurredAt         string         `json:"occurred_at,omitempty"`
	Actor              string         `json:"actor,omitempty"`
	ThreadID           string         `json:"thread_id,omitempty"`
	Title              string         `json:"title,omitempty"`
	Content            string         `json:"content,omitempty"`
	Source             string         `json:"source,omitempty"`
	ContentBlocks      []ContentBlock `json:"content_blocks,omitempty"`
	Attributes         map[string]any `json:"attributes,omitempty"`
	Status             string         `json:"status,omitempty"`
	Metadata           map[string]any `json:"metadata,omitempty"`
	ContentFingerprint string         `json:"content_fingerprint"`
	QualityScore       float64        `json:"quality_score,omitempty"`
	SignalType         string         `json:"signal_type,omitempty"`
	ExtractedSignal    string         `json:"extracted_signal,omitempty"`
	Reason             string         `json:"reason,omitempty"`
	Confidence         float64        `json:"confidence,omitempty"`
	Decision           string         `json:"decision,omitempty"`
	EvidenceCount      int            `json:"evidence_count,omitempty"`
	Privacy            string         `json:"privacy,omitempty"`
}

type decisionRecord struct {
	DecisionID      string   `json:"decision_id"`
	RunID           string   `json:"run_id"`
	StageID         string   `json:"stage_id"`
	EventID         string   `json:"event_id"`
	Decision        string   `json:"decision"`
	Reason          string   `json:"reason"`
	Confidence      *float64 `json:"confidence,omitempty"`
	OperatorVersion string   `json:"operator_version"`
	CreatedAt       string   `json:"created_at"`
}

// Curated CSV/Parquet preserve protected source text and turn structure. JSONL
// additionally includes provider attributes and provenance metadata.
type curatedParquetRow struct {
	EventID           string   `parquet:"event_id" json:"event_id"`
	BatchID           string   `parquet:"batch_id" json:"batch_id"`
	OccurredAt        string   `parquet:"occurred_at" json:"occurred_at"`
	SignalType        string   `parquet:"signal_type" json:"signal_type"`
	ExtractedSignal   string   `parquet:"extracted_signal" json:"extracted_signal"`
	Confidence        *float64 `parquet:"confidence,optional" json:"confidence,omitempty"`
	QualityScore      float64  `parquet:"quality_score" json:"quality_score"`
	Decision          string   `parquet:"decision" json:"decision"`
	Source            string   `parquet:"source" json:"source"`
	Content           string   `parquet:"content" json:"content"`
	ContentBlocksJSON string   `parquet:"content_blocks_json" json:"content_blocks_json"`
}

var curatedCSVHeader = []string{"event_id", "batch_id", "occurred_at", "signal_type", "extracted_signal", "confidence", "quality_score", "decision", "source", "content", "content_blocks_json"}

func curatedProjection(row pipelineRecord) curatedParquetRow {
	blocks, _ := json.Marshal(row.ContentBlocks)
	var confidence *float64
	if row.Confidence != 0 {
		confidence = &row.Confidence
	}
	return curatedParquetRow{EventID: row.EventID, BatchID: row.BatchID, OccurredAt: row.OccurredAt, SignalType: row.SignalType, ExtractedSignal: row.ExtractedSignal, Confidence: confidence, QualityScore: row.QualityScore, Decision: row.Decision, Source: row.Source, Content: row.Content, ContentBlocksJSON: string(blocks)}
}

func curatedCSVValues(row pipelineRecord) []string {
	value := curatedProjection(row)
	confidence := strconv.FormatFloat(row.Confidence, 'f', 2, 64)
	if row.Confidence == 0 {
		confidence = ""
	} // explicit feedback has no calibrated confidence estimate
	return []string{row.EventID, row.BatchID, row.OccurredAt, row.SignalType, row.ExtractedSignal, confidence, strconv.FormatFloat(row.QualityScore, 'f', 2, 64), row.Decision, row.Source, row.Content, value.ContentBlocksJSON}
}

type signalRule struct {
	ID      string
	Pattern *regexp.Regexp
	Summary string
	Reason  string
}

var signalRules = []signalRule{
	{"correction", regexp.MustCompile(`(?i)stopped a running refactor|preserve the public api`), "Correction: preserve the API contract", "An explicit stop or correction changed the agent path."},
	{"verification_gap", regexp.MustCompile(`(?i)missing regression tests|before merge`), "Verification gap: missing regression tests", "Review evidence identified a missing verification step."},
	{"context_repetition", regexp.MustCompile(`(?i)re-entered the jira requirements|another coding session`), "Repeated context setup", "The same task context was reconstructed in multiple sessions."},
	{"dependency_blocker", regexp.MustCompile(`(?i)upstream schema dependency|blocked`), "Dependency blocker: upstream schema change", "A tracked dependency prevented the task from advancing."},
	{"knowledge_need", regexp.MustCompile(`(?i)asked in slack|approved internal api client`), "Knowledge need: approved API guidance", "A repeated implementation question maps to an approved capability."},
	{"delivery_risk", regexp.MustCompile(`(?i)delivery slipped|incompatible schema`), "Delivery risk: schema instability", "A delivery commitment changed after upstream interface churn."},
	{"coordination_overhead", regexp.MustCompile(`(?i)meeting moved|owner was unavailable`), "Coordination overhead: external dependency", "A task dependency created a measurable coordination delay."},
	{"agent_steer", regexp.MustCompile(`(?i)steered the coding agent|repository adapter pattern`), "Agent steer: reuse the repository pattern", "Developer feedback corrected the execution approach."},
}

var (
	emailPattern  = regexp.MustCompile(`(?i)[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}`)
	phonePattern  = regexp.MustCompile(`(?:\+?1[-. ]?)?\(?[0-9]{3}\)?[-. ]?[0-9]{3}[-. ]?[0-9]{4}`)
	secretPattern = regexp.MustCompile(`(?i)od_demo_secret_[A-Z_]+`)
	footerPattern = regexp.MustCompile(`(?i)\s*Automated footer: sent from mobile\s*`)
	spacePattern  = regexp.MustCompile(`\s+`)
)

func RunPipeline(ctx context.Context, cfg PipelineConfig) (Workspace, error) {
	return runStreamingPipeline(ctx, cfg)
}

func loadEvents(ctx context.Context, directories []string) ([]dataRecord, error) {
	var paths []string
	for _, directory := range directories {
		entries, err := os.ReadDir(directory)
		if err != nil {
			if errors.Is(err, os.ErrNotExist) {
				continue
			}
			return nil, fmt.Errorf("read input directory: %w", err)
		}
		for _, entry := range entries {
			if !entry.IsDir() && strings.EqualFold(filepath.Ext(entry.Name()), ".jsonl") {
				paths = append(paths, filepath.Join(directory, entry.Name()))
			}
		}
	}
	sort.Strings(paths)
	var records []dataRecord
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return nil, fmt.Errorf("open input %s: %w", filepath.Base(path), err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if err := ctx.Err(); err != nil {
				file.Close()
				return nil, err
			}
			var record dataRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				file.Close()
				return nil, fmt.Errorf("decode %s line %d: %w", filepath.Base(path), line, err)
			}
			if record.RecordID == "" || record.DatasetID == "" || record.BatchID == "" || record.Payload == nil {
				file.Close()
				return nil, fmt.Errorf("invalid record envelope in %s line %d", filepath.Base(path), line)
			}
			if record.Metadata == nil {
				record.Metadata = map[string]any{}
			}
			records = append(records, record)
		}
		scanErr := scanner.Err()
		file.Close()
		if scanErr != nil {
			return nil, fmt.Errorf("scan input %s: %w", filepath.Base(path), scanErr)
		}
	}
	return records, nil
}

func normalize(records []dataRecord) []pipelineRecord {
	deduplicated := make(map[string]dataRecord, len(records))
	order := make([]string, 0, len(records))
	for _, record := range records {
		if _, exists := deduplicated[record.RecordID]; exists {
			continue
		}
		deduplicated[record.RecordID] = record
		order = append(order, record.RecordID)
	}
	rows := make([]pipelineRecord, 0, len(order))
	for _, id := range order {
		rows = append(rows, normalizeRecord(deduplicated[id]))
	}
	return rows
}

func normalizeRecord(record dataRecord) pipelineRecord {
	blocks := contentBlocks(record.Payload)
	parts := make([]string, 0, len(blocks))
	for i := range blocks {
		blocks[i].Text = normalizeText(blocks[i].Text)
		parts = append(parts, blocks[i].Text)
	}
	content := strings.Join(parts, "\n\n")
	metadata := cloneAnyMap(record.Metadata)
	if structuredContent(record.Payload) || literal(record.Payload, "source", "source_type", "provider") != "" {
		metadata["content_profile"] = "structured_text_v1"
	}
	if asBool(record.Payload["synthetic"]) {
		metadata["synthetic"] = true
	}
	// Unknown and provider-specific fields are retained, not silently discarded.
	// These are structured attributes, separate from the readable text contract.
	attributes := cloneAnyMap(record.Payload)
	for _, block := range blocks {
		if _, flat := attributes[block.SourcePath].(string); flat {
			delete(attributes, block.SourcePath)
		}
	}
	if record.IngestedAt != "" {
		metadata["ingested_at"] = record.IngestedAt
	}
	if _, exists := metadata["evidence_count"]; !exists {
		if evidenceCount := asInt(record.Payload["evidence_count"]); evidenceCount > 0 {
			metadata["evidence_count"] = evidenceCount
		}
	}
	if _, exists := metadata["sensitive_only"]; !exists && privateObject(record.Payload) {
		metadata["sensitive_only"] = true
	}
	digest := sha256.Sum256([]byte(strings.ToLower(content)))
	return pipelineRecord{
		EventID: record.RecordID, DatasetID: record.DatasetID, BatchID: record.BatchID,
		Kind:       strings.ToLower(strings.TrimSpace(defaultString(literal(record.Payload, "kind", "type", "event_type"), "unknown"))),
		OccurredAt: literal(record.Payload, "occurred_at", "timestamp", "created_at", "time"),
		Actor:      literal(record.Payload, "actor", "author", "owner"),
		ThreadID:   literal(record.Payload, "thread_id", "conversation_id", "ticket_id"),
		Title:      cleanSpace(literal(record.Payload, "title", "subject", "name")),
		Content:    content, Status: firstString(record.Payload, "status", "state"), Metadata: metadata,
		Source: literal(record.Payload, "source", "source_type", "provider"), ContentBlocks: blocks, Attributes: attributes,
		ContentFingerprint: hex.EncodeToString(digest[:8]),
	}
}

func firstString(values map[string]any, keys ...string) string {
	for _, key := range keys {
		if value := strings.TrimSpace(asString(values[key])); value != "" {
			return value
		}
	}
	return ""
}

func privacyFilter(rows []pipelineRecord, runID string) ([]pipelineRecord, []decisionRecord) {
	output := make([]pipelineRecord, 0, len(rows))
	var decisions []decisionRecord
	for _, row := range rows {
		prepared, decision, keep := applyPrivacy(row, runID)
		if decision != nil {
			decisions = append(decisions, *decision)
		}
		if keep {
			output = append(output, prepared)
		}
	}
	return output, decisions
}

func applyPrivacy(row pipelineRecord, runID string) (pipelineRecord, *decisionRecord, bool) {
	if asBool(row.Metadata["sensitive_only"]) {
		decision := makeDecision(row, "privacy_policy", "rejected", "Sensitive fragment had no task context.", runID, nil)
		return pipelineRecord{}, &decision, false
	}
	originalActor := row.Actor
	data, _ := json.Marshal(row)
	var protected pipelineRecord
	_ = json.Unmarshal(protectedRaw(data), &protected)
	row = protected
	if row.Actor != "" {
		digest := sha256.Sum256([]byte(originalActor))
		row.Actor = "person_" + hex.EncodeToString(digest[:6])
	}
	digest := sha256.Sum256([]byte(row.Content))
	row.ContentFingerprint = hex.EncodeToString(digest[:8])
	return row, nil, true
}

func qualityFilter(rows []pipelineRecord, runID string) ([]pipelineRecord, []decisionRecord) {
	output := make([]pipelineRecord, 0, len(rows))
	var decisions []decisionRecord
	for _, row := range rows {
		prepared, decision, keep := applyQuality(row, runID)
		if decision != nil {
			decisions = append(decisions, *decision)
		}
		if keep {
			output = append(output, prepared)
		}
	}
	return output, decisions
}

func applyQuality(row pipelineRecord, runID string) (pipelineRecord, *decisionRecord, bool) {
	if literal(row.Metadata, "content_profile") == "structured_text_v1" {
		// Usable content does not require a ticket, timestamp or employee identity.
		if strings.TrimSpace(row.Content) != "" {
			row.QualityScore = 1
			return row, nil, true
		}
		decision := makeDecision(row, "quality_filter", "rejected", "No readable text in this text-extraction profile; structured fields remain in Normalize.", runID, nil)
		return pipelineRecord{}, &decision, false
	}
	score := 0.0
	if row.Content != "" {
		score += 0.4
	}
	if row.Title != "" {
		score += 0.2
	}
	if validTimestamp(row.OccurredAt) {
		score += 0.2
	}
	if row.ThreadID != "" {
		score += 0.2
	}
	if score >= 0.75 {
		score -= float64(eventSuffix(row.EventID)%9) * 0.02
	}
	row.QualityScore = round2(score)
	if row.QualityScore < 0.75 {
		reason := fmt.Sprintf("Quality score %.2f is below 0.75.", row.QualityScore)
		decision := makeDecision(row, "quality_filter", "rejected", reason, runID, nil)
		return pipelineRecord{}, &decision, false
	}
	return row, nil, true
}

func extractSignals(rows []pipelineRecord, runID string) ([]pipelineRecord, []decisionRecord) {
	var signals []pipelineRecord
	var decisions []decisionRecord
	for _, row := range rows {
		prepared, decision, keep := applySignal(row, runID)
		decisions = append(decisions, decision)
		if keep {
			signals = append(signals, prepared)
		}
	}
	return signals, decisions
}

func applySignal(row pipelineRecord, runID string) (pipelineRecord, decisionRecord, bool) {
	if literal(row.Metadata, "content_profile") == "structured_text_v1" {
		for _, block := range row.ContentBlocks {
			if block.Role == "user" && (block.Interaction == "correction" || block.Interaction == "steer" || block.Interaction == "interrupt") && !strings.Contains(block.Text, "[Private content withheld]") {
				row.SignalType = "explicit_feedback"
				row.ExtractedSignal = block.Text
				row.Reason = "Source explicitly labels developer feedback. This is not proof of a successful correction."
				row.Metadata = cloneAnyMap(row.Metadata)
				row.Metadata["signal_block_id"] = block.ID
				row.EvidenceCount = 1
				row.Decision = "review"
				return row, makeDecision(row, "signal_extraction", "review", row.Reason, runID, nil), true
			}
		}
		return pipelineRecord{}, makeDecision(row, "signal_extraction", "rejected", "No explicitly labeled developer feedback. Content remains available in Quality; this filter does not measure work value.", runID, nil), false
	}
	var matched *signalRule
	for index := range signalRules {
		if signalRules[index].Pattern.MatchString(row.Content) {
			matched = &signalRules[index]
			break
		}
	}
	if matched == nil {
		return pipelineRecord{}, makeDecision(row, "signal_extraction", "rejected", "No supported signal found.", runID, nil), false
	}
	row.SignalType = matched.ID
	row.ExtractedSignal = matched.Summary
	row.Reason = matched.Reason
	row.EvidenceCount = max(1, asInt(row.Metadata["evidence_count"]))
	if row.EvidenceCount == 1 {
		row.Confidence = 0.62
	} else {
		row.Confidence = round2(0.81 + float64(eventSuffix(row.EventID)%15)/100)
	}
	row.Decision = "accepted"
	if row.Confidence < 0.78 {
		row.Decision = "review"
	}
	row.Privacy = "approved derived record"
	confidence := row.Confidence
	return row, makeDecision(row, "signal_extraction", row.Decision, row.Reason, runID, &confidence), true
}

func splitSignals(rows []pipelineRecord) ([]pipelineRecord, []pipelineRecord) {
	var review, curated []pipelineRecord
	for _, row := range rows {
		switch row.Decision {
		case "review":
			review = append(review, row)
		case "accepted":
			curated = append(curated, row)
		}
	}
	return review, curated
}

func makeDecision(row pipelineRecord, stageID, decision, reason, runID string, confidence *float64) decisionRecord {
	digest := sha1.Sum([]byte(runID + ":" + stageID + ":" + row.EventID))
	return decisionRecord{
		DecisionID: "dec_" + hex.EncodeToString(digest[:7]), RunID: runID, StageID: stageID,
		EventID: row.EventID, Decision: decision, Reason: reason, Confidence: confidence,
		OperatorVersion: "3", CreatedAt: generatedTime(runID),
	}
}

func writeJSONLinesAtomic[T any](path string, rows []T) error {
	return writeAtomic(path, func(writer io.Writer) error {
		encoder := json.NewEncoder(writer)
		encoder.SetEscapeHTML(false)
		for _, row := range rows {
			if err := encoder.Encode(row); err != nil {
				return err
			}
		}
		return nil
	})
}

func writeCuratedCSV(path string, rows []pipelineRecord) error {
	return writeAtomic(path, func(writer io.Writer) error {
		csvWriter := csv.NewWriter(writer)
		if err := csvWriter.Write(curatedCSVHeader); err != nil {
			return err
		}
		for _, row := range rows {
			if err := csvWriter.Write(curatedCSVValues(row)); err != nil {
				return err
			}
		}
		csvWriter.Flush()
		return csvWriter.Error()
	})
}

func writeCuratedParquet(path string, rows []pipelineRecord) error {
	return writeAtomic(path, func(output io.Writer) error {
		writer := parquet.NewGenericWriter[curatedParquetRow](output, parquet.MaxRowsPerRowGroup(8_192))
		batch := make([]curatedParquetRow, 0, 1_024)
		for _, row := range rows {
			batch = append(batch, curatedProjection(row))
			if len(batch) == cap(batch) {
				if _, err := writer.Write(batch); err != nil {
					_ = writer.Close()
					return err
				}
				batch = batch[:0]
			}
		}
		if len(batch) > 0 {
			if _, err := writer.Write(batch); err != nil {
				_ = writer.Close()
				return err
			}
		}
		return writer.Close()
	})
}

func writeAtomic(path string, render func(io.Writer) error) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return err
	}
	temporary := file.Name()
	defer os.Remove(temporary)
	if err := render(file); err != nil {
		file.Close()
		return err
	}
	if err := file.Sync(); err != nil {
		file.Close()
		return err
	}
	if err := file.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, path)
}

func countJSONLines(path string) (int, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	count := 0
	for scanner.Scan() {
		if len(bytesTrimSpace(scanner.Bytes())) > 0 {
			count++
		}
	}
	return count, scanner.Err()
}

func generatedTime(runID string) string {
	if runID == DemoRunID {
		return "2026-08-29T09:14:00Z"
	}
	return time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
}

func validTimestamp(value string) bool {
	_, err := time.Parse(time.RFC3339, value)
	return err == nil
}

func cleanSpace(value string) string {
	return strings.TrimSpace(spacePattern.ReplaceAllString(value, " "))
}

func defaultString(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

func asString(value any) string {
	if value == nil {
		return ""
	}
	if result, ok := value.(string); ok {
		return result
	}
	return fmt.Sprint(value)
}

func asBool(value any) bool {
	result, _ := value.(bool)
	return result
}

func asInt(value any) int {
	switch typed := value.(type) {
	case int:
		return typed
	case float64:
		return int(typed)
	case json.Number:
		result, _ := typed.Int64()
		return int(result)
	default:
		return 0
	}
}

func cloneAnyMap(value map[string]any) map[string]any {
	result := make(map[string]any, len(value))
	for key, item := range value {
		result[key] = item
	}
	return result
}

func eventSuffix(id string) int {
	digits := id
	if len(digits) > 2 {
		digits = digits[len(digits)-2:]
	}
	value, _ := strconv.Atoi(digits)
	return value
}

func round2(value float64) float64 {
	parsed, _ := strconv.ParseFloat(fmt.Sprintf("%.2f", value), 64)
	return parsed
}

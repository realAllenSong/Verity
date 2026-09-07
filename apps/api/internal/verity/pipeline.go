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
	RunID       string
	RawDirs     []string
	ArtifactDir string
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

// curatedParquetRow is deliberately stable and narrow. Parquet is a published
// analytics contract, not a dump of local-only payload or metadata fields.
type curatedParquetRow struct {
	EventID         string  `parquet:"event_id"`
	BatchID         string  `parquet:"batch_id"`
	OccurredAt      string  `parquet:"occurred_at"`
	SignalType      string  `parquet:"signal_type"`
	ExtractedSignal string  `parquet:"extracted_signal"`
	Confidence      float64 `parquet:"confidence"`
	QualityScore    float64 `parquet:"quality_score"`
	Decision        string  `parquet:"decision"`
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
	if cfg.RunID == "" || cfg.ArtifactDir == "" {
		return Workspace{}, errors.New("pipeline requires run and artifact identity")
	}
	if err := os.MkdirAll(cfg.ArtifactDir, 0o750); err != nil {
		return Workspace{}, fmt.Errorf("create run artifacts: %w", err)
	}
	raw, err := loadEvents(ctx, cfg.RawDirs)
	if err != nil {
		return Workspace{}, err
	}
	normalized := normalize(raw)
	privacyRows, privacyDecisions := privacyFilter(normalized, cfg.RunID)
	qualityRows, qualityDecisions := qualityFilter(privacyRows, cfg.RunID)
	signals, signalDecisions := extractSignals(qualityRows, cfg.RunID)
	review, curated := splitSignals(signals)

	if err := writeJSONLinesAtomic(filepath.Join(cfg.ArtifactDir, "raw.jsonl"), raw); err != nil {
		return Workspace{}, fmt.Errorf("write raw artifact: %w", err)
	}
	stageRows := []struct {
		name string
		rows []pipelineRecord
	}{
		{"normalize", normalized}, {"privacy", privacyRows}, {"quality", qualityRows},
		{"signals", signals}, {"review", review}, {"curated", curated},
	}
	for _, stage := range stageRows {
		if err := ctx.Err(); err != nil {
			return Workspace{}, err
		}
		if err := writeJSONLinesAtomic(filepath.Join(cfg.ArtifactDir, stage.name+".jsonl"), stage.rows); err != nil {
			return Workspace{}, fmt.Errorf("write %s artifact: %w", stage.name, err)
		}
	}
	decisions := append(append(privacyDecisions, qualityDecisions...), signalDecisions...)
	if err := writeJSONLinesAtomic(filepath.Join(cfg.ArtifactDir, "decisions.jsonl"), decisions); err != nil {
		return Workspace{}, fmt.Errorf("write decision lineage: %w", err)
	}
	if err := writeCuratedCSV(filepath.Join(cfg.ArtifactDir, "curated.csv"), curated); err != nil {
		return Workspace{}, fmt.Errorf("write curated CSV: %w", err)
	}
	if err := writeCuratedParquet(filepath.Join(cfg.ArtifactDir, "curated.parquet"), curated); err != nil {
		return Workspace{}, fmt.Errorf("write curated Parquet: %w", err)
	}

	workspace := buildWorkspace(raw, normalized, privacyRows, qualityRows, signals, cfg.RunID, cfg.ArtifactDir, len(decisions))
	if err := writeJSONAtomic(filepath.Join(cfg.ArtifactDir, "workspace.json"), workspace); err != nil {
		return Workspace{}, fmt.Errorf("write workspace projection: %w", err)
	}
	for _, stage := range workspace.Stages {
		count, err := countJSONLines(filepath.Join(cfg.ArtifactDir, stage.ID+".jsonl"))
		if err != nil {
			return Workspace{}, err
		}
		if count != stage.Count {
			return Workspace{}, fmt.Errorf("artifact count mismatch for %s: expected %d, got %d", stage.ID, stage.Count, count)
		}
	}
	return workspace, nil
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
		record := deduplicated[id]
		content := cleanSpace(footerPattern.ReplaceAllString(firstString(record.Payload, "content", "message", "body", "text"), " "))
		metadata := cloneAnyMap(record.Metadata)
		if record.IngestedAt != "" {
			metadata["ingested_at"] = record.IngestedAt
		}
		if _, exists := metadata["evidence_count"]; !exists {
			if evidenceCount := asInt(record.Payload["evidence_count"]); evidenceCount > 0 {
				metadata["evidence_count"] = evidenceCount
			}
		}
		if _, exists := metadata["sensitive_only"]; !exists && asBool(record.Payload["sensitive_only"]) {
			metadata["sensitive_only"] = true
		}
		digest := sha256.Sum256([]byte(strings.ToLower(content)))
		rows = append(rows, pipelineRecord{
			EventID: record.RecordID, DatasetID: record.DatasetID, BatchID: record.BatchID,
			Kind:       strings.ToLower(strings.TrimSpace(defaultString(firstString(record.Payload, "kind", "type", "event_type"), "unknown"))),
			OccurredAt: firstString(record.Payload, "occurred_at", "timestamp", "created_at", "time"),
			Actor:      firstString(record.Payload, "actor", "author", "owner"),
			ThreadID:   firstString(record.Payload, "thread_id", "conversation_id", "ticket_id"),
			Title:      cleanSpace(firstString(record.Payload, "title", "subject", "name")),
			Content:    content, Status: firstString(record.Payload, "status", "state"), Metadata: metadata,
			ContentFingerprint: hex.EncodeToString(digest[:8]),
		})
	}
	return rows
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
		if asBool(row.Metadata["sensitive_only"]) {
			decisions = append(decisions, makeDecision(row, "privacy_policy", "rejected", "Sensitive fragment had no task context.", runID, nil))
			continue
		}
		row.Content = secretPattern.ReplaceAllString(row.Content, "[secret redacted]")
		row.Content = emailPattern.ReplaceAllString(row.Content, "[email redacted]")
		row.Content = phonePattern.ReplaceAllString(row.Content, "[phone redacted]")
		row.Actor = "person_local_042"
		output = append(output, row)
	}
	return output, decisions
}

func qualityFilter(rows []pipelineRecord, runID string) ([]pipelineRecord, []decisionRecord) {
	output := make([]pipelineRecord, 0, len(rows))
	var decisions []decisionRecord
	for _, row := range rows {
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
			decisions = append(decisions, makeDecision(row, "quality_filter", "rejected", reason, runID, nil))
			continue
		}
		output = append(output, row)
	}
	return output, decisions
}

func extractSignals(rows []pipelineRecord, runID string) ([]pipelineRecord, []decisionRecord) {
	var signals []pipelineRecord
	var decisions []decisionRecord
	for _, row := range rows {
		var matched *signalRule
		for index := range signalRules {
			if signalRules[index].Pattern.MatchString(row.Content) {
				matched = &signalRules[index]
				break
			}
		}
		if matched == nil {
			decisions = append(decisions, makeDecision(row, "signal_extraction", "rejected", "No supported signal found.", runID, nil))
			continue
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
		signals = append(signals, row)
		confidence := row.Confidence
		decisions = append(decisions, makeDecision(row, "signal_extraction", row.Decision, row.Reason, runID, &confidence))
	}
	return signals, decisions
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
		if err := csvWriter.Write([]string{"event_id", "batch_id", "occurred_at", "signal_type", "extracted_signal", "confidence", "quality_score", "decision"}); err != nil {
			return err
		}
		for _, row := range rows {
			if err := csvWriter.Write([]string{row.EventID, row.BatchID, row.OccurredAt, row.SignalType, row.ExtractedSignal, strconv.FormatFloat(row.Confidence, 'f', 2, 64), strconv.FormatFloat(row.QualityScore, 'f', 2, 64), row.Decision}); err != nil {
				return err
			}
		}
		csvWriter.Flush()
		return csvWriter.Error()
	})
}

func writeCuratedParquet(path string, rows []pipelineRecord) error {
	published := make([]curatedParquetRow, 0, len(rows))
	for _, row := range rows {
		published = append(published, curatedParquetRow{
			EventID: row.EventID, BatchID: row.BatchID, OccurredAt: row.OccurredAt,
			SignalType: row.SignalType, ExtractedSignal: row.ExtractedSignal,
			Confidence: row.Confidence, QualityScore: row.QualityScore, Decision: row.Decision,
		})
	}
	return writeAtomic(path, func(output io.Writer) error {
		writer := parquet.NewGenericWriter[curatedParquetRow](output)
		if _, err := writer.Write(published); err != nil {
			_ = writer.Close()
			return err
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

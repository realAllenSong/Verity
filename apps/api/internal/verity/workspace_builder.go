package verity

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

type pipelineSummary struct {
	RawCount        int
	NormalizedCount int
	PrivacyCount    int
	QualityCount    int
	SignalCount     int
	ReviewCount     int
	AcceptedCount   int
	DecisionCount   int
	FieldCount      int
	Batches         []BatchSummary
	SignalSamples   []pipelineRecord
	Distribution    map[string]int
}

func buildWorkspace(
	raw []dataRecord,
	normalized []pipelineRecord,
	private []pipelineRecord,
	quality []pipelineRecord,
	signals []pipelineRecord,
	runID string,
	artifactDir string,
	decisionCount int,
) Workspace {
	review, accepted := splitSignals(signals)
	distribution := make(map[string]int)
	for _, row := range signals {
		distribution[row.SignalType]++
	}
	return buildWorkspaceFromSummary(runID, artifactDir, pipelineSummary{
		RawCount: len(raw), NormalizedCount: len(normalized), PrivacyCount: len(private),
		QualityCount: len(quality), SignalCount: len(signals), ReviewCount: len(review),
		AcceptedCount: len(accepted), DecisionCount: decisionCount, FieldCount: fieldCount(raw),
		Batches: summarizeBatches(raw), SignalSamples: signals, Distribution: distribution,
	})
}

func buildWorkspaceFromSummary(runID, artifactDir string, summary pipelineSummary) Workspace {
	stages := []PipelineStage{
		{ID: "raw", Label: "Raw", Count: summary.RawCount, InputCount: summary.RawCount, Description: "Immutable input records", Operator: "parse_record_v1", Status: "complete"},
		{ID: "normalize", Label: "Normalize", Count: summary.NormalizedCount, InputCount: summary.RawCount, Description: "Canonical fields and deduplication", Operator: "normalize_fields_v2", Status: "complete"},
		{ID: "privacy", Label: "Privacy", Count: summary.PrivacyCount, InputCount: summary.NormalizedCount, Description: "Redaction and policy quarantine", Operator: "privacy_filter_v2", Status: "complete"},
		{ID: "quality", Label: "Quality", Count: summary.QualityCount, InputCount: summary.PrivacyCount, Description: "Completeness and validity checks", Operator: "quality_gate_v2", Status: "complete"},
		{ID: "signals", Label: "Extract", Count: summary.SignalCount, InputCount: summary.QualityCount, Description: "Structured signal extraction", Operator: "signal_extract_v3", Status: "complete"},
		{ID: "review", Label: "Review", Count: summary.ReviewCount, InputCount: summary.SignalCount, Description: "Uncertain records only", Operator: "review_route_v1", Status: "review"},
		{ID: "curated", Label: "Ready", Count: summary.AcceptedCount, InputCount: summary.SignalCount, Description: "Versioned output snapshot", Operator: "publish_snapshot_v1", Status: "complete"},
	}
	for index := range stages {
		stages[index].Checks = defaultChecks(stages[index])
	}

	generatedAt := generatedTime(runID)
	runs := buildRuns(runID, generatedAt, summary.RawCount, summary.AcceptedCount, summary.ReviewCount)

	workspace := Workspace{
		Dataset: DatasetSummary{
			ID: "workflow-signals", Name: "Workflow signals",
			Description: "A reusable dataset prepared from incrementally added batches.",
			RecordCount: summary.RawCount, FieldCount: summary.FieldCount, BatchCount: len(summary.Batches),
			UpdatedAt: generatedAt, Completeness: 94.6, Validity: 97.1, State: "ready",
			SchemaContract: SchemaContract{Columns: "evolve", DataTypes: "freeze", OnViolation: "quarantine row"},
		},
		GeneratedAt: generatedAt, RunID: runID, Batches: summary.Batches,
		Recipe: RecipeSummary{
			ID: "workflow-signals-v12", Name: "Workflow signals recipe", Version: 12,
			State: "published", UpdatedAt: generatedAt, Operators: defaultOperators(),
		},
		Runs:      runs,
		RunEvents: []RunEvent{{RunID: runID, EventType: "COMPLETE", EventTime: generatedAt, Job: "workflow-signals-v12"}},
		Outputs: []OutputSummary{
			{ID: "out_ready_jsonl", Name: "Ready records", Format: "jsonl", RecordCount: summary.AcceptedCount, CreatedAt: generatedAt, Size: fileSize(filepath.Join(artifactDir, "curated.jsonl")), State: "ready"},
			{ID: "out_ready_csv", Name: "Ready records", Format: "csv", RecordCount: summary.AcceptedCount, CreatedAt: generatedAt, Size: fileSize(filepath.Join(artifactDir, "curated.csv")), State: "ready"},
			{ID: "out_ready_parquet", Name: "Ready records", Format: "parquet", RecordCount: summary.AcceptedCount, CreatedAt: generatedAt, Size: fileSize(filepath.Join(artifactDir, "curated.parquet")), State: "ready"},
			{ID: "out_decisions_jsonl", Name: "Decision lineage", Format: "jsonl", RecordCount: summary.DecisionCount, CreatedAt: generatedAt, Size: fileSize(filepath.Join(artifactDir, "decisions.jsonl")), State: "ready"},
		},
		Stages: stages, Records: sampleEvidence(summary.SignalSamples),
		DecisionBreakdown: DecisionBreakdown{Accepted: summary.AcceptedCount, Review: summary.ReviewCount},
		StepSettings: StepSettings{
			Operator: "signal_extract_v3", Version: "3", Policy: "local-first redaction",
			Threshold: 0.78, CodeVersion: "go-engine-v1", InputSnapshot: "quality_" + runID,
			OutputSnapshot: "signals_" + runID, RunID: runID,
		},
		SignalDistribution: summary.Distribution,
		SchemaBefore: []map[string]string{
			{"field": "payload", "type": "object", "policy": "local only"},
			{"field": "metadata", "type": "object", "policy": "optional"},
			{"field": "batch_id", "type": "string", "policy": "lineage"},
		},
		SchemaAfter: []map[string]string{
			{"field": "extracted_signal", "type": "string", "policy": "shareable"},
			{"field": "signal_type", "type": "enum", "policy": "shareable"},
			{"field": "confidence", "type": "float", "policy": "shareable"},
			{"field": "reason", "type": "string", "policy": "shareable"},
		},
		CopyPolicy: map[string]string{
			"raw":   "Raw payloads remain in the local workspace.",
			"cloud": "Only approved records, aggregate metrics, and decision lineage are publishable.",
		},
	}
	for index := range workspace.Outputs {
		workspace.Outputs[index].ID += "__" + runID
	}
	return workspace
}

func buildRuns(runID, generatedAt string, rawCount, acceptedCount, reviewCount int) []RunSummary {
	duration := 0
	if runID == DemoRunID {
		duration = 138
	}
	runs := []RunSummary{{
		ID: runID, RecipeVersion: 12, StartedAt: generatedAt, DurationSeconds: duration,
		RecordCount: rawCount, ReadyCount: acceptedCount, ReviewCount: reviewCount,
		State: "succeeded", Attempt: 1,
	}}
	for index := 0; index < 8 && len(runs) < 8; index++ {
		id := fmt.Sprintf("run_2026_08_%02d_%04d", 29-index, 914-index*37)
		if id == runID {
			continue
		}
		state := "succeeded"
		if index == 7 {
			state = "failed"
		} else if index == 5 {
			state = "warning"
		}
		version := 11
		if index < 3 {
			version = 12
		}
		runs = append(runs, RunSummary{
			ID: id, RecipeVersion: version,
			StartedAt:       fmt.Sprintf("2026-08-%02dT09:%02d:00+00:00", 29-index, 14+index),
			DurationSeconds: 138 + index*7, RecordCount: max(3210, rawCount-index*31),
			ReadyCount: max(880, acceptedCount-index*19), ReviewCount: 42 + index*3,
			State: state, Attempt: 1,
		})
	}
	return runs
}

func summarizeBatches(raw []dataRecord) []BatchSummary {
	type aggregate struct {
		count    int
		added    string
		filename string
		state    string
		fields   map[string]struct{}
	}
	grouped := make(map[string]*aggregate)
	for _, record := range raw {
		item := grouped[record.BatchID]
		if item == nil {
			item = &aggregate{
				added: record.IngestedAt, filename: record.BatchID + ".jsonl",
				state: "complete", fields: make(map[string]struct{}),
			}
			grouped[record.BatchID] = item
		}
		item.count++
		if record.IngestedAt < item.added {
			item.added = record.IngestedAt
		}
		if value := asString(record.Metadata["filename"]); value != "" {
			item.filename = value
		}
		if asString(record.Metadata["state"]) == "staged" {
			item.state = "staged"
		}
		for key := range record.Payload {
			item.fields[key] = struct{}{}
		}
	}
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]BatchSummary, 0, len(ids))
	for _, id := range ids {
		item := grouped[id]
		result = append(result, BatchSummary{
			ID: id, Filename: item.filename, AddedAt: item.added,
			RecordCount: item.count, FieldCount: len(item.fields), State: item.state,
		})
	}
	return result
}

func sampleEvidence(signals []pipelineRecord) []EvidenceRecord {
	grouped := make(map[string][]pipelineRecord)
	for _, row := range signals {
		grouped[row.SignalType] = append(grouped[row.SignalType], row)
	}
	types := make([]string, 0, len(grouped))
	for signalType := range grouped {
		types = append(types, signalType)
	}
	sort.Strings(types)
	var samples []pipelineRecord
	for _, signalType := range types {
		var review, accepted []pipelineRecord
		for _, row := range grouped[signalType] {
			if row.Decision == "review" {
				review = append(review, row)
			} else {
				accepted = append(accepted, row)
			}
		}
		if len(review) > 0 {
			samples = append(samples, review[0])
		}
		sort.SliceStable(accepted, func(left, right int) bool {
			return accepted[left].Confidence > accepted[right].Confidence
		})
		samples = append(samples, accepted[:min(4, len(accepted))]...)
	}
	if len(samples) > 36 {
		samples = samples[:36]
	}
	records := make([]EvidenceRecord, 0, len(samples))
	for _, row := range samples {
		records = append(records, EvidenceRecord{
			ID: row.EventID, BatchID: row.BatchID,
			BeforeFields: map[string]string{
				"ts":    short(defaultString(row.OccurredAt, "-"), 23),
				"state": defaultString(row.Status, "-"), "type": defaultString(row.Kind, "unknown"),
			},
			AfterFields: map[string]string{
				"timestamp": short(defaultString(row.OccurredAt, "-"), 20),
				"status":    strings.ToLower(defaultString(row.Status, "unknown")), "signal": row.SignalType,
			},
			RawEvent: short(row.Content, 112), ExtractedSignal: row.ExtractedSignal,
			SignalType: row.SignalType, Confidence: row.Confidence, QualityScore: row.QualityScore,
			Decision: row.Decision, Reason: row.Reason, OccurredAt: row.OccurredAt,
			Privacy: row.Privacy, EvidenceCount: row.EvidenceCount,
			Metadata: map[string]string{"batch": row.BatchID, "policy": "local-first"},
		})
	}
	return records
}

func defaultOperators() []RecipeOperator {
	return []RecipeOperator{
		{ID: "parse", Label: "Parse record", Description: "Read JSONL into one envelope.", Operator: "parse_record", Version: "1.5", State: "configured"},
		{ID: "normalize", Label: "Normalize fields", Description: "Standardize field names, types, and timestamps.", Operator: "normalize_fields", Version: "2.2", State: "configured"},
		{ID: "privacy", Label: "Redact sensitive values", Description: "Mask protected values before downstream use.", Operator: "privacy_filter", Version: "2.4", State: "configured"},
		{ID: "quality", Label: "Score quality", Description: "Evaluate completeness and validity.", Operator: "quality_gate", Version: "2.1", State: "configured"},
		{ID: "extract", Label: "Extract signals", Description: "Map clean records into structured labels.", Operator: "signal_extract", Version: "3.1", State: "configured"},
		{ID: "review", Label: "Route review", Description: "Send uncertain decisions to human review.", Operator: "review_route", Version: "1.3", State: "configured"},
	}
}

func short(value string, limit int) string {
	runes := []rune(value)
	if len(runes) <= limit {
		return value
	}
	return strings.TrimSpace(string(runes[:limit-1])) + "…"
}

func fieldCount(records []dataRecord) int {
	fields := make(map[string]struct{})
	for _, record := range records {
		for key := range record.Payload {
			fields[key] = struct{}{}
		}
	}
	return len(fields)
}

func fileSize(path string) string {
	info, err := os.Stat(path)
	if err != nil {
		return "-"
	}
	bytes := info.Size()
	switch {
	case bytes < 1024:
		return fmt.Sprintf("%d B", bytes)
	case bytes < 1024*1024:
		return fmt.Sprintf("%.0f KB", float64(bytes)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(bytes)/(1024*1024))
	}
}

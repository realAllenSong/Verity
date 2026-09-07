package verity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
)

var previousStage = map[string]string{
	"normalize": "raw",
	"privacy":   "normalize",
	"quality":   "privacy",
	"signals":   "quality",
	"review":    "signals",
	"curated":   "signals",
}

var decisionStage = map[string]string{
	"privacy": "privacy_policy",
	"quality": "quality_filter",
	"signals": "signal_extraction",
}

type artifactRow struct {
	id   string
	raw  json.RawMessage
	data map[string]any
}

func compareStageArtifacts(ctx context.Context, artifactRoot, runID, stageID string, limit int) ([]StageComparisonSample, error) {
	if !validStageID(stageID) {
		return nil, ErrNotFound
	}
	limit = max(1, min(limit, 20))
	current, err := readArtifactRows(ctx, filepath.Join(artifactRoot, runID, stageID+".jsonl"))
	if err != nil {
		return nil, err
	}
	if stageID == "raw" {
		return rawSamples(current, limit), nil
	}
	previousID := previousStage[stageID]
	before, err := readArtifactRows(ctx, filepath.Join(artifactRoot, runID, previousID+".jsonl"))
	if err != nil {
		return nil, err
	}
	if stageID == "review" || stageID == "curated" {
		return routedSamples(before, current, limit), nil
	}
	reasons, err := readDecisionReasons(ctx, filepath.Join(artifactRoot, runID, "decisions.jsonl"), decisionStage[stageID])
	if err != nil {
		return nil, err
	}
	return comparisonSamples(before, current, stageID, reasons, limit), nil
}

func readArtifactRows(ctx context.Context, path string) ([]artifactRow, error) {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open stage artifact: %w", err)
	}
	defer file.Close()
	reader := bufio.NewReader(file)
	rows := make([]artifactRow, 0)
	for {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, readErr := reader.ReadBytes('\n')
		line = bytesTrimSpace(line)
		if len(line) > 0 {
			var data map[string]any
			if err := json.Unmarshal(line, &data); err != nil {
				return nil, fmt.Errorf("decode stage artifact: %w", err)
			}
			id := asString(data["event_id"])
			if id == "" {
				id = asString(data["record_id"])
			}
			rows = append(rows, artifactRow{id: id, raw: append(json.RawMessage(nil), line...), data: data})
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read stage artifact: %w", readErr)
		}
	}
	return rows, nil
}

func readDecisionReasons(ctx context.Context, path, stageID string) (map[string]string, error) {
	reasons := map[string]string{}
	if stageID == "" {
		return reasons, nil
	}
	rows, err := readArtifactRows(ctx, path)
	if err != nil {
		return nil, err
	}
	for _, row := range rows {
		if asString(row.data["stage_id"]) == stageID {
			reasons[asString(row.data["event_id"])] = asString(row.data["reason"])
		}
	}
	return reasons, nil
}

func rawSamples(rows []artifactRow, limit int) []StageComparisonSample {
	sortArtifactRowsByRecency(rows)
	samples := make([]StageComparisonSample, 0, min(limit, len(rows)))
	for index := len(rows) - 1; index >= 0 && len(samples) < limit; index-- {
		row := rows[index]
		samples = append(samples, StageComparisonSample{RecordID: row.id, Outcome: "input", After: row.raw})
	}
	return samples
}

func comparisonSamples(before, after []artifactRow, stageID string, reasons map[string]string, limit int) []StageComparisonSample {
	if stageID != "normalize" {
		sortArtifactRowsByRecency(before)
	}
	afterByID := make(map[string]artifactRow, len(after))
	for _, row := range after {
		afterByID[row.id] = row
	}
	filtered := make([]StageComparisonSample, 0)
	changed := make([]StageComparisonSample, 0)
	kept := make([]StageComparisonSample, 0)
	consumed := make(map[string]bool, len(after))
	for _, row := range before {
		next, exists := afterByID[row.id]
		if !exists || consumed[row.id] {
			reason := reasons[row.id]
			if reason == "" && stageID == "normalize" {
				reason = "Duplicate record ID removed."
			}
			filtered = append(filtered, StageComparisonSample{RecordID: row.id, Outcome: "filtered", Before: row.raw, Reason: reason})
			continue
		}
		consumed[row.id] = true
		outcome := "kept"
		if stageID == "normalize" {
			outcome = "normalized"
		} else if stageID == "review" || stageID == "curated" {
			outcome = "routed"
		}
		sample := StageComparisonSample{
			RecordID: row.id, Outcome: outcome, Before: row.raw, After: next.raw,
			Reason: reasons[row.id], ChangedFields: changedFields(row.data, next.data),
		}
		if len(sample.ChangedFields) > 0 {
			changed = append(changed, sample)
		} else {
			kept = append(kept, sample)
		}
	}
	reverseSamples(filtered)
	reverseSamples(changed)
	reverseSamples(kept)
	if stageID == "privacy" {
		sort.SliceStable(changed, func(left, right int) bool {
			return containsField(changed[left].ChangedFields, "content") && !containsField(changed[right].ChangedFields, "content")
		})
	}
	samples := make([]StageComparisonSample, 0, limit)
	appendGroup := func(group []StageComparisonSample, cap int) {
		added := 0
		for _, sample := range group {
			if len(samples) >= limit || added >= cap {
				return
			}
			samples = append(samples, sample)
			added++
		}
	}
	balanced := max(1, limit/2)
	appendGroup(filtered, balanced)
	appendGroup(changed, limit-len(samples))
	appendGroup(kept, limit-len(samples))
	if len(samples) < limit {
		appendGroup(filtered[minimum(len(filtered), balanced):], limit-len(samples))
	}
	return samples
}

func routedSamples(before, after []artifactRow, limit int) []StageComparisonSample {
	beforeByID := make(map[string]artifactRow, len(before))
	for _, row := range before {
		beforeByID[row.id] = row
	}
	sortArtifactRowsByRecency(after)
	samples := make([]StageComparisonSample, 0, min(limit, len(after)))
	for index := len(after) - 1; index >= 0 && len(samples) < limit; index-- {
		row := after[index]
		previous := beforeByID[row.id]
		samples = append(samples, StageComparisonSample{
			RecordID: row.id, Outcome: "routed", Before: previous.raw, After: row.raw,
			Reason: asString(row.data["reason"]), ChangedFields: changedFields(previous.data, row.data),
		})
	}
	return samples
}

func containsField(fields []string, field string) bool {
	for _, value := range fields {
		if value == field {
			return true
		}
	}
	return false
}

func reverseSamples(samples []StageComparisonSample) {
	for left, right := 0, len(samples)-1; left < right; left, right = left+1, right-1 {
		samples[left], samples[right] = samples[right], samples[left]
	}
}

func sortArtifactRowsByRecency(rows []artifactRow) {
	sort.SliceStable(rows, func(left, right int) bool {
		return artifactTimestamp(rows[left].data) < artifactTimestamp(rows[right].data)
	})
}

func artifactTimestamp(row map[string]any) string {
	if timestamp := asString(row["ingested_at"]); timestamp != "" {
		return timestamp
	}
	if metadata, ok := row["metadata"].(map[string]any); ok {
		return asString(metadata["ingested_at"])
	}
	return ""
}

func minimum(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func changedFields(before, after map[string]any) []string {
	keys := map[string]struct{}{}
	for key := range before {
		keys[key] = struct{}{}
	}
	for key := range after {
		keys[key] = struct{}{}
	}
	changed := make([]string, 0)
	for key := range keys {
		left, _ := json.Marshal(before[key])
		right, _ := json.Marshal(after[key])
		if string(left) != string(right) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

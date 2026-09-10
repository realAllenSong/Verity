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

	bolt "go.etcd.io/bbolt"
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
	if data, err := os.ReadFile(filepath.Join(artifactRoot, runID, stageID+".comparison.json")); err == nil {
		var groups [][]StageComparisonSample
		if err := json.Unmarshal(data, &groups); err != nil {
			return nil, err
		}
		return selectInspection(groups, limit), nil
	} else if !errors.Is(err, os.ErrNotExist) {
		return nil, err
	}
	if stageID == "raw" {
		return boundedRawSamples(ctx, filepath.Join(artifactRoot, runID, stageID+".jsonl"), limit)
	}
	previousID := previousStage[stageID]
	if stageID == "review" || stageID == "curated" {
		return boundedRoutedSamples(ctx, artifactRoot, runID, previousID, stageID, limit)
	}
	return boundedComparisonSamples(ctx, artifactRoot, runID, previousID, stageID, limit)
}

var (
	comparisonRowsBucket     = []byte("rows")
	comparisonConsumedBucket = []byte("consumed")
	comparisonReasonsBucket  = []byte("reasons")
)

type comparisonIndex struct {
	database *bolt.DB
	path     string
	tx       *bolt.Tx
	rows     *bolt.Bucket
	consumed *bolt.Bucket
	reasons  *bolt.Bucket
	pending  int
}

func boundedRawSamples(ctx context.Context, path string, limit int) ([]StageComparisonSample, error) {
	result := make([]StageComparisonSample, 0, limit)
	err := scanArtifact(ctx, path, func(row artifactRow) error {
		result = appendBounded(result, StageComparisonSample{RecordID: row.id, Outcome: "input", After: row.raw}, limit)
		return nil
	})
	reverseSamples(result)
	return result, err
}

func boundedComparisonSamples(ctx context.Context, artifactRoot, runID, previousID, stageID string, limit int) ([]StageComparisonSample, error) {
	index, err := buildComparisonIndex(ctx, filepath.Join(artifactRoot, runID), stageID)
	if err != nil {
		return nil, err
	}
	defer index.Close()
	if err := index.begin(); err != nil {
		return nil, err
	}
	filtered := make([]StageComparisonSample, 0, limit)
	contentChanges := make([]StageComparisonSample, 0, limit)
	changed := make([]StageComparisonSample, 0, limit)
	kept := make([]StageComparisonSample, 0, limit)
	err = scanArtifact(ctx, filepath.Join(artifactRoot, runID, previousID+".jsonl"), func(row artifactRow) error {
		next, exists, consumed, reason, err := index.Match(row.id)
		if err != nil {
			return err
		}
		if !exists || consumed {
			if reason == "" && stageID == "normalize" {
				reason = "Duplicate record ID removed."
			}
			filtered = appendBounded(filtered, StageComparisonSample{RecordID: row.id, Outcome: "filtered", Before: row.raw, Reason: reason}, limit)
			return nil
		}
		outcome := "kept"
		if stageID == "normalize" {
			outcome = "normalized"
		}
		sample := StageComparisonSample{RecordID: row.id, Outcome: outcome, Before: row.raw, After: next.raw, Reason: reason, ChangedFields: changedFields(row.data, next.data)}
		switch {
		case len(sample.ChangedFields) == 0:
			kept = appendBounded(kept, sample, limit)
		case stageID == "privacy" && containsField(sample.ChangedFields, "content"):
			contentChanges = appendBounded(contentChanges, sample, limit)
		default:
			changed = appendBounded(changed, sample, limit)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	if err := index.commit(); err != nil {
		return nil, err
	}
	for _, group := range [][]StageComparisonSample{filtered, contentChanges, changed, kept} {
		reverseSamples(group)
	}
	result := make([]StageComparisonSample, 0, limit)
	appendUpTo := func(group []StageComparisonSample, maximum int) {
		for index, sample := range group {
			if len(result) >= limit || index >= maximum {
				return
			}
			result = append(result, sample)
		}
	}
	balanced := max(1, limit/2)
	appendUpTo(filtered, balanced)
	appendUpTo(contentChanges, limit-len(result))
	appendUpTo(changed, limit-len(result))
	appendUpTo(kept, limit-len(result))
	if len(result) < limit && len(filtered) > balanced {
		appendUpTo(filtered[balanced:], limit-len(result))
	}
	return result, nil
}

func boundedRoutedSamples(ctx context.Context, artifactRoot, runID, previousID, stageID string, limit int) ([]StageComparisonSample, error) {
	index, err := buildComparisonIndexFromPath(ctx, filepath.Join(artifactRoot, runID, previousID+".jsonl"))
	if err != nil {
		return nil, err
	}
	defer index.Close()
	result := make([]StageComparisonSample, 0, limit)
	err = scanArtifact(ctx, filepath.Join(artifactRoot, runID, stageID+".jsonl"), func(row artifactRow) error {
		previous, exists, err := index.Lookup(row.id)
		if err != nil {
			return err
		}
		if !exists {
			return nil
		}
		result = appendBounded(result, StageComparisonSample{RecordID: row.id, Outcome: "routed", Before: previous.raw, After: row.raw, Reason: asString(row.data["reason"]), ChangedFields: changedFields(previous.data, row.data)}, limit)
		return nil
	})
	reverseSamples(result)
	return result, err
}

func buildComparisonIndex(ctx context.Context, directory, stageID string) (*comparisonIndex, error) {
	index, err := buildComparisonIndexFromPath(ctx, filepath.Join(directory, stageID+".jsonl"))
	if err != nil {
		return nil, err
	}
	if decisionID := decisionStage[stageID]; decisionID != "" {
		if err := index.writeReasons(ctx, filepath.Join(directory, "decisions.jsonl"), decisionID); err != nil {
			index.Close()
			return nil, err
		}
	}
	return index, nil
}

func buildComparisonIndexFromPath(ctx context.Context, source string) (*comparisonIndex, error) {
	temporary, err := os.CreateTemp(filepath.Dir(source), ".comparison-*.db")
	if err != nil {
		return nil, err
	}
	path := temporary.Name()
	if err := temporary.Close(); err != nil {
		os.Remove(path)
		return nil, err
	}
	database, err := bolt.Open(path, 0o600, &bolt.Options{Timeout: 0})
	if err != nil {
		os.Remove(path)
		return nil, err
	}
	index := &comparisonIndex{database: database, path: path}
	if err := index.begin(); err != nil {
		index.Close()
		return nil, err
	}
	err = scanArtifact(ctx, source, func(row artifactRow) error { return index.Put(row.id, row.raw) })
	if err == nil {
		err = index.commit()
	}
	if err != nil {
		index.Close()
		return nil, err
	}
	return index, nil
}

func (i *comparisonIndex) begin() error {
	tx, err := i.database.Begin(true)
	if err != nil {
		return err
	}
	rows, err := tx.CreateBucketIfNotExists(comparisonRowsBucket)
	if err != nil {
		tx.Rollback()
		return err
	}
	consumed, err := tx.CreateBucketIfNotExists(comparisonConsumedBucket)
	if err != nil {
		tx.Rollback()
		return err
	}
	reasons, err := tx.CreateBucketIfNotExists(comparisonReasonsBucket)
	if err != nil {
		tx.Rollback()
		return err
	}
	i.tx, i.rows, i.consumed, i.reasons, i.pending = tx, rows, consumed, reasons, 0
	return nil
}

func (i *comparisonIndex) rotate() error {
	if i.pending < 10_000 {
		return nil
	}
	if err := i.commit(); err != nil {
		return err
	}
	return i.begin()
}

func (i *comparisonIndex) commit() error {
	if i.tx == nil {
		return nil
	}
	err := i.tx.Commit()
	i.tx, i.rows, i.consumed, i.reasons, i.pending = nil, nil, nil, nil, 0
	return err
}

func (i *comparisonIndex) Put(id string, value []byte) error {
	if err := i.rows.Put([]byte(id), append([]byte(nil), value...)); err != nil {
		return err
	}
	i.pending++
	return i.rotate()
}

func (i *comparisonIndex) Lookup(id string) (artifactRow, bool, error) {
	var raw []byte
	err := i.database.View(func(tx *bolt.Tx) error {
		value := tx.Bucket(comparisonRowsBucket).Get([]byte(id))
		if value != nil {
			raw = append([]byte(nil), value...)
		}
		return nil
	})
	if err != nil || raw == nil {
		return artifactRow{}, false, err
	}
	row, err := decodeArtifactRow(raw)
	return row, err == nil, err
}

func (i *comparisonIndex) Match(id string) (artifactRow, bool, bool, string, error) {
	key := []byte(id)
	value := i.rows.Get(key)
	reason := string(i.reasons.Get(key))
	if value == nil {
		return artifactRow{}, false, false, reason, nil
	}
	if i.consumed.Get(key) != nil {
		return artifactRow{}, true, true, reason, nil
	}
	if err := i.consumed.Put(key, []byte{1}); err != nil {
		return artifactRow{}, false, false, reason, err
	}
	i.pending++
	row, err := decodeArtifactRow(value)
	if err != nil {
		return artifactRow{}, false, false, reason, err
	}
	if err := i.rotate(); err != nil {
		return artifactRow{}, false, false, reason, err
	}
	return row, true, false, reason, nil
}

func (i *comparisonIndex) writeReasons(ctx context.Context, path, stageID string) error {
	if err := i.begin(); err != nil {
		return err
	}
	err := scanArtifact(ctx, path, func(row artifactRow) error {
		if asString(row.data["stage_id"]) != stageID {
			return nil
		}
		if err := i.reasons.Put([]byte(asString(row.data["event_id"])), []byte(asString(row.data["reason"]))); err != nil {
			return err
		}
		i.pending++
		return i.rotate()
	})
	if err != nil {
		return err
	}
	return i.commit()
}

func (i *comparisonIndex) Close() {
	if i.tx != nil {
		_ = i.tx.Rollback()
	}
	_ = i.database.Close()
	_ = os.Remove(i.path)
}

func scanArtifact(ctx context.Context, path string, apply func(artifactRow) error) error {
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return ErrNotFound
		}
		return err
	}
	defer file.Close()
	reader := bufio.NewReaderSize(file, 256*1024)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, readErr := reader.ReadBytes('\n')
		line = bytesTrimSpace(line)
		if len(line) > 0 {
			row, err := decodeArtifactRow(line)
			if err != nil {
				return err
			}
			if err := apply(row); err != nil {
				return err
			}
		}
		if errors.Is(readErr, io.EOF) {
			return nil
		}
		if readErr != nil {
			return readErr
		}
	}
}

func decodeArtifactRow(raw []byte) (artifactRow, error) {
	var data map[string]any
	if err := json.Unmarshal(raw, &data); err != nil {
		return artifactRow{}, fmt.Errorf("decode stage artifact: %w", err)
	}
	id := asString(data["event_id"])
	if id == "" {
		id = asString(data["record_id"])
	}
	return artifactRow{id: id, raw: append(json.RawMessage(nil), raw...), data: data}, nil
}

func appendBounded[T any](items []T, item T, limit int) []T {
	if len(items) < limit {
		return append(items, item)
	}
	copy(items, items[1:])
	items[len(items)-1] = item
	return items
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
		beforeValue, had := before[key]
		afterValue, has := after[key]
		left, _ := json.Marshal(beforeValue)
		right, _ := json.Marshal(afterValue)
		if had != has || string(left) != string(right) {
			changed = append(changed, key)
		}
	}
	sort.Strings(changed)
	return changed
}

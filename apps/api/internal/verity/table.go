package verity

// The table is an inspectable, paginated boundary, not a sample. Its cache is
// derived from immutable artifacts with a disk-backed identity join. No full
// dataset is loaded into the browser or into a Go slice.
import (
	"bufio"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"

	bolt "go.etcd.io/bbolt"
)

type TableRow struct {
	StageComparisonSample
	Ordinal int    `json:"ordinal"`
	Change  string `json:"change"`
}

type TableMeta struct {
	Rows   int            `json:"rows"`
	Fields []string       `json:"fields"`
	Counts map[string]int `json:"counts"`
}

type TablePage struct {
	TableMeta
	RunID         string     `json:"run_id"`
	StageID       string     `json:"stage_id"`
	PreviousStage string     `json:"previous_stage_id,omitempty"`
	Snapshot      string     `json:"snapshot"`
	Records       []TableRow `json:"records"`
	NextCursor    string     `json:"next_cursor,omitempty"`
	Scanned       int        `json:"scanned"`
}

type tableCursor struct {
	Snapshot string `json:"s"`
	Filter   string `json:"f"`
	Query    string `json:"q"`
	Offset   int64  `json:"o"`
}

func tableBody(data map[string]any) map[string]any {
	if body, ok := data["payload"].(map[string]any); ok {
		return body
	}
	return data
}

func tableChange(before, after json.RawMessage, outcome string) string {
	if outcome == "input" {
		return "input"
	}
	if outcome == "elsewhere" {
		return "elsewhere"
	}
	if len(after) == 0 {
		return "removed"
	}
	if len(before) == 0 {
		return "added"
	}
	var left, right map[string]any
	_ = json.Unmarshal(before, &left)
	_ = json.Unmarshal(after, &right)
	if len(changedFields(tableBody(left), tableBody(right))) > 0 {
		return "changed"
	}
	return "unchanged"
}

func (s *Store) Table(ctx context.Context, stage, expectedRun, cursor, filter, query string, limit int) (TablePage, error) {
	if !validStageID(stage) {
		return TablePage{}, ErrNotFound
	}
	switch filter {
	case "all", "changed", "removed", "added", "unchanged", "result", "elsewhere":
	default:
		return TablePage{}, fmt.Errorf("%w: unknown table filter", ErrInvalidImport)
	}
	if len(query) > 200 {
		return TablePage{}, fmt.Errorf("%w: search is limited to 200 characters", ErrInvalidImport)
	}
	s.mu.RLock()
	runID := s.workspace.RunID
	outputs := append([]OutputSummary(nil), s.workspace.Outputs...)
	s.mu.RUnlock()
	if expectedRun != "" && expectedRun != runID {
		return TablePage{}, ErrConflict
	}
	dir := filepath.Join(s.cfg.ArtifactsDir, runID)
	revision := ""
	// A reviewed result is a new snapshot, not the original run's ready file.
	if stage == "curated" || stage == "review" {
		for _, output := range outputs {
			parts := strings.Split(output.ID, "__")
			if len(parts) == 3 && parts[1] == runID && safeRecordID(parts[2]) == parts[2] {
				revision = parts[2]
				break
			}
		}
	}
	snapshot := runID + ":" + revision + ":" + stage
	query = strings.ToLower(strings.TrimSpace(query))
	position := tableCursor{Snapshot: snapshot, Filter: filter, Query: query}
	if cursor != "" {
		encoded, err := base64.RawURLEncoding.DecodeString(cursor)
		if err != nil || json.Unmarshal(encoded, &position) != nil || position.Offset < 0 || position.Snapshot != snapshot || position.Filter != filter || position.Query != query {
			return TablePage{}, fmt.Errorf("%w: cursor belongs to another table view", ErrInvalidImport)
		}
	}
	cacheDir := dir
	if revision != "" {
		cacheDir = filepath.Join(dir, revision)
	}
	stem := filepath.Join(cacheDir, stage+".table-v1")
	// Only one cache builder at a time; request cancellation is checked while
	// streaming. A canceled build never publishes metadata or partial rows.
	s.tableGate.Lock()
	meta, err := readTableMeta(stem + ".json")
	if errors.Is(err, os.ErrNotExist) {
		meta, err = buildTable(ctx, dir, cacheDir, stage, revision, stem)
	}
	s.tableGate.Unlock()
	if err != nil {
		return TablePage{}, err
	}
	file, err := os.Open(stem + ".jsonl")
	if err != nil {
		return TablePage{}, err
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil {
		return TablePage{}, err
	}
	if position.Offset > info.Size() {
		return TablePage{}, fmt.Errorf("%w: cursor outside artifact", ErrInvalidImport)
	}
	if _, err := file.Seek(position.Offset, io.SeekStart); err != nil {
		return TablePage{}, err
	}
	reader := bufio.NewReaderSize(file, 64*1024)
	page := TablePage{TableMeta: meta, RunID: runID, StageID: stage, PreviousStage: previousStage[stage], Snapshot: snapshot, Records: make([]TableRow, 0, limit)}
	// Sparse searches can advance without matches. Bound work per request and
	// expose next_cursor, rather than blocking indefinitely on a huge file.
	for page.Scanned < 20_000 && len(page.Records) < max(1, min(limit, 100)) {
		if err := ctx.Err(); err != nil {
			return TablePage{}, err
		}
		line, err := reader.ReadBytes('\n')
		position.Offset += int64(len(line))
		if len(bytesTrimSpace(line)) > 0 {
			var row TableRow
			if err := json.Unmarshal(line, &row); err != nil {
				return TablePage{}, err
			}
			page.Scanned++
			match := filter == "all" || filter == row.Change || (filter == "result" && len(row.After) > 0)
			if match && (query == "" || strings.Contains(strings.ToLower(string(line)), query)) {
				page.Records = append(page.Records, row)
			}
		}
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			return TablePage{}, err
		}
	}
	if position.Offset < info.Size() {
		data, _ := json.Marshal(position)
		page.NextCursor = base64.RawURLEncoding.EncodeToString(data)
	}
	return page, nil
}

func readTableMeta(path string) (TableMeta, error) {
	var meta TableMeta
	data, err := os.ReadFile(path)
	if err != nil {
		return meta, err
	}
	err = json.Unmarshal(data, &meta)
	return meta, err
}

func buildTable(ctx context.Context, dir, cacheDir, stage, revision, stem string) (TableMeta, error) {
	meta := TableMeta{Counts: map[string]int{}, Fields: []string{}}
	fields := map[string]bool{}
	writer, err := newAtomicJSONL(stem + ".jsonl")
	if err != nil {
		return meta, err
	}
	defer writer.Abort()
	appendRow := func(sample StageComparisonSample) error {
		meta.Rows++
		change := tableChange(sample.Before, sample.After, sample.Outcome)
		meta.Counts[change]++
		if len(sample.After) > 0 {
			meta.Counts["result"]++
		}
		for _, raw := range []json.RawMessage{sample.Before, sample.After} {
			if len(raw) == 0 {
				continue
			}
			var body map[string]any
			if err := json.Unmarshal(raw, &body); err != nil {
				return err
			}
			for field := range tableBody(body) {
				fields[field] = true
			}
		}
		return writer.Write(TableRow{StageComparisonSample: sample, Ordinal: meta.Rows, Change: change})
	}
	if stage == "raw" {
		err = scanArtifact(ctx, filepath.Join(dir, "raw.jsonl"), func(row artifactRow) error {
			return appendRow(StageComparisonSample{RecordID: row.id, Outcome: "input", After: row.raw})
		})
	} else if revision != "" {
		// Rebuild reviewed branch boundaries from immutable signals and the
		// revision's saved decisions. No stale review/ready preview survives.
		var events []ReviewEvent
		data, e := os.ReadFile(filepath.Join(cacheDir, "reviews.json"))
		if e != nil {
			return meta, e
		}
		if e = json.Unmarshal(data, &events); e != nil {
			return meta, e
		}
		latest := latestReviews(events)
		err = scanArtifact(ctx, filepath.Join(dir, "signals.jsonl"), func(row artifactRow) error {
			var record pipelineRecord
			if e := json.Unmarshal(row.raw, &record); e != nil {
				return e
			}
			if event, ok := matchingReview(record, latest); ok {
				record.Decision = event.Decision
			}
			keep := stage == "review" && record.Decision == "review" || stage == "curated" && (record.Decision == "accepted" || record.Decision == "modified")
			sample := StageComparisonSample{RecordID: row.id, Before: row.raw, Outcome: "elsewhere", Reason: "Routed outside this branch."}
			if keep {
				sample.Outcome = "routed"
				sample.After, _ = json.Marshal(record)
				sample.Reason = "Current review decision: " + record.Decision
			}
			return appendRow(sample)
		})
	} else {
		index, e := buildComparisonIndex(ctx, dir, stage)
		if e != nil {
			return meta, e
		}
		defer index.Close()
		if e = index.begin(); e != nil {
			return meta, e
		}
		err = scanArtifact(ctx, filepath.Join(dir, previousStage[stage]+".jsonl"), func(row artifactRow) error {
			next, exists, consumed, reason, e := index.Match(row.id)
			if e != nil {
				return e
			}
			sample := StageComparisonSample{RecordID: row.id, Before: row.raw, Outcome: "kept", Reason: reason}
			if !exists || consumed {
				sample.Outcome = "filtered"
				if stage == "normalize" {
					sample.Reason = "Duplicate record ID removed."
				}
				if stage == "review" || stage == "curated" {
					sample.Outcome = "elsewhere"
					sample.Reason = "Routed outside this branch."
				}
			} else {
				sample.After = next.raw
				if stage == "review" || stage == "curated" {
					sample.Outcome = "routed"
				}
			}
			return appendRow(sample)
		})
		if err != nil {
			return meta, err
		}
		if err = index.commit(); err != nil {
			return meta, err
		}
		// Newly emitted rows have no predecessor. Never silently drop them.
		err = scanArtifact(ctx, filepath.Join(dir, stage+".jsonl"), func(row artifactRow) error {
			var used bool
			e := index.database.View(func(tx *bolt.Tx) error {
				used = tx.Bucket(comparisonConsumedBucket).Get([]byte(row.id)) != nil
				return nil
			})
			if e != nil || used {
				return e
			}
			return appendRow(StageComparisonSample{RecordID: row.id, Outcome: "added", After: row.raw, Reason: "Emitted by this stage."})
		})
	}
	if err != nil {
		return meta, err
	}
	for field := range fields {
		meta.Fields = append(meta.Fields, field)
	}
	sort.Strings(meta.Fields)
	if err := writer.Commit(); err != nil {
		return meta, err
	}
	return meta, writeJSONAtomic(stem+".json", meta)
}

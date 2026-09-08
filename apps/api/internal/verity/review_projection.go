package verity

import (
	"context"
	"crypto/sha256"
	"encoding/csv"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/parquet-go/parquet-go"
)

// ReviewPage reads the actual queue, not the visualization sample. The first
// unresolved page is refilled after each decision so every record is reachable.
func (s *Store) ReviewPage(ctx context.Context, limit int) (int, []EvidenceRecord, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	limit = max(1, min(limit, 200))
	rows := make([]EvidenceRecord, 0, limit)
	overrides := latestReviews(s.reviewEvents)
	err := forEachJSONLine[pipelineRecord](ctx, filepath.Join(s.cfg.ArtifactsDir, s.workspace.RunID, "review.jsonl"), func(row pipelineRecord) error {
		if event, ok := matchingReview(row, overrides); ok && event.Decision != "review" {
			return nil
		}
		rows = append(rows, evidenceFor(row))
		if len(rows) == limit {
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, os.ErrNotExist) {
		// Seed-only workspaces have no run artifacts yet.
		for _, row := range s.workspace.Records {
			if row.Decision == "review" && len(rows) < limit {
				rows = append(rows, row)
			}
		}
		err = nil
	}
	if errors.Is(err, io.EOF) {
		err = nil
	}
	return s.workspace.DecisionBreakdown.Review, rows, err
}

func evidenceFor(row pipelineRecord) EvidenceRecord {
	return EvidenceRecord{ID: row.EventID, BatchID: row.BatchID, RawEvent: row.Content,
		ExtractedSignal: row.ExtractedSignal, SignalType: row.SignalType, Confidence: row.Confidence,
		QualityScore: row.QualityScore, Decision: row.Decision, Reason: row.Reason,
		OccurredAt: row.OccurredAt, Privacy: row.Privacy, EvidenceCount: row.EvidenceCount}
}

func reviewHash(row pipelineRecord) string {
	bytes, _ := json.Marshal(row)
	hash := sha256.Sum256(bytes)
	return hex.EncodeToString(hash[:])
}

func latestReviews(events []ReviewEvent) map[string]ReviewEvent {
	latest := make(map[string]ReviewEvent, len(events))
	for _, event := range events {
		latest[event.RecordID] = event
	}
	return latest
}

func matchingReview(row pipelineRecord, latest map[string]ReviewEvent) (ReviewEvent, bool) {
	event, ok := latest[row.EventID]
	return event, ok && event.RecordHash != "" && event.RecordHash == reviewHash(row)
}

func (s *Store) findReviewRecordLocked(id string) (pipelineRecord, error) {
	var found pipelineRecord
	err := forEachJSONLine[pipelineRecord](context.Background(), filepath.Join(s.cfg.ArtifactsDir, s.workspace.RunID, "signals.jsonl"), func(row pipelineRecord) error {
		if row.EventID == id {
			found = row
			return io.EOF
		}
		return nil
	})
	if errors.Is(err, io.EOF) {
		return found, nil
	}
	if err != nil {
		return found, err
	}
	return found, ErrNotFound
}

// Each review revision is a new output snapshot. Publish the workspace pointer
// only after all formats and its audit file are durable; earlier downloads stay valid.
func (s *Store) projectReviewsLocked(workspace Workspace, events []ReviewEvent) (Workspace, error) {
	source := filepath.Join(s.cfg.ArtifactsDir, workspace.RunID, "signals.jsonl")
	if _, err := os.Stat(source); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			applyReviewOverrides(&workspace, events)
			return workspace, nil
		}
		return Workspace{}, err
	}
	if len(events) == 0 {
		return workspace, nil
	}
	encoded, _ := json.Marshal(events)
	hash := sha256.Sum256(encoded)
	revision := "review_" + hex.EncodeToString(hash[:12])
	directory := filepath.Join(s.cfg.ArtifactsDir, workspace.RunID, revision)
	jsonl, err := newAtomicJSONL(filepath.Join(directory, "curated.jsonl"))
	if err != nil {
		return Workspace{}, err
	}
	defer jsonl.Abort()
	csvFile, err := newAtomicFile(filepath.Join(directory, "curated.csv"))
	if err != nil {
		return Workspace{}, err
	}
	defer csvFile.Abort()
	csvWriter := csv.NewWriter(csvFile.file)
	if err := csvWriter.Write([]string{"event_id", "batch_id", "occurred_at", "signal_type", "extracted_signal", "confidence", "quality_score", "decision"}); err != nil {
		return Workspace{}, err
	}
	parquetFile, err := newAtomicFile(filepath.Join(directory, "curated.parquet"))
	if err != nil {
		return Workspace{}, err
	}
	defer parquetFile.Abort()
	pw := parquet.NewGenericWriter[curatedParquetRow](parquetFile.file, parquet.MaxRowsPerRowGroup(8192))
	defer pw.Close()
	buffer := make([]curatedParquetRow, 0, 1024)
	latest := latestReviews(events)
	counts := DecisionBreakdown{}
	err = forEachJSONLine[pipelineRecord](context.Background(), source, func(row pipelineRecord) error {
		if event, ok := matchingReview(row, latest); ok {
			row.Decision = event.Decision
		}
		adjustDecision(&counts, row.Decision, 1)
		if row.Decision != "accepted" && row.Decision != "modified" {
			return nil
		}
		projection := curatedParquetRow{EventID: row.EventID, BatchID: row.BatchID, OccurredAt: row.OccurredAt,
			SignalType: row.SignalType, ExtractedSignal: row.ExtractedSignal, Confidence: row.Confidence,
			QualityScore: row.QualityScore, Decision: row.Decision}
		if err := jsonl.Write(projection); err != nil {
			return err
		}
		if err := csvWriter.Write([]string{row.EventID, row.BatchID, row.OccurredAt, row.SignalType, row.ExtractedSignal, strconv.FormatFloat(row.Confidence, 'f', -1, 64), strconv.FormatFloat(row.QualityScore, 'f', -1, 64), row.Decision}); err != nil {
			return err
		}
		buffer = append(buffer, projection)
		if len(buffer) == cap(buffer) {
			if _, err := pw.Write(buffer); err != nil {
				return err
			}
			buffer = buffer[:0]
		}
		return nil
	})
	if err != nil {
		return Workspace{}, err
	}
	if _, err := pw.Write(buffer); err != nil {
		return Workspace{}, err
	}
	if err := pw.Close(); err != nil {
		return Workspace{}, err
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return Workspace{}, err
	}
	for _, commit := range []func() error{jsonl.Commit, csvFile.Commit, parquetFile.Commit} {
		if err := commit(); err != nil {
			return Workspace{}, err
		}
	}
	workspace.DecisionBreakdown = counts
	for i, record := range workspace.Records {
		if event, ok := latest[record.ID]; ok {
			workspace.Records[i].Decision = event.Decision
			if event.Note != "" {
				workspace.Records[i].Reason = event.Note
			}
		}
	}
	for i := range workspace.Outputs {
		output := &workspace.Outputs[i]
		if strings.Contains(strings.ToLower(output.Name), "decision") {
			continue
		}
		output.ID = fmt.Sprintf("out_ready_%s__%s__%s", output.Format, workspace.RunID, revision)
		output.RecordCount = counts.Accepted
		output.Size = fileSize(filepath.Join(directory, "curated."+output.Format))
	}
	if err := writeJSONAtomic(filepath.Join(directory, "reviews.json"), events); err != nil {
		return Workspace{}, err
	}
	if err := writeJSONAtomic(filepath.Join(directory, "workspace.json"), workspace); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

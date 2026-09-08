package verity

import (
	"bufio"
	"context"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"

	"github.com/parquet-go/parquet-go"
)

type atomicFile struct {
	file      *os.File
	temporary string
	target    string
}

type atomicJSONL struct {
	file    *atomicFile
	encoder *json.Encoder
}

type streamBatch struct {
	count    int
	added    string
	filename string
	state    string
	checksum string
	fields   map[string]struct{}
}

type evidenceBucket struct {
	review   *pipelineRecord
	accepted []pipelineRecord
}

type evidenceSampler struct {
	buckets map[string]*evidenceBucket
}

func runStreamingPipeline(ctx context.Context, cfg PipelineConfig) (Workspace, error) {
	if cfg.RunID == "" || cfg.ArtifactDir == "" {
		return Workspace{}, errors.New("pipeline requires run and artifact identity")
	}
	if err := os.MkdirAll(cfg.ArtifactDir, 0o750); err != nil {
		return Workspace{}, fmt.Errorf("create run artifacts: %w", err)
	}
	summary := pipelineSummary{Distribution: make(map[string]int)}
	if err := streamRawStage(ctx, cfg, &summary); err != nil {
		return Workspace{}, err
	}
	reportStage(cfg, "raw", summary.RawCount, summary.RawCount)
	if err := streamNormalizeStage(ctx, cfg, &summary); err != nil {
		return Workspace{}, err
	}
	reportStage(cfg, "normalize", summary.NormalizedCount, summary.RawCount)
	decisions, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "decisions.jsonl"))
	if err != nil {
		return Workspace{}, err
	}
	defer decisions.Abort()
	if err := streamPrivacyStage(ctx, cfg, &summary, decisions); err != nil {
		return Workspace{}, err
	}
	reportStage(cfg, "privacy", summary.PrivacyCount, summary.NormalizedCount)
	if err := streamQualityStage(ctx, cfg, &summary, decisions); err != nil {
		return Workspace{}, err
	}
	reportStage(cfg, "quality", summary.QualityCount, summary.PrivacyCount)
	if err := streamSignalStages(ctx, cfg, &summary, decisions); err != nil {
		return Workspace{}, err
	}
	reportStage(cfg, "signals", summary.SignalCount, summary.QualityCount)
	reportStage(cfg, "review", summary.ReviewCount, summary.SignalCount)
	reportStage(cfg, "curated", summary.AcceptedCount, summary.SignalCount)
	if err := decisions.Commit(); err != nil {
		return Workspace{}, fmt.Errorf("publish decision lineage: %w", err)
	}

	workspace := buildWorkspaceFromSummary(cfg.RunID, cfg.ArtifactDir, summary)
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

func reportStage(cfg PipelineConfig, stageID string, count, inputCount int) {
	if cfg.OnStageCommitted != nil {
		cfg.OnStageCommitted(StageProgress{StageID: stageID, Count: count, InputCount: inputCount})
	}
}

func streamRawStage(ctx context.Context, cfg PipelineConfig, summary *pipelineSummary) error {
	inspection := &inspectionCollector{}
	paths, err := rawSourcePaths(cfg.RawDirs)
	if err != nil {
		return err
	}
	writer, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "raw.jsonl"))
	if err != nil {
		return err
	}
	defer writer.Abort()
	fields := make(map[string]struct{})
	batches := make(map[string]*streamBatch)
	for _, path := range paths {
		file, err := os.Open(path)
		if err != nil {
			return fmt.Errorf("open input %s: %w", filepath.Base(path), err)
		}
		scanner := bufio.NewScanner(file)
		scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
		line := 0
		for scanner.Scan() {
			line++
			if err := ctx.Err(); err != nil {
				file.Close()
				return err
			}
			if len(bytesTrimSpace(scanner.Bytes())) == 0 {
				continue
			}
			var record dataRecord
			if err := json.Unmarshal(scanner.Bytes(), &record); err != nil {
				file.Close()
				return fmt.Errorf("decode %s line %d: %w", filepath.Base(path), line, err)
			}
			if record.RecordID == "" || record.DatasetID == "" || record.BatchID == "" || record.Payload == nil {
				file.Close()
				return fmt.Errorf("invalid record envelope in %s line %d", filepath.Base(path), line)
			}
			if record.Metadata == nil {
				record.Metadata = map[string]any{}
			}
			if err := writer.Write(record); err != nil {
				file.Close()
				return err
			}
			summary.RawCount++
			inspection.Add(nil, record, "input", "")
			batch := batches[record.BatchID]
			if batch == nil {
				batch = &streamBatch{added: record.IngestedAt, filename: record.BatchID + ".jsonl", state: "complete", fields: make(map[string]struct{})}
				batches[record.BatchID] = batch
			}
			batch.count++
			if batch.added == "" || (record.IngestedAt != "" && record.IngestedAt < batch.added) {
				batch.added = record.IngestedAt
			}
			if filename := asString(record.Metadata["filename"]); filename != "" {
				batch.filename = filename
			}
			if asString(record.Metadata["state"]) == "staged" {
				batch.state = "staged"
			}
			if checksum := asString(record.Metadata["source_checksum"]); checksum != "" {
				batch.checksum = checksum
			}
			for key := range record.Payload {
				fields[key] = struct{}{}
				batch.fields[key] = struct{}{}
			}
		}
		scanErr := scanner.Err()
		closeErr := file.Close()
		if scanErr != nil {
			return fmt.Errorf("scan input %s: %w", filepath.Base(path), scanErr)
		}
		if closeErr != nil {
			return closeErr
		}
	}
	if err := writer.Commit(); err != nil {
		return fmt.Errorf("publish raw stage: %w", err)
	}
	summary.FieldCount = len(fields)
	summary.Batches = summarizeStreamBatches(batches)
	return inspection.Commit(cfg.ArtifactDir, "raw")
}

func streamNormalizeStage(ctx context.Context, cfg PipelineConfig, summary *pipelineSummary) error {
	inspection := &inspectionCollector{}
	writer, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "normalize.jsonl"))
	if err != nil {
		return err
	}
	defer writer.Abort()
	index, err := openDedupIndex(filepath.Join(cfg.ArtifactDir, ".dedup.db"))
	if err != nil {
		return err
	}
	closed := false
	defer func() {
		if !closed {
			_ = index.Close(false)
		}
	}()
	err = forEachJSONLine[dataRecord](ctx, filepath.Join(cfg.ArtifactDir, "raw.jsonl"), func(record dataRecord) error {
		seen, err := index.Seen(record.RecordID)
		if err != nil {
			return err
		}
		if seen {
			inspection.Add(record, nil, "filtered", "Duplicate record ID removed.")
			return nil
		}
		prepared := normalizeRecord(record)
		inspection.Add(record, prepared, "normalized", "Canonical fields and types.")
		if err := writer.Write(prepared); err != nil {
			return err
		}
		summary.NormalizedCount++
		return nil
	})
	if err != nil {
		return err
	}
	if err := index.Close(true); err != nil {
		return err
	}
	closed = true
	if err := writer.Commit(); err != nil {
		return fmt.Errorf("publish normalize stage: %w", err)
	}
	return inspection.Commit(cfg.ArtifactDir, "normalize")
}

func streamPrivacyStage(ctx context.Context, cfg PipelineConfig, summary *pipelineSummary, decisions *atomicJSONL) error {
	inspection := &inspectionCollector{}
	writer, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "privacy.jsonl"))
	if err != nil {
		return err
	}
	defer writer.Abort()
	err = forEachJSONLine[pipelineRecord](ctx, filepath.Join(cfg.ArtifactDir, "normalize.jsonl"), func(row pipelineRecord) error {
		prepared, decision, keep := applyPrivacy(row, cfg.RunID)
		reason := ""
		if decision != nil {
			reason = decision.Reason
		}
		if keep {
			inspection.Add(row, prepared, "kept", reason)
		} else {
			inspection.Add(row, nil, "filtered", reason)
		}
		if decision != nil {
			if err := decisions.Write(*decision); err != nil {
				return err
			}
			summary.DecisionCount++
		}
		if keep {
			if err := writer.Write(prepared); err != nil {
				return err
			}
			summary.PrivacyCount++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := writer.Commit(); err != nil {
		return err
	}
	return inspection.Commit(cfg.ArtifactDir, "privacy")
}

func streamQualityStage(ctx context.Context, cfg PipelineConfig, summary *pipelineSummary, decisions *atomicJSONL) error {
	inspection := &inspectionCollector{}
	writer, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "quality.jsonl"))
	if err != nil {
		return err
	}
	defer writer.Abort()
	err = forEachJSONLine[pipelineRecord](ctx, filepath.Join(cfg.ArtifactDir, "privacy.jsonl"), func(row pipelineRecord) error {
		prepared, decision, keep := applyQuality(row, cfg.RunID)
		reason := ""
		if decision != nil {
			reason = decision.Reason
		}
		if keep {
			inspection.Add(row, prepared, "kept", reason)
		} else {
			inspection.Add(row, nil, "filtered", reason)
		}
		if decision != nil {
			if err := decisions.Write(*decision); err != nil {
				return err
			}
			summary.DecisionCount++
		}
		if keep {
			if err := writer.Write(prepared); err != nil {
				return err
			}
			summary.QualityCount++
		}
		return nil
	})
	if err != nil {
		return err
	}
	if err := writer.Commit(); err != nil {
		return err
	}
	return inspection.Commit(cfg.ArtifactDir, "quality")
}

func streamSignalStages(ctx context.Context, cfg PipelineConfig, summary *pipelineSummary, decisions *atomicJSONL) error {
	inspection, reviewInspection, readyInspection := &inspectionCollector{}, &inspectionCollector{}, &inspectionCollector{}
	signals, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "signals.jsonl"))
	if err != nil {
		return err
	}
	defer signals.Abort()
	review, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "review.jsonl"))
	if err != nil {
		return err
	}
	defer review.Abort()
	curated, err := newAtomicJSONL(filepath.Join(cfg.ArtifactDir, "curated.jsonl"))
	if err != nil {
		return err
	}
	defer curated.Abort()
	csvFile, err := newAtomicFile(filepath.Join(cfg.ArtifactDir, "curated.csv"))
	if err != nil {
		return err
	}
	defer csvFile.Abort()
	csvWriter := csv.NewWriter(csvFile.file)
	if err := csvWriter.Write([]string{"event_id", "batch_id", "occurred_at", "signal_type", "extracted_signal", "confidence", "quality_score", "decision"}); err != nil {
		return err
	}
	parquetFile, err := newAtomicFile(filepath.Join(cfg.ArtifactDir, "curated.parquet"))
	if err != nil {
		return err
	}
	defer parquetFile.Abort()
	parquetWriter := parquet.NewGenericWriter[curatedParquetRow](parquetFile.file, parquet.MaxRowsPerRowGroup(8_192))
	parquetOpen := true
	defer func() {
		if parquetOpen {
			_ = parquetWriter.Close()
		}
	}()
	parquetRows := make([]curatedParquetRow, 0, 1_024)
	sampler := evidenceSampler{buckets: make(map[string]*evidenceBucket)}

	err = forEachJSONLine[pipelineRecord](ctx, filepath.Join(cfg.ArtifactDir, "quality.jsonl"), func(row pipelineRecord) error {
		prepared, decision, keep := applySignal(row, cfg.RunID)
		if keep {
			inspection.Add(row, prepared, "kept", decision.Reason)
		} else {
			inspection.Add(row, nil, "filtered", decision.Reason)
		}
		if err := decisions.Write(decision); err != nil {
			return err
		}
		summary.DecisionCount++
		if !keep {
			return nil
		}
		if err := signals.Write(prepared); err != nil {
			return err
		}
		summary.SignalCount++
		summary.Distribution[prepared.SignalType]++
		sampler.Add(prepared)
		if prepared.Decision == "review" {
			reviewInspection.Add(prepared, prepared, "routed", prepared.Reason)
			summary.ReviewCount++
			return review.Write(prepared)
		}
		summary.AcceptedCount++
		readyInspection.Add(prepared, prepared, "routed", prepared.Reason)
		if err := curated.Write(prepared); err != nil {
			return err
		}
		if err := csvWriter.Write([]string{prepared.EventID, prepared.BatchID, prepared.OccurredAt, prepared.SignalType, prepared.ExtractedSignal, strconv.FormatFloat(prepared.Confidence, 'f', 2, 64), strconv.FormatFloat(prepared.QualityScore, 'f', 2, 64), prepared.Decision}); err != nil {
			return err
		}
		parquetRows = append(parquetRows, curatedParquetRow{EventID: prepared.EventID, BatchID: prepared.BatchID, OccurredAt: prepared.OccurredAt, SignalType: prepared.SignalType, ExtractedSignal: prepared.ExtractedSignal, Confidence: prepared.Confidence, QualityScore: prepared.QualityScore, Decision: prepared.Decision})
		if len(parquetRows) == cap(parquetRows) {
			if _, err := parquetWriter.Write(parquetRows); err != nil {
				return err
			}
			parquetRows = parquetRows[:0]
		}
		return nil
	})
	if err != nil {
		return err
	}
	if len(parquetRows) > 0 {
		if _, err := parquetWriter.Write(parquetRows); err != nil {
			return err
		}
	}
	csvWriter.Flush()
	if err := csvWriter.Error(); err != nil {
		return err
	}
	if err := parquetWriter.Close(); err != nil {
		return err
	}
	parquetOpen = false
	if err := signals.Commit(); err != nil {
		return err
	}
	if err := review.Commit(); err != nil {
		return err
	}
	if err := curated.Commit(); err != nil {
		return err
	}
	if err := csvFile.Commit(); err != nil {
		return err
	}
	if err := parquetFile.Commit(); err != nil {
		return err
	}
	summary.SignalSamples = sampler.Samples()
	for stage, collector := range map[string]*inspectionCollector{"signals": inspection, "review": reviewInspection, "curated": readyInspection} {
		if err := collector.Commit(cfg.ArtifactDir, stage); err != nil {
			return err
		}
	}
	return nil
}

func rawSourcePaths(directories []string) ([]string, error) {
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
	return paths, nil
}

func forEachJSONLine[T any](ctx context.Context, path string, apply func(T) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	line := 0
	for scanner.Scan() {
		line++
		if err := ctx.Err(); err != nil {
			return err
		}
		data := bytesTrimSpace(scanner.Bytes())
		if len(data) == 0 {
			continue
		}
		var item T
		if err := json.Unmarshal(data, &item); err != nil {
			return fmt.Errorf("decode %s line %d: %w", filepath.Base(path), line, err)
		}
		if err := apply(item); err != nil {
			return err
		}
	}
	return scanner.Err()
}

func newAtomicFile(target string) (*atomicFile, error) {
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return nil, err
	}
	file, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+"-*.tmp")
	if err != nil {
		return nil, err
	}
	return &atomicFile{file: file, temporary: file.Name(), target: target}, nil
}

func (a *atomicFile) Commit() error {
	if a.file == nil {
		return nil
	}
	if err := a.file.Sync(); err != nil {
		return err
	}
	if err := a.file.Close(); err != nil {
		return err
	}
	a.file = nil
	if err := os.Chmod(a.temporary, 0o600); err != nil {
		return err
	}
	if err := os.Rename(a.temporary, a.target); err != nil {
		return err
	}
	a.temporary = ""
	return nil
}

func (a *atomicFile) Abort() {
	if a.file != nil {
		_ = a.file.Close()
		a.file = nil
	}
	if a.temporary != "" {
		_ = os.Remove(a.temporary)
		a.temporary = ""
	}
}

func newAtomicJSONL(target string) (*atomicJSONL, error) {
	file, err := newAtomicFile(target)
	if err != nil {
		return nil, err
	}
	encoder := json.NewEncoder(file.file)
	encoder.SetEscapeHTML(false)
	return &atomicJSONL{file: file, encoder: encoder}, nil
}

func (a *atomicJSONL) Write(value any) error { return a.encoder.Encode(value) }
func (a *atomicJSONL) Commit() error         { return a.file.Commit() }
func (a *atomicJSONL) Abort()                { a.file.Abort() }

func summarizeStreamBatches(grouped map[string]*streamBatch) []BatchSummary {
	ids := make([]string, 0, len(grouped))
	for id := range grouped {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	result := make([]BatchSummary, 0, len(ids))
	for _, id := range ids {
		item := grouped[id]
		result = append(result, BatchSummary{ID: id, Filename: item.filename, AddedAt: item.added, RecordCount: item.count, FieldCount: len(item.fields), State: item.state, Checksum: item.checksum})
	}
	return result
}

func (s *evidenceSampler) Add(row pipelineRecord) {
	bucket := s.buckets[row.SignalType]
	if bucket == nil {
		bucket = &evidenceBucket{}
		s.buckets[row.SignalType] = bucket
	}
	if row.Decision == "review" {
		if bucket.review == nil {
			copy := row
			bucket.review = &copy
		}
		return
	}
	bucket.accepted = append(bucket.accepted, row)
	sort.SliceStable(bucket.accepted, func(left, right int) bool {
		return bucket.accepted[left].Confidence > bucket.accepted[right].Confidence
	})
	if len(bucket.accepted) > 4 {
		bucket.accepted = bucket.accepted[:4]
	}
}

func (s *evidenceSampler) Samples() []pipelineRecord {
	result := make([]pipelineRecord, 0, len(s.buckets)*5)
	for _, bucket := range s.buckets {
		if bucket.review != nil {
			result = append(result, *bucket.review)
		}
		result = append(result, bucket.accepted...)
	}
	return result
}

package verity

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	ErrInvalidImport    = errors.New("invalid import")
	ErrUploadOffset     = errors.New("upload offset conflict")
	ErrUploadIncomplete = errors.New("upload is incomplete")
	ErrUploadTooLarge   = errors.New("upload exceeds configured limit")
)

func (s *Store) CreateImport(request ImportCreate, idempotencyKey string) (ImportSummary, bool, error) {
	filename := strings.TrimSpace(request.Filename)
	idempotencyKey = strings.TrimSpace(idempotencyKey)
	if request.DatasetID != s.Workspace().Dataset.ID {
		return ImportSummary{}, false, ErrNotFound
	}
	if filename == "" || filename != filepath.Base(filename) || strings.ContainsAny(filename, "\x00/\\") {
		return ImportSummary{}, false, fmt.Errorf("%w: filename must be a plain file name", ErrInvalidImport)
	}
	if request.SizeBytes < 1 {
		return ImportSummary{}, false, fmt.Errorf("%w: size_bytes must be greater than zero", ErrInvalidImport)
	}
	if request.SizeBytes > s.cfg.MaxUploadBytes {
		return ImportSummary{}, false, ErrUploadTooLarge
	}
	if idempotencyKey == "" || len(idempotencyKey) > 200 {
		return ImportSummary{}, false, fmt.Errorf("%w: Idempotency-Key is required and must be 200 characters or fewer", ErrInvalidImport)
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if existingID := s.importKeys[idempotencyKey]; existingID != "" {
		return s.imports[existingID], true, nil
	}
	importID, err := randomID("imp_")
	if err != nil {
		return ImportSummary{}, false, fmt.Errorf("create import identity: %w", err)
	}
	uploadID, err := randomID("upl_")
	if err != nil {
		return ImportSummary{}, false, fmt.Errorf("create upload identity: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	created := ImportSummary{
		ImportID: importID, UploadID: uploadID, UploadURL: "/api/v1/uploads/" + uploadID,
		DatasetID: request.DatasetID, Filename: filename, MediaType: strings.TrimSpace(request.MediaType),
		SizeBytes: request.SizeBytes, State: ImportCreated, CreatedAt: now,
	}
	target := s.incompleteUploadPath(uploadID)
	file, err := os.OpenFile(target, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return ImportSummary{}, false, fmt.Errorf("create upload: %w", err)
	}
	if err := file.Close(); err != nil {
		return ImportSummary{}, false, fmt.Errorf("close upload: %w", err)
	}
	s.imports[importID] = created
	s.importKeys[idempotencyKey] = importID
	if err := s.persistLocked(); err != nil {
		return ImportSummary{}, false, err
	}
	return created, false, nil
}

func (s *Store) ImportByUpload(uploadID string) (ImportSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	for _, item := range s.imports {
		if item.UploadID == uploadID {
			return item, true
		}
	}
	return ImportSummary{}, false
}

func (s *Store) Import(importID string) (ImportSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	item, ok := s.imports[importID]
	return item, ok
}

func (s *Store) AppendUpload(uploadID string, expectedOffset int64, body io.Reader, contentLength int64) (int64, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	var item ImportSummary
	found := false
	for _, candidate := range s.imports {
		if candidate.UploadID == uploadID {
			item, found = candidate, true
			break
		}
	}
	if !found {
		return 0, ErrNotFound
	}
	if item.State != ImportCreated && item.State != ImportUploading {
		return item.Offset, ErrConflict
	}
	if expectedOffset != item.Offset {
		return item.Offset, ErrUploadOffset
	}
	remaining := item.SizeBytes - item.Offset
	if contentLength > remaining {
		return item.Offset, ErrUploadTooLarge
	}
	file, err := os.OpenFile(s.incompleteUploadPath(uploadID), os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return item.Offset, fmt.Errorf("open upload: %w", err)
	}
	start := item.Offset
	written, copyErr := io.Copy(file, io.LimitReader(body, remaining+1))
	if written > remaining {
		_ = file.Truncate(start)
		_ = file.Close()
		return start, ErrUploadTooLarge
	}
	if syncErr := file.Sync(); syncErr != nil && copyErr == nil {
		copyErr = syncErr
	}
	if closeErr := file.Close(); closeErr != nil && copyErr == nil {
		copyErr = closeErr
	}
	if copyErr != nil {
		_ = os.Truncate(s.incompleteUploadPath(uploadID), start)
		return start, fmt.Errorf("write upload: %w", copyErr)
	}
	item.Offset += written
	item.State = ImportUploading
	s.imports[item.ImportID] = item
	if err := s.persistLocked(); err != nil {
		return start, err
	}
	return item.Offset, nil
}

func (s *Store) CompleteImport(importID, idempotencyKey string) (JobSummary, bool, error) {
	if strings.TrimSpace(idempotencyKey) == "" {
		return JobSummary{}, false, fmt.Errorf("%w: Idempotency-Key is required", ErrInvalidImport)
	}
	s.mu.Lock()
	item, ok := s.imports[importID]
	if !ok {
		s.mu.Unlock()
		return JobSummary{}, false, ErrNotFound
	}
	if item.JobID != "" {
		job := s.jobs[item.JobID]
		s.mu.Unlock()
		return job, true, nil
	}
	if item.Offset != item.SizeBytes {
		s.mu.Unlock()
		return JobSummary{}, false, ErrUploadIncomplete
	}
	jobID, err := randomID("job_")
	if err != nil {
		s.mu.Unlock()
		return JobSummary{}, false, err
	}
	target := s.completeUploadPath(item)
	if err := os.Rename(s.incompleteUploadPath(item.UploadID), target); err != nil {
		s.mu.Unlock()
		return JobSummary{}, false, fmt.Errorf("finalize upload: %w", err)
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	job := JobSummary{
		ID: jobID, ImportID: importID, State: ImportProfiling,
		StatusURL: "/api/v1/jobs/" + jobID, EventsURL: "/api/v1/jobs/" + jobID + "/events",
		CreatedAt: now, UpdatedAt: now,
	}
	item.State = ImportProfiling
	item.JobID = jobID
	s.imports[importID] = item
	s.jobs[jobID] = job
	s.appendJobEventLocked(jobID, "profile_ready", "", 0, 0, "Upload committed; profiling started.")
	if err := s.persistLocked(); err != nil {
		s.mu.Unlock()
		return JobSummary{}, false, err
	}
	s.mu.Unlock()
	go s.processImport(jobID, target)
	return job, false, nil
}

func (s *Store) Job(jobID string) (JobSummary, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	job, ok := s.jobs[jobID]
	return job, ok
}

func (s *Store) JobEvents(jobID string, after uint64) ([]JobEvent, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if _, ok := s.jobs[jobID]; !ok {
		return nil, false
	}
	events := s.jobEvents[jobID]
	result := make([]JobEvent, 0, len(events))
	for _, event := range events {
		if event.Sequence > after {
			result = append(result, event)
		}
	}
	return result, true
}

func (s *Store) StageRecords(ctx context.Context, stageID, cursor string, limit int) (StagePage, error) {
	if !validStageID(stageID) {
		return StagePage{}, ErrNotFound
	}
	offset := 0
	if strings.TrimSpace(cursor) != "" {
		parsed, err := strconv.Atoi(cursor)
		if err != nil || parsed < 0 {
			return StagePage{}, fmt.Errorf("%w: cursor must be a non-negative integer", ErrInvalidImport)
		}
		offset = parsed
	}
	limit = max(1, min(limit, 200))
	s.mu.RLock()
	runID := s.workspace.RunID
	s.mu.RUnlock()
	path := filepath.Join(s.cfg.ArtifactsDir, runID, stageID+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return StagePage{}, ErrNotFound
		}
		return StagePage{}, err
	}
	defer file.Close()
	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 64*1024*1024)
	page := StagePage{StageID: stageID, Rows: make([]json.RawMessage, 0, limit)}
	seen := 0
	for scanner.Scan() {
		if err := ctx.Err(); err != nil {
			return StagePage{}, err
		}
		line := bytesTrimSpace(scanner.Bytes())
		if len(line) == 0 {
			continue
		}
		if seen < offset {
			seen++
			continue
		}
		if len(page.Rows) == limit {
			page.NextCursor = strconv.Itoa(offset + limit)
			break
		}
		if !json.Valid(line) {
			return StagePage{}, errors.New("stage artifact contains invalid JSON")
		}
		page.Rows = append(page.Rows, json.RawMessage(append([]byte(nil), line...)))
		seen++
	}
	if err := scanner.Err(); err != nil {
		return StagePage{}, err
	}
	page.Returned = len(page.Rows)
	return page, nil
}

func (s *Store) OutputPath(outputID string) (string, OutputSummary, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	var selected OutputSummary
	for _, output := range s.workspace.Outputs {
		if output.ID == outputID {
			selected = output
			break
		}
	}
	if selected.ID == "" {
		return "", OutputSummary{}, ErrNotFound
	}
	filename := "curated." + selected.Format
	if strings.Contains(strings.ToLower(selected.Name), "decision") {
		filename = "decisions.jsonl"
	}
	path := filepath.Join(s.cfg.ArtifactsDir, s.workspace.RunID, filename)
	if _, err := os.Stat(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", OutputSummary{}, ErrNotFound
		}
		return "", OutputSummary{}, err
	}
	return path, selected, nil
}

func (s *Store) processImport(jobID, sourcePath string) {
	job, ok := s.Job(jobID)
	if !ok {
		return
	}
	item, ok := s.Import(job.ImportID)
	if !ok {
		return
	}
	batch, format, err := s.canonicalizeImport(context.Background(), item, sourcePath)
	if err != nil {
		s.failImport(jobID, err)
		return
	}

	s.mu.Lock()
	item = s.imports[item.ImportID]
	item.State = ImportQueued
	item.Format = format.String()
	s.imports[item.ImportID] = item
	s.workspace.Batches = append([]BatchSummary{batch}, s.workspace.Batches...)
	s.workspace.Dataset.BatchCount++
	s.workspace.Dataset.RecordCount += batch.RecordCount
	s.workspace.Dataset.FieldCount = max(s.workspace.Dataset.FieldCount, batch.FieldCount)
	s.workspace.Dataset.UpdatedAt = batch.AddedAt
	s.workspace.Dataset.State = "processing"
	job = s.jobs[jobID]
	job.State = JobQueued
	job.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	s.jobs[jobID] = job
	s.appendJobEventLocked(jobID, "job_queued", "", batch.RecordCount, batch.RecordCount, "Default workflow queued automatically.")
	persistErr := s.persistLocked()
	s.mu.Unlock()
	if persistErr != nil {
		s.failImport(jobID, persistErr)
		return
	}

	s.updateJobState(jobID, JobRunning, "stage_started", "raw", "Pipeline started.")
	response, err := s.Run(context.Background())
	if err != nil {
		s.failImport(jobID, err)
		return
	}
	s.mu.Lock()
	job = s.jobs[jobID]
	job.State = JobSucceeded
	job.RunID = response.RunID
	job.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	if len(s.workspace.Outputs) > 0 {
		job.OutputID = s.workspace.Outputs[0].ID
	}
	s.jobs[jobID] = job
	item = s.imports[job.ImportID]
	item.State = ImportSucceeded
	item.FinishedAt = job.UpdatedAt
	s.imports[item.ImportID] = item
	for _, stage := range s.workspace.Stages {
		s.appendJobEventLocked(jobID, "stage_committed", stage.ID, stage.Count, stage.InputCount, stage.Label+" committed.")
	}
	if s.workspace.DecisionBreakdown.Review > 0 {
		s.appendJobEventLocked(jobID, "review_required", "review", s.workspace.DecisionBreakdown.Review, s.workspace.DecisionBreakdown.Review, "Some ambiguous records are ready for human review.")
	}
	s.appendJobEventLocked(jobID, "output_ready", "curated", s.workspace.DecisionBreakdown.Accepted, s.workspace.Dataset.RecordCount, "Curated output is ready.")
	_ = s.persistLocked()
	s.mu.Unlock()
}

func (s *Store) canonicalizeImport(ctx context.Context, item ImportSummary, sourcePath string) (BatchSummary, InputFormat, error) {
	checksum, err := fileSHA256(sourcePath)
	if err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	source, err := os.Open(sourcePath)
	if err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	header := make([]byte, 4096)
	read, readErr := source.Read(header)
	if readErr != nil && !errors.Is(readErr, io.EOF) {
		source.Close()
		return BatchSummary{}, InputFormat{}, readErr
	}
	if _, err := source.Seek(0, io.SeekStart); err != nil {
		source.Close()
		return BatchSummary{}, InputFormat{}, err
	}
	format, err := DetectFormat(item.Filename, header[:read])
	if err != nil {
		source.Close()
		return BatchSummary{}, InputFormat{}, err
	}
	stream, err := OpenRecordStream(source, format)
	if err != nil {
		source.Close()
		return BatchSummary{}, InputFormat{}, err
	}
	defer stream.Close()
	defer source.Close()

	batchID := "batch_" + checksum[:16]
	target := filepath.Join(s.cfg.ArtifactsDir, "staged", batchID+".jsonl")
	temporary, err := os.CreateTemp(filepath.Dir(target), batchID+"-*.tmp")
	if err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	fields := make(map[string]struct{})
	count := 0
	ingestedAt := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	writer := bufio.NewWriterSize(temporary, 256*1024)
	encoder := json.NewEncoder(writer)
	for {
		row, err := stream.Next(ctx)
		if errors.Is(err, io.EOF) {
			break
		}
		if err != nil {
			writer.Flush()
			temporary.Close()
			return BatchSummary{}, InputFormat{}, err
		}
		for field := range row {
			fields[field] = struct{}{}
		}
		recordID := safeRecordID(asString(row["record_id"]))
		payload := row
		metadata := map[string]any{"filename": item.Filename, "format": format.String(), "source_checksum": checksum}
		if nested, nestedOK := row["payload"].(map[string]any); nestedOK {
			payload = nested
			if supplied, metadataOK := row["metadata"].(map[string]any); metadataOK {
				for key, value := range supplied {
					metadata[key] = value
				}
			}
		}
		if recordID == "" {
			recordID = fmt.Sprintf("rec_%s_%08d", checksum[:8], count)
		}
		envelope := dataRecord{
			RecordID: recordID, DatasetID: item.DatasetID, BatchID: batchID,
			IngestedAt: ingestedAt, Payload: payload, Metadata: metadata,
		}
		if err := encoder.Encode(envelope); err != nil {
			writer.Flush()
			temporary.Close()
			return BatchSummary{}, InputFormat{}, err
		}
		count++
	}
	if count == 0 {
		writer.Flush()
		temporary.Close()
		return BatchSummary{}, InputFormat{}, fmt.Errorf("%w: source contains no records", ErrInvalidImport)
	}
	if err := writer.Flush(); err != nil {
		temporary.Close()
		return BatchSummary{}, InputFormat{}, err
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return BatchSummary{}, InputFormat{}, err
	}
	if err := temporary.Close(); err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return BatchSummary{}, InputFormat{}, err
	}
	return BatchSummary{
		ID: batchID, Filename: item.Filename, AddedAt: ingestedAt, RecordCount: count,
		FieldCount: len(fields), State: "staged", Checksum: checksum,
	}, format, nil
}

func (s *Store) updateJobState(jobID string, state LifecycleState, eventType, stageID, message string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job := s.jobs[jobID]
	job.State = state
	job.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	s.jobs[jobID] = job
	item := s.imports[job.ImportID]
	item.State = state
	s.imports[item.ImportID] = item
	s.appendJobEventLocked(jobID, eventType, stageID, 0, 0, message)
	_ = s.persistLocked()
}

func (s *Store) failImport(jobID string, failure error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.jobs[jobID]
	if !ok {
		return
	}
	job.State = JobFailed
	job.Error = failure.Error()
	job.UpdatedAt = time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	s.jobs[jobID] = job
	item := s.imports[job.ImportID]
	item.State = ImportFailed
	item.Error = failure.Error()
	s.imports[item.ImportID] = item
	s.appendJobEventLocked(jobID, "job_failed", "", 0, 0, "Import failed: "+failure.Error())
	_ = s.persistLocked()
}

func (s *Store) appendJobEventLocked(jobID, eventType, stageID string, completed, total int, message string) {
	events := s.jobEvents[jobID]
	events = append(events, JobEvent{
		JobID: jobID, Sequence: uint64(len(events) + 1), EventType: eventType, StageID: stageID,
		CompletedRecords: completed, TotalRecords: total,
		Timestamp: time.Now().UTC().Truncate(time.Millisecond).Format(time.RFC3339Nano), Message: message,
	})
	s.jobEvents[jobID] = events
}

func (s *Store) incompleteUploadPath(uploadID string) string {
	return filepath.Join(s.cfg.ArtifactsDir, "uploads", "incomplete", uploadID+".part")
}

func (s *Store) completeUploadPath(item ImportSummary) string {
	return filepath.Join(s.cfg.ArtifactsDir, "uploads", "complete", item.UploadID+"-"+filepath.Base(item.Filename))
}

func canonicalRowFingerprint(row map[string]any) string {
	keys := make([]string, 0, len(row))
	for key := range row {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	hash := sha256.New()
	for _, key := range keys {
		_, _ = io.WriteString(hash, key)
		encoded, _ := json.Marshal(row[key])
		_, _ = hash.Write(encoded)
	}
	return hex.EncodeToString(hash.Sum(nil))
}

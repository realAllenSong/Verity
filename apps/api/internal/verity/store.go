package verity

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

var (
	ErrNotFound = errors.New("resource not found")
	ErrConflict = errors.New("resource conflict")
)

type StoreConfig struct {
	RepoRoot       string
	SeedWorkspace  string
	StatePath      string
	ArtifactsDir   string
	MaxUploadBytes int64
}

type ReviewEvent struct {
	RecordHash string `json:"record_hash,omitempty"`
	RecordID   string `json:"record_id"`
	Previous   string `json:"previous"`
	Decision   string `json:"decision"`
	Note       string `json:"note,omitempty"`
	Occurred   string `json:"occurred_at"`
	ActorType  string `json:"actor_type"`
}

type persistedState struct {
	Workspace    Workspace                `json:"workspace"`
	ReviewEvents []ReviewEvent            `json:"review_events"`
	Imports      map[string]ImportSummary `json:"imports,omitempty"`
	Jobs         map[string]JobSummary    `json:"jobs,omitempty"`
	JobEvents    map[string][]JobEvent    `json:"job_events,omitempty"`
	ImportKeys   map[string]string        `json:"import_keys,omitempty"`
}

type Store struct {
	mu           sync.RWMutex
	runGate      sync.Mutex
	cfg          StoreConfig
	engine       Engine
	workspace    Workspace
	reviewEvents []ReviewEvent
	imports      map[string]ImportSummary
	jobs         map[string]JobSummary
	jobEvents    map[string][]JobEvent
	importKeys   map[string]string
	running      bool
}

func NewStore(cfg StoreConfig, engine Engine) (*Store, error) {
	if cfg.MaxUploadBytes <= 0 {
		cfg.MaxUploadBytes = 5 << 30
	}
	store := &Store{cfg: cfg, engine: engine}
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o750); err != nil {
		return nil, fmt.Errorf("create control state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ArtifactsDir, "staged"), 0o750); err != nil {
		return nil, fmt.Errorf("create staged batch directory: %w", err)
	}
	for _, directory := range []string{
		filepath.Join(cfg.ArtifactsDir, "uploads", "incomplete"),
		filepath.Join(cfg.ArtifactsDir, "uploads", "complete"),
	} {
		if err := os.MkdirAll(directory, 0o750); err != nil {
			return nil, fmt.Errorf("create upload directory: %w", err)
		}
	}
	if err := store.load(); err != nil {
		return nil, err
	}
	return store, nil
}

func (s *Store) load() error {
	if data, err := os.ReadFile(s.cfg.StatePath); err == nil {
		var state persistedState
		if err := json.Unmarshal(data, &state); err != nil {
			return fmt.Errorf("decode control state: %w", err)
		}
		s.workspace = state.Workspace
		s.reviewEvents = state.ReviewEvents
		s.imports = state.Imports
		s.jobs = state.Jobs
		s.jobEvents = state.JobEvents
		s.importKeys = state.ImportKeys
		s.initializeLifecycleMaps()
		s.enrich(&s.workspace)
		return s.validateWorkspace(s.workspace)
	} else if !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("read control state: %w", err)
	}

	workspace, err := readWorkspace(s.cfg.SeedWorkspace)
	if err != nil {
		return fmt.Errorf("load seed workspace: %w", err)
	}
	s.enrich(&workspace)
	if err := s.validateWorkspace(workspace); err != nil {
		return err
	}
	s.workspace = workspace
	s.initializeLifecycleMaps()
	return s.persistLocked()
}

func (s *Store) initializeLifecycleMaps() {
	if s.imports == nil {
		s.imports = make(map[string]ImportSummary)
	}
	if s.jobs == nil {
		s.jobs = make(map[string]JobSummary)
	}
	if s.jobEvents == nil {
		s.jobEvents = make(map[string][]JobEvent)
	}
	if s.importKeys == nil {
		s.importKeys = make(map[string]string)
	}
}

func (s *Store) Workspace() Workspace {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return cloneWorkspace(s.workspace)
}

func (s *Store) ETag() string {
	s.mu.RLock()
	defer s.mu.RUnlock()
	data, _ := json.Marshal(s.workspace)
	digest := sha256.Sum256(data)
	return `"` + hex.EncodeToString(digest[:12]) + `"`
}

func (s *Store) ReviewQueue() (int, []EvidenceRecord) {
	total, records, _ := s.ReviewPage(context.Background(), 200)
	return total, records
}

func (s *Store) UpdateReview(recordID string, update ReviewUpdate) (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if update.Decision != "accepted" && update.Decision != "rejected" && update.Decision != "modified" {
		return Workspace{}, ErrInvalidImport
	}
	row, lookupErr := s.findReviewRecordLocked(recordID)
	if lookupErr == nil {
		previous := row.Decision
		if event, ok := matchingReview(row, latestReviews(s.reviewEvents)); ok {
			previous = event.Decision
		}
		if previous == update.Decision && strings.TrimSpace(update.Note) == "" {
			return cloneWorkspace(s.workspace), nil
		}
		events := append(append([]ReviewEvent(nil), s.reviewEvents...), ReviewEvent{
			RecordID: recordID, RecordHash: reviewHash(row), Previous: previous, Decision: update.Decision,
			Note: strings.TrimSpace(update.Note), Occurred: time.Now().UTC().Format(time.RFC3339Nano), ActorType: "local_reviewer",
		})
		fresh, err := s.projectReviewsLocked(cloneWorkspace(s.workspace), events)
		if err != nil {
			return Workspace{}, err
		}
		before, oldEvents := s.workspace, s.reviewEvents
		s.workspace, s.reviewEvents = fresh, events
		if err := s.persistLocked(); err != nil {
			s.workspace, s.reviewEvents = before, oldEvents
			return Workspace{}, err
		}
		return cloneWorkspace(fresh), nil
	}
	if !errors.Is(lookupErr, os.ErrNotExist) {
		return Workspace{}, lookupErr
	}
	index := -1
	for i := range s.workspace.Records {
		if s.workspace.Records[i].ID == recordID {
			index = i
			break
		}
	}
	if index < 0 {
		return Workspace{}, ErrNotFound
	}
	previous := s.workspace.Records[index].Decision
	if previous == update.Decision && strings.TrimSpace(update.Note) == "" {
		return cloneWorkspace(s.workspace), nil
	}
	adjustDecision(&s.workspace.DecisionBreakdown, previous, -1)
	adjustDecision(&s.workspace.DecisionBreakdown, update.Decision, 1)
	s.workspace.Records[index].Decision = update.Decision
	if note := strings.TrimSpace(update.Note); note != "" {
		s.workspace.Records[index].Reason = note
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	s.reviewEvents = append(s.reviewEvents, ReviewEvent{
		RecordID: recordID, Previous: previous, Decision: update.Decision,
		Note: strings.TrimSpace(update.Note), Occurred: now, ActorType: "local_reviewer",
	})
	if err := s.persistLocked(); err != nil {
		return Workspace{}, err
	}
	if err := s.writeReviewProjectionLocked(); err != nil {
		return Workspace{}, err
	}
	return cloneWorkspace(s.workspace), nil
}

func (s *Store) StageBatch(
	datasetID string,
	request BatchCreate,
	idempotencyKey string,
) (BatchStageResponse, bool, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if datasetID != s.workspace.Dataset.ID {
		return BatchStageResponse{}, false, ErrNotFound
	}
	canonical, err := json.Marshal(struct {
		DatasetID      string                   `json:"dataset_id"`
		Filename       string                   `json:"filename"`
		Records        []map[string]interface{} `json:"records"`
		IdempotencyKey string                   `json:"idempotency_key,omitempty"`
	}{datasetID, filepath.Base(request.Filename), request.Records, idempotencyKey})
	if err != nil {
		return BatchStageResponse{}, false, fmt.Errorf("encode batch: %w", err)
	}
	digest := sha256.Sum256(canonical)
	checksum := hex.EncodeToString(digest[:])
	batchID := "batch_" + checksum[:16]
	fields := fieldsFor(request.Records)
	for _, existing := range s.workspace.Batches {
		if existing.ID == batchID {
			return BatchStageResponse{
				Batch: existing, SampleFields: fields,
				Message: "Batch already staged. The idempotency key prevented a duplicate.",
			}, true, nil
		}
	}

	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	batch := BatchSummary{
		ID: batchID, Filename: filepath.Base(request.Filename), AddedAt: now,
		RecordCount: len(request.Records), FieldCount: len(fields), State: "staged",
		Checksum: checksum,
	}
	if err := s.writeBatch(batch, request.Records); err != nil {
		return BatchStageResponse{}, false, err
	}
	s.workspace.Batches = append([]BatchSummary{batch}, s.workspace.Batches...)
	s.workspace.Dataset.BatchCount++
	s.workspace.Dataset.RecordCount += len(request.Records)
	s.workspace.Dataset.UpdatedAt = now
	s.workspace.Dataset.State = "attention"
	if err := s.persistLocked(); err != nil {
		return BatchStageResponse{}, false, err
	}
	return BatchStageResponse{
		Batch: batch, SampleFields: fields,
		Message: "Batch staged. Review field mapping before the next run.",
	}, false, nil
}

func (s *Store) Run(ctx context.Context) (RunResponse, error) {
	return s.run(ctx, nil)
}

func (s *Store) run(ctx context.Context, onStage func(StageProgress)) (RunResponse, error) {
	s.runGate.Lock()
	defer s.runGate.Unlock()
	s.mu.Lock()
	if s.running {
		s.mu.Unlock()
		return RunResponse{}, ErrConflict
	}
	s.running = true
	now := time.Now().UTC()
	runID := fmt.Sprintf("run_%s_%06d", now.Format("2006_01_02_150405"), now.Nanosecond()/1_000)
	now = now.Truncate(time.Second)
	startEvent := RunEvent{RunID: runID, EventType: "START", EventTime: now.Format(time.RFC3339), Job: s.workspace.Recipe.ID}
	s.workspace.RunEvents = append([]RunEvent{startEvent}, s.workspace.RunEvents...)
	if err := s.persistLocked(); err != nil {
		s.running = false
		s.mu.Unlock()
		return RunResponse{}, fmt.Errorf("persist run start: %w", err)
	}
	s.mu.Unlock()

	var err error
	if engine, ok := s.engine.(progressEngine); ok {
		err = engine.RunWithProgress(ctx, runID, onStage)
	} else {
		err = s.engine.Run(ctx, runID)
	}
	finished := time.Now().UTC().Truncate(time.Second)

	s.mu.Lock()
	defer s.mu.Unlock()
	defer func() { s.running = false }()
	if err != nil {
		s.workspace.RunEvents = append([]RunEvent{{
			RunID: runID, EventType: "FAIL", EventTime: finished.Format(time.RFC3339),
			Job: s.workspace.Recipe.ID, Message: "The data-plane engine did not complete.",
		}}, s.workspace.RunEvents...)
		s.workspace.Runs = append([]RunSummary{{
			ID: runID, RecipeVersion: s.workspace.Recipe.Version,
			StartedAt: now.Format(time.RFC3339), DurationSeconds: int(finished.Sub(now).Seconds()),
			RecordCount: s.workspace.Dataset.RecordCount, State: "failed", Attempt: 1,
			FailureReason: "data-plane execution failed",
		}}, s.workspace.Runs...)
		_ = s.persistLocked()
		return RunResponse{}, err
	}
	workspacePath := filepath.Join(s.cfg.ArtifactsDir, runID, "workspace.json")
	fresh, err := readWorkspace(workspacePath)
	if err != nil {
		s.workspace.RunEvents = append([]RunEvent{{
			RunID: runID, EventType: "FAIL", EventTime: finished.Format(time.RFC3339),
			Job: s.workspace.Recipe.ID, Message: "The completed workspace could not be loaded.",
		}}, s.workspace.RunEvents...)
		_ = s.persistLocked()
		return RunResponse{}, fmt.Errorf("load completed run: %w", err)
	}
	for index := range fresh.Runs {
		if fresh.Runs[index].ID == runID {
			fresh.Runs[index].DurationSeconds = max(1, int(finished.Sub(now).Seconds()))
			break
		}
	}
	oldEvents := s.workspace.RunEvents
	previousBatches := append([]BatchSummary(nil), s.workspace.Batches...)
	s.enrich(&fresh)
	mergeMissingBatches(&fresh, previousBatches, s.cfg.ArtifactsDir)
	fresh, err = s.projectReviewsLocked(fresh, s.reviewEvents)
	if err != nil {
		return RunResponse{}, err
	}
	fresh.RunEvents = append([]RunEvent{{
		RunID: runID, EventType: "COMPLETE", EventTime: finished.Format(time.RFC3339),
		Job: fresh.Recipe.ID,
	}}, oldEvents...)
	if len(fresh.RunEvents) > 80 {
		fresh.RunEvents = fresh.RunEvents[:80]
	}
	s.workspace = fresh
	if err := s.persistLocked(); err != nil {
		return RunResponse{}, err
	}
	counts := make(map[string]int, len(fresh.Stages))
	for _, stage := range fresh.Stages {
		counts[stage.ID] = stage.Count
	}
	return RunResponse{RunID: runID, State: "succeeded", StageCounts: counts}, nil
}

func mergeMissingBatches(workspace *Workspace, previous []BatchSummary, artifactsDir string) {
	present := make(map[string]struct{}, len(workspace.Batches))
	for _, batch := range workspace.Batches {
		present[batch.ID] = struct{}{}
	}
	for _, batch := range previous {
		if _, exists := present[batch.ID]; exists {
			continue
		}
		if _, err := os.Stat(filepath.Join(artifactsDir, "staged", batch.ID+".jsonl")); err != nil {
			continue
		}
		workspace.Batches = append(workspace.Batches, batch)
		workspace.Dataset.RecordCount += batch.RecordCount
		workspace.Dataset.FieldCount = max(workspace.Dataset.FieldCount, batch.FieldCount)
	}
	workspace.Dataset.BatchCount = len(workspace.Batches)
}

func applyReviewOverrides(workspace *Workspace, events []ReviewEvent) {
	latest := make(map[string]ReviewEvent, len(events))
	for _, event := range events {
		latest[event.RecordID] = event
	}
	for index := range workspace.Records {
		event, exists := latest[workspace.Records[index].ID]
		if !exists {
			continue
		}
		previous := workspace.Records[index].Decision
		if previous != event.Decision {
			adjustDecision(&workspace.DecisionBreakdown, previous, -1)
			adjustDecision(&workspace.DecisionBreakdown, event.Decision, 1)
		}
		workspace.Records[index].Decision = event.Decision
		if event.Note != "" {
			workspace.Records[index].Reason = event.Note
		}
	}
}

func (s *Store) Preview(ctx context.Context, stageID string, limit int) ([]json.RawMessage, error) {
	s.mu.RLock()
	runID := s.workspace.RunID
	found := false
	for _, stage := range s.workspace.Stages {
		if stage.ID == stageID {
			found = true
			break
		}
	}
	s.mu.RUnlock()
	if !found {
		return nil, ErrNotFound
	}
	return s.engine.Preview(ctx, runID, stageID, limit)
}

func (s *Store) Compare(ctx context.Context, stageID string, limit int) (StageComparisonResponse, error) {
	s.mu.RLock()
	runID := s.workspace.RunID
	var selected *PipelineStage
	for index := range s.workspace.Stages {
		if s.workspace.Stages[index].ID == stageID {
			stage := s.workspace.Stages[index]
			selected = &stage
			break
		}
	}
	s.mu.RUnlock()
	if selected == nil {
		return StageComparisonResponse{}, ErrNotFound
	}
	samples, err := s.engine.Compare(ctx, runID, stageID, limit)
	if err != nil {
		return StageComparisonResponse{}, err
	}
	removed := max(0, selected.InputCount-selected.Count)
	if stageID == "raw" || stageID == "review" || stageID == "curated" {
		removed = 0
	}
	return StageComparisonResponse{
		StageID: stageID, PreviousStage: previousStage[stageID], InputCount: selected.InputCount,
		OutputCount: selected.Count, RemovedCount: removed, Samples: samples,
	}, nil
}

func (s *Store) IsReady() error {
	s.mu.RLock()
	defer s.mu.RUnlock()
	if err := s.validateWorkspace(s.workspace); err != nil {
		return err
	}
	testPath := filepath.Join(filepath.Dir(s.cfg.StatePath), ".write-check")
	if err := os.WriteFile(testPath, []byte("ok"), 0o600); err != nil {
		return fmt.Errorf("control state is not writable: %w", err)
	}
	if err := os.Remove(testPath); err != nil {
		return fmt.Errorf("remove readiness probe: %w", err)
	}
	return nil
}

func (s *Store) persistLocked() error {
	return writeJSONAtomic(s.cfg.StatePath, persistedState{
		Workspace: s.workspace, ReviewEvents: s.reviewEvents, Imports: s.imports,
		Jobs: s.jobs, JobEvents: s.jobEvents, ImportKeys: s.importKeys,
	})
}

func randomID(prefix string) (string, error) {
	buffer := make([]byte, 12)
	if _, err := rand.Read(buffer); err != nil {
		return "", err
	}
	return prefix + hex.EncodeToString(buffer), nil
}

func (s *Store) writeBatch(batch BatchSummary, records []map[string]interface{}) error {
	target := filepath.Join(s.cfg.ArtifactsDir, "staged", batch.ID+".jsonl")
	temporary, err := os.CreateTemp(filepath.Dir(target), batch.ID+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create staged batch: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	encoder := json.NewEncoder(temporary)
	for index, uploaded := range records {
		payload := uploaded
		metadata := map[string]interface{}{
			"filename": batch.Filename, "state": "staged", "checksum": batch.Checksum,
		}
		recordID := fmt.Sprintf("rec_%s_%06d", batch.ID[6:14], index)
		if nested, ok := uploaded["payload"].(map[string]interface{}); ok {
			payload = nested
			if provided := safeRecordID(asString(uploaded["record_id"])); provided != "" {
				recordID = provided
			}
			if uploadedMetadata, ok := uploaded["metadata"].(map[string]interface{}); ok {
				for key, value := range uploadedMetadata {
					metadata[key] = value
				}
			}
		} else if provided := safeRecordID(asString(uploaded["record_id"])); provided != "" {
			recordID = provided
			payload = cloneAnyMap(uploaded)
			delete(payload, "record_id")
		}
		envelope := map[string]interface{}{
			"record_id":   recordID,
			"dataset_id":  s.workspace.Dataset.ID,
			"batch_id":    batch.ID,
			"ingested_at": batch.AddedAt,
			"payload":     payload,
			"metadata":    metadata,
		}
		if err := encoder.Encode(envelope); err != nil {
			temporary.Close()
			return fmt.Errorf("encode staged batch: %w", err)
		}
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync staged batch: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close staged batch: %w", err)
	}
	if err := os.Chmod(temporaryName, 0o600); err != nil {
		return fmt.Errorf("protect staged batch: %w", err)
	}
	if err := os.Rename(temporaryName, target); err != nil {
		return fmt.Errorf("commit staged batch: %w", err)
	}
	return nil
}

func safeRecordID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 200 || strings.ContainsAny(value, "\r\n\t/\\") {
		return ""
	}
	return value
}

func (s *Store) writeReviewProjectionLocked() error {
	path := filepath.Join(s.cfg.ArtifactsDir, s.workspace.RunID, "review-overrides.jsonl")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()
	encoder := json.NewEncoder(file)
	for _, event := range s.reviewEvents {
		if err := encoder.Encode(event); err != nil {
			return err
		}
	}
	return file.Sync()
}

func (s *Store) enrich(workspace *Workspace) {
	if workspace.Dataset.State == "" {
		workspace.Dataset.State = "ready"
	}
	if workspace.Dataset.SchemaContract.Columns == "" {
		workspace.Dataset.SchemaContract = SchemaContract{
			Columns: "evolve", DataTypes: "freeze", OnViolation: "quarantine row",
		}
	}
	for index := range workspace.Stages {
		stage := &workspace.Stages[index]
		if stage.Status == "" {
			stage.Status = "complete"
		}
		if len(stage.Checks) == 0 {
			stage.Checks = defaultChecks(*stage)
		}
	}
	if len(workspace.RunEvents) == 0 && workspace.RunID != "" {
		workspace.RunEvents = []RunEvent{{
			RunID: workspace.RunID, EventType: "COMPLETE", EventTime: workspace.GeneratedAt,
			Job: workspace.Recipe.ID,
		}}
	}
}

func (s *Store) validateWorkspace(workspace Workspace) error {
	if workspace.Dataset.ID == "" || workspace.RunID == "" || len(workspace.Stages) == 0 {
		return errors.New("workspace is missing dataset, run, or stage identity")
	}
	return nil
}

func defaultChecks(stage PipelineStage) []QualityCheck {
	retention := 100.0
	if stage.InputCount > 0 {
		retention = 100 * float64(stage.Count) / float64(stage.InputCount)
	}
	checks := []QualityCheck{{
		ID: stage.ID + "_artifact", Label: "Artifact readable", State: "passed",
		Severity: "blocking", Observed: fmt.Sprintf("%d records", stage.Count),
	}}
	if stage.ID != "raw" {
		checks = append(checks, QualityCheck{
			ID: stage.ID + "_retention", Label: "Retention accounted for", State: "passed",
			Severity: "warning", Observed: fmt.Sprintf("%.1f%% retained", retention),
		})
	}
	return checks
}

func fieldsFor(records []map[string]interface{}) []string {
	set := make(map[string]struct{})
	for _, record := range records {
		for field := range record {
			set[field] = struct{}{}
		}
	}
	fields := make([]string, 0, len(set))
	for field := range set {
		fields = append(fields, field)
	}
	sort.Strings(fields)
	if len(fields) > 12 {
		fields = fields[:12]
	}
	return fields
}

func adjustDecision(breakdown *DecisionBreakdown, decision string, delta int) {
	switch decision {
	case "accepted", "modified":
		breakdown.Accepted += delta
	case "rejected":
		breakdown.Rejected += delta
	case "review":
		breakdown.Review += delta
	}
}

func readWorkspace(path string) (Workspace, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Workspace{}, err
	}
	var workspace Workspace
	if err := json.Unmarshal(data, &workspace); err != nil {
		return Workspace{}, err
	}
	return workspace, nil
}

func cloneWorkspace(workspace Workspace) Workspace {
	data, _ := json.Marshal(workspace)
	var clone Workspace
	_ = json.Unmarshal(data, &clone)
	return clone
}

func writeJSONAtomic(path string, value interface{}) error {
	data, err := json.MarshalIndent(value, "", "  ")
	if err != nil {
		return fmt.Errorf("encode state: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		return fmt.Errorf("create state directory: %w", err)
	}
	temporary, err := os.CreateTemp(filepath.Dir(path), filepath.Base(path)+"-*.tmp")
	if err != nil {
		return fmt.Errorf("create temporary state: %w", err)
	}
	temporaryName := temporary.Name()
	defer os.Remove(temporaryName)
	if err := temporary.Chmod(0o600); err != nil {
		temporary.Close()
		return err
	}
	if _, err := temporary.Write(append(data, '\n')); err != nil {
		temporary.Close()
		return fmt.Errorf("write state: %w", err)
	}
	if err := temporary.Sync(); err != nil {
		temporary.Close()
		return fmt.Errorf("sync state: %w", err)
	}
	if err := temporary.Close(); err != nil {
		return fmt.Errorf("close state: %w", err)
	}
	if err := os.Rename(temporaryName, path); err != nil {
		return fmt.Errorf("commit state: %w", err)
	}
	return nil
}

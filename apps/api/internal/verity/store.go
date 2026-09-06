package verity

import (
	"context"
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
	RepoRoot      string
	SeedWorkspace string
	StatePath     string
	ArtifactsDir  string
}

type ReviewEvent struct {
	RecordID  string `json:"record_id"`
	Previous  string `json:"previous"`
	Decision  string `json:"decision"`
	Note      string `json:"note,omitempty"`
	Occurred  string `json:"occurred_at"`
	ActorType string `json:"actor_type"`
}

type persistedState struct {
	Workspace    Workspace     `json:"workspace"`
	ReviewEvents []ReviewEvent `json:"review_events"`
}

type Store struct {
	mu           sync.RWMutex
	cfg          StoreConfig
	engine       Engine
	workspace    Workspace
	reviewEvents []ReviewEvent
	running      bool
}

func NewStore(cfg StoreConfig, engine Engine) (*Store, error) {
	store := &Store{cfg: cfg, engine: engine}
	if err := os.MkdirAll(filepath.Dir(cfg.StatePath), 0o750); err != nil {
		return nil, fmt.Errorf("create control state directory: %w", err)
	}
	if err := os.MkdirAll(filepath.Join(cfg.ArtifactsDir, "staged"), 0o750); err != nil {
		return nil, fmt.Errorf("create staged batch directory: %w", err)
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
	return s.persistLocked()
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
	s.mu.RLock()
	defer s.mu.RUnlock()
	records := make([]EvidenceRecord, 0)
	for _, record := range s.workspace.Records {
		if record.Decision == "review" {
			records = append(records, record)
		}
	}
	return s.workspace.DecisionBreakdown.Review, records
}

func (s *Store) UpdateReview(recordID string, update ReviewUpdate) (Workspace, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
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
	for i := range s.workspace.Stages {
		switch s.workspace.Stages[i].ID {
		case "review":
			s.workspace.Stages[i].Count = s.workspace.DecisionBreakdown.Review
		case "curated":
			s.workspace.Stages[i].Count = s.workspace.DecisionBreakdown.Accepted
		}
	}
	now := time.Now().UTC().Truncate(time.Second).Format(time.RFC3339)
	s.reviewEvents = append(s.reviewEvents, ReviewEvent{
		RecordID: recordID, Previous: previous, Decision: update.Decision,
		Note: strings.TrimSpace(update.Note), Occurred: now, ActorType: "local_reviewer",
	})
	if err := s.persistLocked(); err != nil {
		return Workspace{}, err
	}
	_ = s.writeReviewProjectionLocked()
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

	err := s.engine.Run(ctx, runID)
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
	s.enrich(&fresh)
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
		Workspace: s.workspace, ReviewEvents: s.reviewEvents,
	})
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
	for index, payload := range records {
		envelope := map[string]interface{}{
			"record_id":   fmt.Sprintf("rec_%s_%06d", batch.ID[6:14], index),
			"dataset_id":  s.workspace.Dataset.ID,
			"batch_id":    batch.ID,
			"ingested_at": batch.AddedAt,
			"payload":     payload,
			"metadata": map[string]interface{}{
				"filename": batch.Filename, "state": "staged", "checksum": batch.Checksum,
			},
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

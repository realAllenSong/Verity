package verity

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type fakeEngine struct {
	root string
	seed string
	fail bool
}

func (f fakeEngine) Run(_ context.Context, runID string) error {
	if f.fail {
		return context.DeadlineExceeded
	}
	workspace, err := readWorkspace(f.seed)
	if err != nil {
		return err
	}
	workspace.RunID = runID
	workspace.GeneratedAt = time.Now().UTC().Format(time.RFC3339)
	return writeJSONAtomic(filepath.Join(f.root, "artifacts", runID, "workspace.json"), workspace)
}

func (f fakeEngine) Preview(_ context.Context, _, _ string, limit int) ([]json.RawMessage, error) {
	rows := []json.RawMessage{json.RawMessage(`{"record_id":"rec_1"}`), json.RawMessage(`{"record_id":"rec_2"}`)}
	if limit < len(rows) {
		return rows[:limit], nil
	}
	return rows, nil
}

func (f fakeEngine) Compare(_ context.Context, _, _ string, _ int) ([]StageComparisonSample, error) {
	return []StageComparisonSample{{RecordID: "rec_1", Outcome: "kept", After: json.RawMessage(`{"record_id":"rec_1"}`)}}, nil
}

func testServer(t *testing.T, token string) (*Store, http.Handler, StoreConfig, fakeEngine) {
	t.Helper()
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	temporary := t.TempDir()
	artifacts := filepath.Join(temporary, "artifacts")
	engine := fakeEngine{
		root: temporary,
		seed: filepath.Join(repoRoot, "apps", "web", "src", "data", "demo-workspace.json"),
	}
	cfg := StoreConfig{
		RepoRoot:      temporary,
		SeedWorkspace: engine.seed,
		StatePath:     filepath.Join(artifacts, "control", "state.json"),
		ArtifactsDir:  artifacts,
	}
	store, err := NewStore(cfg, engine)
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	handler := NewHandler(store, HTTPConfig{
		APIToken:       token,
		AllowedOrigin:  map[string]struct{}{"http://127.0.0.1:3000": {}},
		OpenAPIPath:    filepath.Join(repoRoot, "packages", "contracts", "openapi.json"),
		RequestTimeout: 5 * time.Second,
	}, logger)
	return store, handler, cfg, engine
}

func TestWorkspaceHealthAndConditionalRead(t *testing.T) {
	_, handler, _, _ := testServer(t, "")
	health := httptest.NewRecorder()
	handler.ServeHTTP(health, httptest.NewRequest(http.MethodGet, "/health", nil))
	if health.Code != http.StatusOK || !strings.Contains(health.Body.String(), `"control_plane":"go"`) {
		t.Fatalf("unexpected health response: %d %s", health.Code, health.Body.String())
	}

	first := httptest.NewRecorder()
	handler.ServeHTTP(first, httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil))
	if first.Code != http.StatusOK || first.Header().Get("ETag") == "" {
		t.Fatalf("unexpected workspace response: %d", first.Code)
	}
	secondRequest := httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil)
	secondRequest.Header.Set("If-None-Match", first.Header().Get("ETag"))
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, secondRequest)
	if second.Code != http.StatusNotModified {
		t.Fatalf("expected 304, got %d", second.Code)
	}
}

func TestOutputDownloadIncludesVerifiableChecksum(t *testing.T) {
	store, handler, cfg, _ := testServer(t, "")
	workspace := store.Workspace()
	if len(workspace.Outputs) == 0 {
		t.Fatal("seed workspace has no outputs")
	}
	outputID := "out_ready_parquet"
	path := filepath.Join(cfg.ArtifactsDir, workspace.RunID, "curated.parquet")
	if err := os.MkdirAll(filepath.Dir(path), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("curated bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/api/v1/outputs/"+outputID, nil))
	if response.Code != http.StatusOK {
		t.Fatalf("download returned %d: %s", response.Code, response.Body.String())
	}
	if got := response.Header().Get("X-Verity-SHA256"); got != "e7b66d8c49422d62ce61f92a7e677508bf600bc015eae96ec4c91d0108d2dfde" {
		t.Fatalf("checksum header = %q", got)
	}
}

func TestBatchStagingIsIdempotentAndPersistent(t *testing.T) {
	_, handler, cfg, engine := testServer(t, "")
	body := `{"filename":"measurements.json","records":[{"timestamp":"2026-09-04T12:00:00Z","value":42}]}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/workflow-signals/batches", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", "upload-42")
	first := httptest.NewRecorder()
	handler.ServeHTTP(first, request)
	if first.Code != http.StatusCreated {
		t.Fatalf("expected 201, got %d: %s", first.Code, first.Body.String())
	}

	retry := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/workflow-signals/batches", strings.NewReader(body))
	retry.Header.Set("Content-Type", "application/json")
	retry.Header.Set("Idempotency-Key", "upload-42")
	second := httptest.NewRecorder()
	handler.ServeHTTP(second, retry)
	if second.Code != http.StatusOK || !strings.Contains(second.Body.String(), "prevented a duplicate") {
		t.Fatalf("unexpected retry: %d %s", second.Code, second.Body.String())
	}

	reloaded, err := NewStore(cfg, engine)
	if err != nil {
		t.Fatal(err)
	}
	if got := reloaded.Workspace().Dataset.BatchCount; got != 7 {
		t.Fatalf("expected 7 batches after restart, got %d", got)
	}
}

func TestReviewDecisionPersists(t *testing.T) {
	store, handler, cfg, engine := testServer(t, "")
	stagesBefore := store.Workspace().Stages
	_, records := store.ReviewQueue()
	if len(records) == 0 {
		t.Fatal("seed workspace has no sampled review records")
	}
	path := "/api/v1/reviews/" + records[0].ID
	request := httptest.NewRequest(http.MethodPatch, path, strings.NewReader(`{"decision":"accepted","note":"Verified against the local source."}`))
	request.Header.Set("Content-Type", "application/json")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected review response: %d %s", response.Code, response.Body.String())
	}
	reloaded, err := NewStore(cfg, engine)
	if err != nil {
		t.Fatal(err)
	}
	if reloaded.Workspace().DecisionBreakdown.Review != store.Workspace().DecisionBreakdown.Review {
		t.Fatal("review count did not survive restart")
	}
	for index, stage := range store.Workspace().Stages {
		if stage.Count != stagesBefore[index].Count {
			t.Fatal("review override mutated an immutable run-stage count")
		}
	}
	resolvedCount := store.Workspace().DecisionBreakdown.Review
	if _, err := store.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	if store.Workspace().DecisionBreakdown.Review != resolvedCount {
		t.Fatal("review override disappeared after the next pipeline run")
	}
}

func TestAuthenticationValidationAndPreview(t *testing.T) {
	_, handler, _, _ := testServer(t, "test-token")
	unauthorized := httptest.NewRecorder()
	handler.ServeHTTP(unauthorized, httptest.NewRequest(http.MethodGet, "/api/v1/workspace", nil))
	if unauthorized.Code != http.StatusUnauthorized || unauthorized.Header().Get("Content-Type") != "application/problem+json" {
		t.Fatalf("unexpected unauthorized response: %d", unauthorized.Code)
	}

	request := httptest.NewRequest(http.MethodGet, "/api/v1/stages/signals/preview?limit=1", nil)
	request.Header.Set("Authorization", "Bearer test-token")
	preview := httptest.NewRecorder()
	handler.ServeHTTP(preview, request)
	if preview.Code != http.StatusOK || !strings.Contains(preview.Body.String(), `"count":1`) {
		t.Fatalf("unexpected preview: %d %s", preview.Code, preview.Body.String())
	}

	comparisonRequest := httptest.NewRequest(http.MethodGet, "/api/v1/stages/signals/comparison?limit=8", nil)
	comparisonRequest.Header.Set("Authorization", "Bearer test-token")
	comparison := httptest.NewRecorder()
	handler.ServeHTTP(comparison, comparisonRequest)
	if comparison.Code != http.StatusOK || !strings.Contains(comparison.Body.String(), `"previous_stage_id":"quality"`) {
		t.Fatalf("unexpected comparison: %d %s", comparison.Code, comparison.Body.String())
	}

	invalid := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/workflow-signals/batches", strings.NewReader(`{"filename":"x","records":[],"unknown":true}`))
	invalid.Header.Set("Authorization", "Bearer test-token")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected strict JSON validation, got %d", invalidResponse.Code)
	}
}

func TestPortableEnvelopeStagingPreservesIdentityAndMetadata(t *testing.T) {
	store, _, cfg, _ := testServer(t, "")
	response, _, err := store.StageBatch("workflow-signals", BatchCreate{
		Filename: "portable.json",
		Records: []map[string]interface{}{{
			"record_id": "portable_evt_001",
			"payload":   map[string]interface{}{"message": "Asked in Slack for the approved internal API client."},
			"metadata":  map[string]interface{}{"evidence_count": float64(4)},
		}},
	}, "portable-envelope")
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(cfg.ArtifactsDir, "staged", response.Batch.ID+".jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(data)
	if !strings.Contains(text, `"record_id":"portable_evt_001"`) || !strings.Contains(text, `"evidence_count":4`) {
		t.Fatalf("portable envelope fields were not preserved: %s", text)
	}
}

func TestRunLifecycleCompletes(t *testing.T) {
	_, handler, _, _ := testServer(t, "")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil))
	if response.Code != http.StatusOK || !strings.Contains(response.Body.String(), `"state":"succeeded"`) {
		t.Fatalf("unexpected run response: %d %s", response.Code, response.Body.String())
	}
}

func TestRunFailureIsRecordedWithoutPublishingSuccess(t *testing.T) {
	store, handler, _, _ := testServer(t, "")
	store.engine = fakeEngine{fail: true}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodPost, "/api/v1/runs", nil))
	if response.Code != http.StatusInternalServerError {
		t.Fatalf("expected 500, got %d: %s", response.Code, response.Body.String())
	}
	workspace := store.Workspace()
	if workspace.RunEvents[0].EventType != "FAIL" || workspace.Runs[0].State != "failed" {
		t.Fatalf("failure lifecycle was not persisted: %#v %#v", workspace.RunEvents[0], workspace.Runs[0])
	}
}

func TestOpenAPIContainsEveryPublicOperation(t *testing.T) {
	repoRoot, err := filepath.Abs(filepath.Join("..", "..", "..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(repoRoot, "packages", "contracts", "openapi.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !json.Valid(data) {
		t.Fatal("OpenAPI contract is not valid JSON")
	}
	for _, path := range []string{
		"/health", "/ready", "/api/v1/workspace", "/api/v1/runs",
		"/api/v1/imports", "/api/v1/uploads/{upload_id}",
		"/api/v1/imports/{import_id}/complete", "/api/v1/jobs/{job_id}",
		"/api/v1/jobs/{job_id}/events", "/api/v1/stages/{stage_id}/records",
		"/api/v1/outputs/{output_id}",
		"/api/v1/datasets/{dataset_id}/batches", "/api/v1/review-queue",
		"/api/v1/reviews/{record_id}", "/api/v1/stages/{stage_id}/preview",
		"/api/v1/stages/{stage_id}/comparison",
		"/api/v1/stages/{stage_id}/table",
		"/api/v1/integrations/airbyte/syncs", "/api/v1/integrations/airbyte/jobs/{job_id}",
	} {
		if !strings.Contains(string(data), `"`+path+`"`) {
			t.Fatalf("OpenAPI contract is missing %s", path)
		}
	}
}

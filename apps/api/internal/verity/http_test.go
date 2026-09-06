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

	invalid := httptest.NewRequest(http.MethodPost, "/api/v1/datasets/workflow-signals/batches", strings.NewReader(`{"filename":"x","records":[],"unknown":true}`))
	invalid.Header.Set("Authorization", "Bearer test-token")
	invalidResponse := httptest.NewRecorder()
	handler.ServeHTTP(invalidResponse, invalid)
	if invalidResponse.Code != http.StatusBadRequest {
		t.Fatalf("expected strict JSON validation, got %d", invalidResponse.Code)
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
	for _, path := range []string{
		"/health", "/ready", "/api/v1/workspace", "/api/v1/runs",
		"/api/v1/datasets/{dataset_id}/batches", "/api/v1/review-queue",
		"/api/v1/reviews/{record_id}", "/api/v1/stages/{stage_id}/preview",
	} {
		if !strings.Contains(string(data), `"`+path+`"`) {
			t.Fatalf("OpenAPI contract is missing %s", path)
		}
	}
}

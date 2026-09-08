package verity

import (
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"go.temporal.io/sdk/testsuite"
)

func TestAirbyteAdapterTriggersAndReadsJobs(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.Header.Get("Authorization") != "Bearer secret" {
			t.Fatal("Airbyte bearer token was not forwarded")
		}
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/v1/jobs":
			var body map[string]string
			if err := json.NewDecoder(request.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["connectionId"] != "connection-42" || body["jobType"] != "sync" {
				t.Fatalf("unexpected sync body: %#v", body)
			}
			writeJSON(response, http.StatusOK, map[string]any{"jobId": 42, "status": "running", "jobType": "sync"})
		case request.Method == http.MethodGet && request.URL.Path == "/v1/jobs/42":
			writeJSON(response, http.StatusOK, map[string]any{"jobId": 42, "status": "succeeded", "jobType": "sync"})
		default:
			http.NotFound(response, request)
		}
	}))
	defer server.Close()
	client, err := NewAirbyteClient(server.URL+"/v1", "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	started, err := client.TriggerSync(context.Background(), "connection-42")
	if err != nil || started.JobID != 42 || started.Status != "running" {
		t.Fatalf("unexpected started job: %#v, %v", started, err)
	}
	finished, err := client.Job(context.Background(), 42)
	if err != nil || finished.Status != "succeeded" {
		t.Fatalf("unexpected finished job: %#v, %v", finished, err)
	}
}

func TestAirbyteRoutesAreOptional(t *testing.T) {
	_, handler, _, _ := testServer(t, "")
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/airbyte/syncs", nil)
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("expected optional integration response, got %d", response.Code)
	}
}

func TestAirbyteHTTPRoutesProxyOnlyJobMetadata(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{"jobId": 91, "status": "running", "jobType": "sync"})
	}))
	defer upstream.Close()
	airbyte, err := NewAirbyteClient(upstream.URL, "", upstream.Client())
	if err != nil {
		t.Fatal(err)
	}
	store, _, cfg, _ := testServer(t, "")
	handler := NewHandler(store, HTTPConfig{
		AllowedOrigin: map[string]struct{}{}, OpenAPIPath: filepath.Join(cfg.RepoRoot, "openapi.json"),
		RequestTimeout: 5 * time.Second, Airbyte: airbyte,
	}, slog.New(slog.NewTextHandler(io.Discard, nil)))
	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodPost, "/api/v1/integrations/airbyte/syncs", strings.NewReader(`{"connection_id":"connection-91"}`))
	request.Header.Set("Content-Type", "application/json")
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted || !strings.Contains(response.Body.String(), `"job_id":91`) {
		t.Fatalf("unexpected Airbyte proxy response: %d %s", response.Code, response.Body.String())
	}
}

func TestTemporalWorkflowRunsTheGoPipeline(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	if _, err := GenerateNoisyFixtures(rawDir); err != nil {
		t.Fatal(err)
	}
	var suite testsuite.WorkflowTestSuite
	environment := suite.NewTestWorkflowEnvironment()
	environment.SetTestTimeout(15 * time.Second)
	environment.RegisterActivity(PipelineActivity)
	environment.ExecuteWorkflow(PipelineWorkflow, PipelineJob{
		RunID: "run_temporal_test", RawDirs: []string{rawDir},
		ArtifactDir: filepath.Join(root, "artifacts", "run_temporal_test"),
	})
	if !environment.IsWorkflowCompleted() || environment.GetWorkflowError() != nil {
		t.Fatalf("Temporal pipeline workflow failed: %v", environment.GetWorkflowError())
	}
	workspace, err := readWorkspace(filepath.Join(root, "artifacts", "run_temporal_test", "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	if workspace.StepSettings.CodeVersion != "go-engine-v1" || workspace.Stages[4].Count != 1086 {
		t.Fatalf("unexpected Temporal output: %#v", workspace.StepSettings)
	}
}

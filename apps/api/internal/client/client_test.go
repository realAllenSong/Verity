package client

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestImportFileWaitAndDownloadUsesTheRESTLifecycle(t *testing.T) {
	t.Parallel()
	var mu sync.Mutex
	var uploaded []byte
	var authorization []string
	output := []byte("published parquet bytes")
	outputHash := sha256.Sum256(output)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		authorization = append(authorization, r.Header.Get("Authorization"))
		mu.Unlock()
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspace":
			writeTestJSON(w, http.StatusOK, map[string]any{"dataset": map[string]any{"id": "dataset_local"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/imports":
			writeTestJSON(w, http.StatusAccepted, map[string]any{
				"import_id": "imp_1", "upload_id": "upl_1", "upload_url": "/api/v1/uploads/upl_1",
				"dataset_id": "dataset_local", "filename": "events.csv", "size_bytes": 24, "offset": 0,
				"state": "created", "created_at": "2026-09-06T00:00:00Z",
			})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/uploads/upl_1":
			offset, _ := strconv.Atoi(r.Header.Get("Upload-Offset"))
			part, _ := io.ReadAll(r.Body)
			mu.Lock()
			if offset != len(uploaded) {
				mu.Unlock()
				http.Error(w, "wrong offset", http.StatusConflict)
				return
			}
			uploaded = append(uploaded, part...)
			next := len(uploaded)
			mu.Unlock()
			w.Header().Set("Upload-Offset", strconv.Itoa(next))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/imports/imp_1/complete":
			writeTestJSON(w, http.StatusAccepted, map[string]any{
				"job_id": "job_1", "import_id": "imp_1", "state": "queued",
				"status_url": "/api/v1/jobs/job_1", "events_url": "/api/v1/jobs/job_1/events",
				"created_at": "2026-09-06T00:00:00Z", "updated_at": "2026-09-06T00:00:00Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/jobs/job_1":
			writeTestJSON(w, http.StatusOK, map[string]any{
				"job_id": "job_1", "import_id": "imp_1", "state": "succeeded", "output_id": "out_1",
				"status_url": "/api/v1/jobs/job_1", "events_url": "/api/v1/jobs/job_1/events",
				"created_at": "2026-09-06T00:00:00Z", "updated_at": "2026-09-06T00:00:01Z",
			})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/outputs/out_1":
			w.Header().Set("X-Verity-SHA256", hex.EncodeToString(outputHash[:]))
			w.Header().Set("Content-Disposition", `attachment; filename="curated.parquet"`)
			_, _ = w.Write(output)
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	directory := t.TempDir()
	source := filepath.Join(directory, "events.csv")
	contents := []byte("id,title\n1,hello world\n")
	if err := os.WriteFile(source, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	api, err := New(server.URL, "secret", server.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.ChunkSize = 7
	job, err := api.ImportFile(context.Background(), source)
	if err != nil {
		t.Fatal(err)
	}
	job, err = api.WaitJob(context.Background(), job.ID, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(directory, "ready.parquet")
	result, err := api.DownloadOutput(context.Background(), job.OutputID, target)
	if err != nil {
		t.Fatal(err)
	}

	mu.Lock()
	defer mu.Unlock()
	if string(uploaded) != string(contents) {
		t.Fatalf("uploaded %q, want %q", uploaded, contents)
	}
	if result.SHA256 != hex.EncodeToString(outputHash[:]) || result.Bytes != int64(len(output)) {
		t.Fatalf("unexpected download result: %+v", result)
	}
	for _, value := range authorization {
		if value != "Bearer secret" {
			t.Fatalf("authorization = %q", value)
		}
	}
}

func TestStageRecordsAndReviewCallsAreBounded(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/stages/privacy/records":
			if r.URL.Query().Get("limit") != "200" || r.URL.Query().Get("cursor") != "next" {
				t.Fatalf("unexpected stage query: %s", r.URL.RawQuery)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"stage_id": "privacy", "rows": []any{map[string]any{"event_id": "evt_1"}}, "returned": 1})
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/review-queue":
			writeTestJSON(w, http.StatusOK, map[string]any{"count": 1, "returned": 1, "records": []any{map[string]any{"id": "evt_1", "decision": "review"}}})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/reviews/evt_1":
			var payload map[string]string
			_ = json.NewDecoder(r.Body).Decode(&payload)
			if payload["decision"] != "accepted" || payload["note"] != "verified evidence" {
				t.Fatalf("unexpected review: %#v", payload)
			}
			writeTestJSON(w, http.StatusOK, map[string]any{"run_id": "run_1"})
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()
	api, _ := New(server.URL, "", server.Client())
	page, err := api.StageRecords(context.Background(), "privacy", "next", 500)
	if err != nil || page.Returned != 1 {
		t.Fatalf("page=%+v err=%v", page, err)
	}
	queue, err := api.Reviews(context.Background())
	if err != nil || queue.Count != 1 {
		t.Fatalf("queue=%+v err=%v", queue, err)
	}
	if _, err := api.SubmitReview(context.Background(), "evt_1", "accepted", "verified evidence"); err != nil {
		t.Fatal(err)
	}
}

func TestAPIErrorKeepsProblemDetails(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/problem+json")
		w.WriteHeader(http.StatusUnprocessableEntity)
		_, _ = io.WriteString(w, `{"title":"Unsupported format","detail":"extension .zip is not supported","status":422}`)
	}))
	defer server.Close()
	api, _ := New(server.URL, "", server.Client())
	_, err := api.Job(context.Background(), "job_1")
	if err == nil || !strings.Contains(err.Error(), "extension .zip is not supported") {
		t.Fatalf("error = %v", err)
	}
}

func TestJobEventsResumeFromLastAcknowledgedSequence(t *testing.T) {
	t.Parallel()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Last-Event-ID") != "7" {
			t.Fatalf("Last-Event-ID = %q", r.Header.Get("Last-Event-ID"))
		}
		w.Header().Set("Content-Type", "text/event-stream")
		_, _ = io.WriteString(w, "id: 8\nevent: stage_committed\ndata: {\"job_id\":\"job_1\",\"sequence\":8,\"event_type\":\"stage_committed\",\"stage_id\":\"privacy\",\"timestamp\":\"2026-09-06T00:00:01Z\"}\n\n")
	}))
	defer server.Close()
	api, _ := New(server.URL, "", server.Client())
	events, err := api.JobEvents(context.Background(), "job_1", 7)
	if err != nil || len(events) != 1 || events[0].Sequence != 8 || events[0].StageID != "privacy" {
		t.Fatalf("events=%+v err=%v", events, err)
	}
}

func writeTestJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}

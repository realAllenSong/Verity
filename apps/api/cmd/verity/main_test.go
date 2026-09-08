package main

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestStatusJSONWritesOnlyStructuredOutputToStdout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Authorization") != "Bearer test-token" {
			http.Error(w, "unauthorized", http.StatusUnauthorized)
			return
		}
		_ = json.NewEncoder(w).Encode(map[string]any{
			"job_id": "job_1", "import_id": "imp_1", "state": "succeeded", "output_id": "out_1",
			"status_url": "/api/v1/jobs/job_1", "events_url": "/api/v1/jobs/job_1/events",
			"created_at": "2026-09-06T00:00:00Z", "updated_at": "2026-09-06T00:00:01Z",
		})
	}))
	defer server.Close()
	var stdout, stderr bytes.Buffer
	exit := run([]string{"status", "job_1", "--server", server.URL, "--json"}, &stdout, &stderr, func(key string) string {
		if key == "VERITY_API_TOKEN" {
			return "test-token"
		}
		return ""
	})
	if exit != 0 || stderr.Len() != 0 {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
	var payload map[string]any
	if err := json.Unmarshal(stdout.Bytes(), &payload); err != nil {
		t.Fatalf("stdout is not JSON: %q", stdout.String())
	}
	if payload["job_id"] != "job_1" || payload["state"] != "succeeded" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestUnknownCommandReturnsUsageError(t *testing.T) {
	var stdout, stderr bytes.Buffer
	exit := run([]string{"launch"}, &stdout, &stderr, func(string) string { return "" })
	if exit != 2 || !strings.Contains(stderr.String(), "verity import") {
		t.Fatalf("exit=%d stderr=%q", exit, stderr.String())
	}
}

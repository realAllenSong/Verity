package mcpserver

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	verityclient "github.com/realAllenSong/Verity/apps/api/internal/client"
)

func TestOfficialSDKDiscoversBoundedToolsAndCompletesImport(t *testing.T) {
	var uploaded int
	output := []byte("published")
	serverAPI := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/workspace":
			writeJSON(w, map[string]any{"dataset": map[string]any{"id": "dataset_local"}})
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/imports":
			writeJSON(w, map[string]any{"import_id": "imp_1", "upload_id": "upl_1", "upload_url": "/api/v1/uploads/upl_1", "dataset_id": "dataset_local", "filename": "events.csv", "size_bytes": 24, "state": "created", "created_at": "2026-09-06T00:00:00Z"})
		case r.Method == http.MethodPatch && r.URL.Path == "/api/v1/uploads/upl_1":
			part, _ := io.ReadAll(r.Body)
			uploaded += len(part)
			w.Header().Set("Upload-Offset", strconv.Itoa(uploaded))
			w.WriteHeader(http.StatusNoContent)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/imports/imp_1/complete":
			writeJSON(w, jobPayload("queued"))
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/jobs/job_1":
			payload := jobPayload("succeeded")
			payload["output_id"] = "out_1"
			writeJSON(w, payload)
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/outputs/out_1":
			_, _ = w.Write(output)
		default:
			http.NotFound(w, r)
		}
	}))
	defer serverAPI.Close()

	api, err := verityclient.New(serverAPI.URL, "", serverAPI.Client())
	if err != nil {
		t.Fatal(err)
	}
	api.ChunkSize = 7
	server := New(api)
	clientTransport, serverTransport := mcp.NewInMemoryTransports()
	serverSession, err := server.Connect(context.Background(), serverTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer serverSession.Close()
	mcpClient := mcp.NewClient(&mcp.Implementation{Name: "verity-test", Version: "1.0.0"}, nil)
	clientSession, err := mcpClient.Connect(context.Background(), clientTransport, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer clientSession.Close()

	tools, err := clientSession.ListTools(context.Background(), &mcp.ListToolsParams{})
	if err != nil {
		t.Fatal(err)
	}
	if len(tools.Tools) != 6 {
		t.Fatalf("discovered %d tools, want 6", len(tools.Tools))
	}
	directory := t.TempDir()
	source := filepath.Join(directory, "events.csv")
	if err := os.WriteFile(source, []byte("id,title\n1,hello world\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	result, err := clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "verity_import", Arguments: map[string]any{"path": source, "wait": true}})
	if err != nil || result.IsError {
		t.Fatalf("import result=%#v err=%v", result, err)
	}
	encoded, _ := json.Marshal(result.StructuredContent)
	if !strings.Contains(string(encoded), `"job_id":"job_1"`) || strings.Contains(string(encoded), "hello world") {
		t.Fatalf("unexpected bounded result: %s", encoded)
	}
	target := filepath.Join(directory, "result.parquet")
	result, err = clientSession.CallTool(ctx, &mcp.CallToolParams{Name: "verity_export", Arguments: map[string]any{"output_id": "out_1", "path": target}})
	if err != nil || result.IsError {
		t.Fatalf("export result=%#v err=%v", result, err)
	}
	if data, err := os.ReadFile(target); err != nil || string(data) != string(output) {
		t.Fatalf("downloaded=%q err=%v", data, err)
	}
}

func jobPayload(state string) map[string]any {
	return map[string]any{"job_id": "job_1", "import_id": "imp_1", "state": state, "status_url": "/api/v1/jobs/job_1", "events_url": "/api/v1/jobs/job_1/events", "created_at": "2026-09-06T00:00:00Z", "updated_at": "2026-09-06T00:00:01Z"}
}

func writeJSON(w http.ResponseWriter, value any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(value)
}

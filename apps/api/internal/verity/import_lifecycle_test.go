package verity

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestUploadResumesAndCompletionAutoStartsJob(t *testing.T) {
	store, handler, _, _ := testServer(t, "")
	payload := []byte("record_id,kind,occurred_at,title,content\nrec_1,correction,2026-09-06T12:00:00Z,Correction,User corrected the agent path\n")
	created := createImportRequest(t, handler, "events.csv", int64(len(payload)), "import-resume")

	first := httptest.NewRequest(http.MethodPatch, created.UploadURL, bytes.NewReader(payload[:37]))
	first.Header.Set("Upload-Offset", "0")
	first.Header.Set("Content-Type", "application/offset+octet-stream")
	firstResponse := httptest.NewRecorder()
	handler.ServeHTTP(firstResponse, first)
	if firstResponse.Code != http.StatusNoContent || firstResponse.Header().Get("Upload-Offset") != "37" {
		t.Fatalf("first chunk = %d %s", firstResponse.Code, firstResponse.Body.String())
	}

	head := httptest.NewRecorder()
	handler.ServeHTTP(head, httptest.NewRequest(http.MethodHead, created.UploadURL, nil))
	if head.Code != http.StatusNoContent || head.Header().Get("Upload-Offset") != "37" {
		t.Fatalf("upload head = %d offset=%s", head.Code, head.Header().Get("Upload-Offset"))
	}

	second := httptest.NewRequest(http.MethodPatch, created.UploadURL, bytes.NewReader(payload[37:]))
	second.Header.Set("Upload-Offset", "37")
	second.Header.Set("Content-Type", "application/offset+octet-stream")
	secondResponse := httptest.NewRecorder()
	handler.ServeHTTP(secondResponse, second)
	if secondResponse.Code != http.StatusNoContent || secondResponse.Header().Get("Upload-Offset") != strconv.Itoa(len(payload)) {
		t.Fatalf("second chunk = %d offset=%s body=%s", secondResponse.Code, secondResponse.Header().Get("Upload-Offset"), secondResponse.Body.String())
	}

	complete := httptest.NewRequest(http.MethodPost, "/api/v1/imports/"+created.ImportID+"/complete", nil)
	complete.Header.Set("Idempotency-Key", "complete-resume")
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, complete)
	if completeResponse.Code != http.StatusAccepted {
		t.Fatalf("complete = %d %s", completeResponse.Code, completeResponse.Body.String())
	}
	var job JobSummary
	if err := json.Unmarshal(completeResponse.Body.Bytes(), &job); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(2 * time.Second)
	for {
		current, ok := store.Job(job.ID)
		if ok && (current.State == JobSucceeded || current.State == JobFailed) {
			job = current
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("automatic job did not reach a terminal state")
		}
		time.Sleep(5 * time.Millisecond)
	}
	if job.State != JobSucceeded {
		t.Fatalf("job state = %s, error=%s", job.State, job.Error)
	}
	imported := 0
	for _, batch := range store.Workspace().Batches {
		if batch.Filename == "events.csv" {
			imported = batch.RecordCount
			break
		}
	}
	if imported != 1 {
		t.Fatalf("imported batch has %d records, want 1", imported)
	}
}

func TestUploadRejectsWrongOffsetAndIncompleteCompletion(t *testing.T) {
	_, handler, _, _ := testServer(t, "")
	created := createImportRequest(t, handler, "events.jsonl", 20, "wrong-offset")

	patch := httptest.NewRequest(http.MethodPatch, created.UploadURL, strings.NewReader("{}\n"))
	patch.Header.Set("Upload-Offset", "10")
	patchResponse := httptest.NewRecorder()
	handler.ServeHTTP(patchResponse, patch)
	if patchResponse.Code != http.StatusConflict {
		t.Fatalf("wrong offset = %d %s", patchResponse.Code, patchResponse.Body.String())
	}

	complete := httptest.NewRequest(http.MethodPost, "/api/v1/imports/"+created.ImportID+"/complete", nil)
	complete.Header.Set("Idempotency-Key", "incomplete")
	completeResponse := httptest.NewRecorder()
	handler.ServeHTTP(completeResponse, complete)
	if completeResponse.Code != http.StatusConflict {
		t.Fatalf("incomplete completion = %d %s", completeResponse.Code, completeResponse.Body.String())
	}
}

func TestImportCreationIsIdempotent(t *testing.T) {
	_, handler, _, _ := testServer(t, "")
	first := createImportRequest(t, handler, "events.csv", 42, "same-import")
	second := createImportRequest(t, handler, "events.csv", 42, "same-import")
	if first.ImportID != second.ImportID || first.UploadID != second.UploadID {
		t.Fatalf("idempotent create returned different imports: %#v %#v", first, second)
	}
}

func createImportRequest(t *testing.T, handler http.Handler, filename string, size int64, key string) ImportSummary {
	t.Helper()
	body := `{"dataset_id":"workflow-signals","filename":"` + filename + `","size_bytes":` + strconv.FormatInt(size, 10) + `}`
	request := httptest.NewRequest(http.MethodPost, "/api/v1/imports", strings.NewReader(body))
	request.Header.Set("Content-Type", "application/json")
	request.Header.Set("Idempotency-Key", key)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)
	if response.Code != http.StatusAccepted {
		t.Fatalf("create import = %d %s", response.Code, response.Body.String())
	}
	var created ImportSummary
	if err := json.Unmarshal(response.Body.Bytes(), &created); err != nil {
		t.Fatal(err)
	}
	return created
}

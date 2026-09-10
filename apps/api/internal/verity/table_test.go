package verity

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func tableFixture(t *testing.T) (*Store, http.Handler, string) {
	t.Helper()
	store, handler, cfg, _ := testServer(t, "")
	dir := filepath.Join(cfg.ArtifactsDir, store.workspace.RunID)
	if err := os.MkdirAll(dir, 0o750); err != nil {
		t.Fatal(err)
	}
	files := map[string]string{
		"raw.jsonl": `{"record_id":"a","payload":{"content":"  hello   world ","measure":12,"optional":null}}
{"record_id":"b","payload":{"content":"keep","kind":"task"}}
{"record_id":"a","payload":{"content":"duplicate should disappear"}}
`,
		"normalize.jsonl": `{"event_id":"a","content":"hello world","quality_score":0.8}
{"event_id":"b","content":"keep","kind":"task"}
{"event_id":"new","content":"generated record"}
`,
		"signals.jsonl": `{"event_id":"a","content":"test signal","decision":"review"}
{"event_id":"b","content":"other signal","decision":"accepted"}
`,
		"curated.jsonl": `{"event_id":"b","content":"other signal","decision":"accepted"}
`,
	}
	for name, data := range files {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(data), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	return store, handler, dir
}

func TestTablePagesKeepRealPairsDuplicatesAndNewRows(t *testing.T) {
	s, _, dir := tableFixture(t)
	page, err := s.Table(context.Background(), "normalize", "", "", "all", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if page.Rows != 4 || len(page.Records) != 2 || page.Counts["removed"] != 1 || page.Counts["added"] != 1 || page.NextCursor == "" {
		t.Fatalf("bad table: %+v", page)
	}
	if page.Records[0].RecordID != "a" || page.Records[0].Change != "changed" || len(page.Records[0].Before) == 0 || len(page.Records[0].After) == 0 {
		t.Fatal("lost first-occurrence match")
	}
	next, err := s.Table(context.Background(), "normalize", "", page.NextCursor, "all", "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if next.Records[0].Ordinal != 3 || next.Records[0].Change != "removed" || next.Records[1].Change != "added" || next.NextCursor != "" {
		t.Fatalf("bad next page %+v", next)
	}
	// The cached table is complete, not the former representative sample.
	if err := os.Rename(filepath.Join(dir, "raw.jsonl"), filepath.Join(dir, "raw.offline")); err != nil {
		t.Fatal(err)
	}
	removed, err := s.Table(context.Background(), "normalize", "", "", "removed", "duplicate", 100)
	if err != nil || len(removed.Records) != 1 || removed.Records[0].RecordID != "a" {
		t.Fatalf("search/filter %+v %v", removed, err)
	}
	if _, err := s.Table(context.Background(), "normalize", "", page.NextCursor, "removed", "", 2); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("accepted cross-filter cursor %v", err)
	}
	if _, err := s.Table(context.Background(), "normalize", "other_run", "", "all", "", 2); !errors.Is(err, ErrConflict) {
		t.Fatalf("accepted stale run %v", err)
	}
	if _, err := s.Table(context.Background(), "normalize", "", "bad!", "all", "", 2); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("accepted malformed cursor %v", err)
	}
}

func TestTableBranchIsNotDeletionAndReviewUsesNewSnapshot(t *testing.T) {
	s, _, dir := tableFixture(t)
	page, err := s.Table(context.Background(), "curated", "", "", "all", "", 1)
	if err != nil {
		t.Fatal(err)
	}
	if page.Records[0].Change != "elsewhere" || page.Counts["removed"] != 0 || page.Counts["result"] != 1 {
		t.Fatalf("branch mislabeled %+v", page)
	}
	var before pipelineRecord
	before.EventID, before.Content, before.Decision = "a", "test signal", "review"
	events := []ReviewEvent{{RecordID: "a", RecordHash: reviewHash(before), Decision: "accepted"}}
	updated, err := s.projectReviewsLocked(s.workspace, events)
	if err != nil {
		t.Fatal(err)
	}
	s.workspace = updated
	after, err := s.Table(context.Background(), "curated", "", "", "result", "", 100)
	if err != nil {
		t.Fatal(err)
	}
	if after.Snapshot == page.Snapshot || len(after.Records) != 2 || after.Records[0].Change != "changed" {
		t.Fatalf("stale review projection %+v", after)
	}
	if _, err := s.Table(context.Background(), "curated", "", page.NextCursor, "all", "", 1); !errors.Is(err, ErrInvalidImport) {
		t.Fatalf("accepted stale revision cursor %v", err)
	}
	review, err := s.Table(context.Background(), "review", "", "", "result", "", 100)
	if err != nil || len(review.Records) != 0 {
		t.Fatalf("resolved item left in queue %+v %v", review, err)
	}
	_ = dir
}

func TestTableHTTPValidationAndRawPayload(t *testing.T) {
	_, handler, _ := tableFixture(t)
	for _, url := range []string{"/api/v1/stages/raw/table?limit=0", "/api/v1/stages/raw/table?filter=bad", "/api/v1/stages/raw/table?cursor=bad!"} {
		r := httptest.NewRecorder()
		handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, url, nil))
		if r.Code != 422 {
			t.Fatalf("%s: %d %s", url, r.Code, r.Body.String())
		}
	}
	r := httptest.NewRecorder()
	handler.ServeHTTP(r, httptest.NewRequest(http.MethodGet, "/api/v1/stages/raw/table?limit=1", nil))
	var page TablePage
	if r.Code != 200 || json.Unmarshal(r.Body.Bytes(), &page) != nil {
		t.Fatalf("raw table %d %s", r.Code, r.Body.String())
	}
	if page.Rows != 3 || page.Records[0].Change != "input" || len(page.Fields) != 4 {
		t.Fatalf("raw payload fields %+v", page)
	}
	for _, field := range page.Fields {
		if field == "payload" {
			t.Fatal("opaque payload instead of columns")
		}
	}
}

func TestTableChangePreservesNullVersusMissing(t *testing.T) {
	if tableChange(json.RawMessage(`{"a":null}`), json.RawMessage(`{}`), "kept") != "changed" {
		t.Fatal("missing and null were conflated")
	}
}

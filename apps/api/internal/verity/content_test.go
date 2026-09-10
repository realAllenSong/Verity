package verity

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func languageFixtures(t *testing.T) []map[string]any {
	t.Helper()
	data, err := os.ReadFile("../../../../sample_data/examples/language-workflow.json")
	if err != nil {
		t.Fatal(err)
	}
	var rows []map[string]any
	if err := json.Unmarshal(data, &rows); err != nil {
		t.Fatal(err)
	}
	return rows
}

func fixtureRecord(body map[string]any) dataRecord {
	return dataRecord{RecordID: literal(body, "record_id"), DatasetID: "language", BatchID: "sample", Payload: body}
}

func TestNormalizePreservesMessagesAndExplicitRelationships(t *testing.T) {
	input := fixtureRecord(languageFixtures(t)[0])
	row := normalizeRecord(input)
	if len(row.ContentBlocks) != 8 {
		t.Fatalf("lost messages: %+v", row.ContentBlocks)
	}
	if row.OccurredAt != "" {
		t.Fatal("invented time")
	}
	correction := row.ContentBlocks[2]
	if correction.Role != "user" || correction.Interaction != "correction" || correction.ReplyTo != "turn-2" || correction.ID != "turn-3" || correction.SourcePath != "messages[2].content" {
		t.Fatalf("lost provenance: %+v", correction)
	}
	if row.ContentBlocks[4].Role != "tool_call" || row.ContentBlocks[4].ID != "call-tests" {
		t.Fatal("lost tool call")
	}
	if !strings.Contains(row.ContentBlocks[5].Text, "\nPASS") {
		t.Fatal("flattened line breaks")
	}
	if _, ok := row.Attributes["workspace"]; !ok {
		t.Fatal("discarded provider attributes")
	}
	if _, ok := row.Attributes["messages"]; !ok {
		t.Fatal("discarded nested source metadata")
	}
	if strings.Contains(row.Content, "PRIVATE_DEMO") {
		t.Fatal("private fragment leaked into canonical content")
	}
	quality, _, keep := applyQuality(row, "sample")
	if !keep {
		t.Fatal("useful text rejected for absent timestamp")
	}
	signal, _, keep := applySignal(quality, "sample")
	if !keep || signal.ExtractedSignal != correction.Text || signal.Decision != "review" || signal.Confidence != 0 {
		t.Fatal("invented signal summary or confidence")
	}
}

func TestNormalizeEveryLanguageSourceAndSparseData(t *testing.T) {
	for _, body := range languageFixtures(t) {
		t.Run(literal(body, "record_id"), func(t *testing.T) {
			row := normalizeRecord(fixtureRecord(body))
			if literal(body, "kind") != "usage_aggregate" && len(row.ContentBlocks) == 0 {
				t.Fatal("empty reading document")
			}
			if row.EventID == "language-demo-jira" && len(row.ContentBlocks) != 2 {
				t.Fatal("description replaced by comment")
			}
			if row.EventID == "language-demo-slides" && (row.ContentBlocks[0].Role != "before" || row.ContentBlocks[1].Role != "after") {
				t.Fatal("lost edit semantics")
			}
			if row.EventID == "language-demo-usage" && row.Attributes["input_tokens"] != float64(48000) {
				t.Fatal("lost numeric attributes")
			}
			data, _ := json.Marshal(row)
			doc := readingDocument(protectedRaw(data))
			if doc.Source == "Outlook" && (strings.Contains(doc.Blocks[0].Text, "<p>") || !strings.Contains(doc.Blocks[0].Text, "send(request)")) {
				t.Fatalf("unreadable email: %+v", doc)
			}
			if !doc.Synthetic {
				t.Fatal("fixture lost synthetic label")
			}
		})
	}
	code := "func run() {\r\n    return\r\n}\r\n"
	row := normalizeRecord(fixtureRecord(map[string]any{"source": "Custom", "body": code, "content": "This separate content must not remove the body field."}))
	if row.Attributes["body"] != code {
		t.Fatal("discarded alternate body")
	}
	if normalizeText(code) != "func run() {\n    return\n}" {
		t.Fatal("code indentation lost")
	}
}

func TestProtectedInspectionDoesNotLeakNestedSecrets(t *testing.T) {
	raw := json.RawMessage(`{"record_id":"safe-id","payload":{"title":"Contact owner@example.invalid","messages":[{"role":"user","content":"Keep this useful request. Bearer DEMOCREDENTIAL123456"},{"role":"user","visibility":"private","content":"PRIVATE_CONTENT"}],"tool":{"arguments":{"api_key":"ARBITRARYSECRET"}},"metadata":{"password":"ANOTHERSECRET"}}}`)
	safe := protectedRaw(raw)
	for _, forbidden := range []string{"owner@example.invalid", "DEMOCREDENTIAL", "PRIVATE_CONTENT", "ARBITRARYSECRET", "ANOTHERSECRET"} {
		if strings.Contains(string(safe), forbidden) {
			t.Fatalf("leaked %s", forbidden)
		}
	}
	if !strings.Contains(string(safe), "Keep this useful request") {
		t.Fatal("removed useful content")
	}
	if !strings.Contains(string(raw), "ARBITRARYSECRET") {
		t.Fatal("mutated original")
	}
	if htmlText("<script>secret()</script><p>One &amp; two</p><p>Three</p>") != "One & two\n\nThree" {
		t.Fatal("unsafe HTML extraction")
	}
}

func TestProtectedTableAndRecordsSearchOnlyVisibleValues(t *testing.T) {
	s, handler, dir := tableFixture(t)
	input := fixtureRecord(languageFixtures(t)[0])
	if err := writeJSONLinesAtomic(filepath.Join(dir, "raw.jsonl"), []dataRecord{input}); err != nil {
		t.Fatal(err)
	}
	if err := writeJSONLinesAtomic(filepath.Join(dir, "normalize.jsonl"), []pipelineRecord{normalizeRecord(input)}); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"raw", "normalize"} {
		page, err := s.Table(context.Background(), stage, "", "", "all", "", 100)
		if err != nil {
			t.Fatal(err)
		}
		encoded, _ := json.Marshal(page)
		if strings.Contains(string(encoded), "PRIVATE_DEMO") || strings.Contains(string(encoded), "DEMONOTAREALCREDENTIAL") {
			t.Fatal("table leaked")
		}
		if len(page.Records[0].AfterDocument.Blocks) != 8 {
			t.Fatal("table lost messages")
		}
		found, err := s.Table(context.Background(), stage, "", "", "all", "DEMONOTAREALCREDENTIAL", 100)
		if err != nil || len(found.Records) != 0 {
			t.Fatal("hidden credential searchable")
		}
		for _, endpoint := range []string{"table", "records"} {
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/api/v1/stages/"+stage+"/"+endpoint, nil))
			if w.Code != 200 || strings.Contains(w.Body.String(), "PRIVATE_DEMO") || strings.Contains(w.Body.String(), "DEMONOTAREALCREDENTIAL") {
				t.Fatalf("unsafe %s: %d", endpoint, w.Code)
			}
		}
	}
}

func TestPrivacyPreservesDistinctActorPseudonymsAndDoesNotMutateInput(t *testing.T) {
	a := normalizeRecord(fixtureRecord(map[string]any{"source": "Custom", "prompt": "Hello", "actor": "a@example.invalid", "metadata": map[string]any{"api_key": "SECRETXYZ"}}))
	b := a
	b.Actor = "b@example.invalid"
	x, _, _ := applyPrivacy(a, "sample")
	y, _, _ := applyPrivacy(b, "sample")
	if x.Actor == y.Actor {
		t.Fatal("collapsed two actors")
	}
	encoded, _ := json.Marshal(x)
	if strings.Contains(string(encoded), "SECRETXYZ") {
		t.Fatal("attributes leaked")
	}
	encoded, _ = json.Marshal(a)
	if !strings.Contains(string(encoded), "SECRETXYZ") {
		t.Fatal("mutated earlier boundary")
	}
}

package verity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReviewQueueIncludesUnsampledRecordsAndExportFollowsDecisions(t *testing.T) {
	store, _, cfg, _ := testServer(t, "")
	raw := filepath.Join(t.TempDir(), "raw")
	if _, err := GenerateNoisyFixtures(raw); err != nil {
		t.Fatal(err)
	}
	store.engine = LocalEngine{RawDir: raw, ArtifactsDir: cfg.ArtifactsDir}
	if _, err := store.Run(context.Background()); err != nil {
		t.Fatal(err)
	}
	initial := store.Workspace()
	total, records := store.ReviewQueue()
	if total != 42 || len(records) != 42 {
		t.Fatalf("full review queue: total=%d returned=%d, want 42", total, len(records))
	}
	initialOutput := initial.Outputs[0].ID
	oldPath, _, err := store.OutputPath(initialOutput)
	if err != nil {
		t.Fatal(err)
	}
	oldBytes, err := os.ReadFile(oldPath)
	if err != nil {
		t.Fatal(err)
	}
	id := records[len(records)-1].ID
	updated, err := store.UpdateReview(id, ReviewUpdate{Decision: "accepted", Note: "Verified against source"})
	if err != nil {
		t.Fatal(err)
	}
	if updated.DecisionBreakdown.Review != 41 || updated.DecisionBreakdown.Accepted != 1045 {
		t.Fatalf("wrong review totals: %+v", updated.DecisionBreakdown)
	}
	path, output, err := store.OutputPath(updated.Outputs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	bytes, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if output.RecordCount != 1045 || !strings.Contains(string(bytes), `"event_id":"`+id+`"`) {
		t.Fatal("accepted record missing from new download")
	}
	unchanged, _, err := store.OutputPath(initialOutput)
	if err != nil {
		t.Fatal(err)
	}
	actual, _ := os.ReadFile(unchanged)
	if string(actual) != string(oldBytes) {
		t.Fatal("old output snapshot was overwritten")
	}
	updated, err = store.UpdateReview(id, ReviewUpdate{Decision: "rejected"})
	if err != nil {
		t.Fatal(err)
	}
	path, _, err = store.OutputPath(updated.Outputs[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	bytes, _ = os.ReadFile(path)
	if strings.Contains(string(bytes), `"event_id":"`+id+`"`) {
		t.Fatal("rejected record still exported")
	}
	reloaded, err := NewStore(cfg, store.engine)
	if err != nil {
		t.Fatal(err)
	}
	remaining, queue := reloaded.ReviewQueue()
	if remaining != 41 || len(queue) != 41 {
		t.Fatalf("review progress lost on restart: %d / %d", remaining, len(queue))
	}
}

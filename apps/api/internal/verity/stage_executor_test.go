package verity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStreamingPipelinePublishesEveryStageAndDiskBackedDedup(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	if _, err := GenerateNoisyFixtures(rawDir); err != nil {
		t.Fatal(err)
	}
	runID := "run_streaming_test"
	artifactDir := filepath.Join(root, "artifacts", runID)
	committed := make([]StageProgress, 0, 7)
	workspace, err := runStreamingPipeline(context.Background(), PipelineConfig{
		RunID: runID, RawDirs: []string{rawDir}, ArtifactDir: artifactDir,
		OnStageCommitted: func(progress StageProgress) {
			if _, err := os.Stat(filepath.Join(artifactDir, progress.StageID+".jsonl")); err != nil {
				t.Errorf("%s callback ran before its artifact was committed: %v", progress.StageID, err)
			}
			committed = append(committed, progress)
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{"raw": 3842, "normalize": 3611, "privacy": 3276, "quality": 2914, "signals": 1086, "review": 42, "curated": 1044}
	for _, stage := range workspace.Stages {
		if stage.Count != want[stage.ID] {
			t.Fatalf("%s count=%d, want %d", stage.ID, stage.Count, want[stage.ID])
		}
		if count, err := countJSONLines(filepath.Join(artifactDir, stage.ID+".jsonl")); err != nil || count != stage.Count {
			t.Fatalf("%s artifact count=%d err=%v", stage.ID, count, err)
		}
	}
	if _, err := os.Stat(filepath.Join(artifactDir, ".dedup.db")); err != nil {
		t.Fatalf("disk-backed dedup index missing: %v", err)
	}
	if len(committed) != 7 {
		t.Fatalf("committed stage events=%d, want 7", len(committed))
	}
	for _, progress := range committed {
		if progress.Count != want[progress.StageID] {
			t.Fatalf("%s callback count=%d, want %d", progress.StageID, progress.Count, want[progress.StageID])
		}
	}
}

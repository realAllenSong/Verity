package verity

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestStageInspectionDoesNotRescanRunArtifacts(t *testing.T) {
	root := t.TempDir()
	raw := filepath.Join(root, "raw")
	if _, err := GenerateNoisyFixtures(raw); err != nil {
		t.Fatal(err)
	}
	engine := LocalEngine{RawDir: raw, ArtifactsDir: filepath.Join(root, "artifacts")}
	if err := engine.Run(context.Background(), "run_inspection"); err != nil {
		t.Fatal(err)
	}
	for _, stage := range []string{"raw", "normalize", "privacy", "quality", "signals", "review", "curated"} {
		if err := os.Rename(filepath.Join(engine.ArtifactsDir, "run_inspection", stage+".jsonl"), filepath.Join(engine.ArtifactsDir, "run_inspection", stage+".offline")); err != nil {
			t.Fatal(err)
		}
	}
	for _, stage := range []string{"raw", "normalize", "privacy", "quality", "signals", "review", "curated"} {
		samples, err := engine.Compare(context.Background(), "run_inspection", stage, 8)
		if err != nil || len(samples) != 8 {
			t.Fatalf("%s inspection: %d rows, %v", stage, len(samples), err)
		}
		if stage == "privacy" {
			filtered, redacted := false, false
			for _, sample := range samples {
				filtered = filtered || sample.Outcome == "filtered"
				redacted = redacted || containsField(sample.ChangedFields, "content")
			}
			if !filtered || !redacted {
				t.Fatal("inspection lost privacy removal or redaction evidence")
			}
		}
	}
}

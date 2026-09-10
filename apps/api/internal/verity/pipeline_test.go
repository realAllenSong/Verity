package verity

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/parquet-go/parquet-go"
)

func TestGoPipelineReproducesTheDemoFunnel(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	artifacts := filepath.Join(root, "artifacts")
	counts, err := GenerateNoisyFixtures(rawDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(counts) != 6 {
		t.Fatalf("expected six traceable batches, got %d", len(counts))
	}
	engine := LocalEngine{RepoRoot: root, RawDir: rawDir, ArtifactsDir: artifacts}
	if err := engine.Run(context.Background(), DemoRunID); err != nil {
		t.Fatal(err)
	}
	workspace, err := readWorkspace(filepath.Join(artifacts, DemoRunID, "workspace.json"))
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]int{
		"raw": 3842, "normalize": 3611, "privacy": 3276, "quality": 2914,
		"signals": 1086, "review": 42, "curated": 1044,
	}
	for _, stage := range workspace.Stages {
		if stage.Count != want[stage.ID] {
			t.Fatalf("unexpected %s count: got %d, want %d", stage.ID, stage.Count, want[stage.ID])
		}
	}
	if workspace.StepSettings.CodeVersion != "go-engine-v2-content" {
		t.Fatalf("workspace was not produced by the Go engine: %s", workspace.StepSettings.CodeVersion)
	}
	if _, err := os.Stat(filepath.Join(artifacts, DemoRunID, "curated.csv")); err != nil {
		t.Fatalf("curated CSV missing: %v", err)
	}
	parquetRows, err := parquet.ReadFile[curatedParquetRow](filepath.Join(artifacts, DemoRunID, "curated.parquet"))
	if err != nil {
		t.Fatalf("curated Parquet is unreadable: %v", err)
	}
	if len(parquetRows) != want["curated"] {
		t.Fatalf("unexpected Parquet row count: got %d, want %d", len(parquetRows), want["curated"])
	}
	rows, err := engine.Preview(context.Background(), DemoRunID, "signals", 3)
	if err != nil || len(rows) != 3 {
		t.Fatalf("unexpected bounded preview: %d rows, %v", len(rows), err)
	}
	comparison, err := engine.Compare(context.Background(), DemoRunID, "privacy", 8)
	if err != nil || len(comparison) != 8 {
		t.Fatalf("unexpected stage comparison: %d rows, %v", len(comparison), err)
	}
	foundFiltered, foundChanged := false, false
	for _, sample := range comparison {
		foundFiltered = foundFiltered || sample.Outcome == "filtered"
		foundChanged = foundChanged || len(sample.ChangedFields) > 0
	}
	if !foundFiltered || !foundChanged {
		t.Fatalf("comparison did not show both removals and transformations: %#v", comparison)
	}
	readyComparison, err := engine.Compare(context.Background(), DemoRunID, "curated", 8)
	if err != nil || len(readyComparison) != 8 {
		t.Fatalf("unexpected ready comparison: %d rows, %v", len(readyComparison), err)
	}
	for _, sample := range readyComparison {
		if sample.Outcome != "routed" {
			t.Fatalf("a branch comparison mislabeled a routed record: %#v", sample)
		}
	}
}

func TestGoPipelineRedactsBeforePublishing(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	artifacts := filepath.Join(root, "artifacts")
	if _, err := GenerateNoisyFixtures(rawDir); err != nil {
		t.Fatal(err)
	}
	engine := LocalEngine{RepoRoot: root, RawDir: rawDir, ArtifactsDir: artifacts}
	if err := engine.Run(context.Background(), "run_privacy_test"); err != nil {
		t.Fatal(err)
	}
	privacy, err := os.ReadFile(filepath.Join(artifacts, "run_privacy_test", "privacy.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	text := string(privacy)
	if strings.Contains(text, "od_demo_secret_") || strings.Contains(text, "@example.invalid") {
		t.Fatal("privacy artifact contains a protected value")
	}
	if !strings.Contains(text, "[email redacted]") {
		t.Fatal("privacy artifact does not expose redaction evidence")
	}
}

func TestGoPipelineRejectsMalformedEnvelope(t *testing.T) {
	root := t.TempDir()
	rawDir := filepath.Join(root, "raw")
	if err := os.MkdirAll(rawDir, 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(rawDir, "bad.jsonl"), []byte("{\"record_id\":\"missing-contract\"}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	engine := LocalEngine{RepoRoot: root, RawDir: rawDir, ArtifactsDir: filepath.Join(root, "artifacts")}
	if err := engine.Run(context.Background(), "run_bad"); err == nil || !strings.Contains(err.Error(), "invalid record envelope") {
		t.Fatalf("expected an envelope validation error, got %v", err)
	}
}

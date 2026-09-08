//go:build load

package verity

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"testing"
)

func TestMillionRecordPipeline(t *testing.T) {
	source := os.Getenv("VERITY_MILLION_RECORD_SOURCE")
	if source == "" {
		t.Skip("set VERITY_MILLION_RECORD_SOURCE to a canonical JSONL directory")
	}
	root := t.TempDir()
	var before, after runtime.MemStats
	runtime.GC()
	runtime.ReadMemStats(&before)
	workspace, err := RunPipeline(context.Background(), PipelineConfig{
		RunID: "run_million", RawDirs: []string{source}, ArtifactDir: filepath.Join(root, "run_million"),
	})
	if err != nil {
		t.Fatal(err)
	}
	runtime.GC()
	runtime.ReadMemStats(&after)
	if workspace.Dataset.RecordCount != 1_000_000 {
		t.Fatalf("raw count=%d", workspace.Dataset.RecordCount)
	}
	if delta := after.Sys - before.Sys; delta > 2<<30 {
		t.Fatalf("Go runtime memory grew by %d bytes", delta)
	}
}

func BenchmarkPipeline10K(b *testing.B) {
	root := b.TempDir()
	rawDir := filepath.Join(root, "raw")
	if _, err := GenerateNoisyFixtures(rawDir); err != nil {
		b.Fatal(err)
	}
	b.ResetTimer()
	for index := 0; index < b.N; index++ {
		artifact := filepath.Join(root, "runs", "bench-"+strconv.Itoa(index))
		if _, err := RunPipeline(context.Background(), PipelineConfig{RunID: "run_bench", RawDirs: []string{rawDir}, ArtifactDir: artifact}); err != nil {
			b.Fatal(err)
		}
	}
}

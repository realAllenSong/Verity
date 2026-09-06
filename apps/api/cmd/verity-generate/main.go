package main

import (
	"context"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/realAllenSong/Verity/apps/api/internal/verity"
)

func main() {
	root := flag.String("root", ".", "repository root")
	artifactRoot := flag.String("artifact-root", "", "artifact root")
	runID := flag.String("run-id", verity.DemoRunID, "run identifier")
	refreshFixtures := flag.Bool("refresh-fixtures", false, "regenerate synthetic input batches")
	writeWebFixture := flag.Bool("write-web-fixture", false, "refresh the checked-in web snapshot")
	flag.Parse()

	absoluteRoot, err := filepath.Abs(*root)
	check(err)
	artifacts := *artifactRoot
	if artifacts == "" {
		artifacts = filepath.Join(absoluteRoot, "artifacts")
	}
	rawDir := filepath.Join(absoluteRoot, "sample_data", "raw")
	batchCounts := map[string]int{}
	if *refreshFixtures || !hasJSONL(rawDir) {
		batchCounts, err = verity.GenerateNoisyFixtures(rawDir)
		check(err)
	}
	engine := verity.LocalEngine{RepoRoot: absoluteRoot, ArtifactsDir: artifacts, RawDir: rawDir}
	check(engine.Run(context.Background(), *runID))
	workspacePath := filepath.Join(artifacts, *runID, "workspace.json")
	if *writeWebFixture {
		check(copyFile(workspacePath, filepath.Join(absoluteRoot, "apps", "web", "src", "data", "demo-workspace.json")))
	}
	workspaceData, err := os.ReadFile(workspacePath)
	check(err)
	var workspace struct {
		Stages []struct {
			ID    string `json:"id"`
			Count int    `json:"count"`
		} `json:"stages"`
	}
	check(json.Unmarshal(workspaceData, &workspace))
	stageCounts := make(map[string]int)
	for _, stage := range workspace.Stages {
		stageCounts[stage.ID] = stage.Count
	}
	check(json.NewEncoder(os.Stdout).Encode(map[string]any{"run_id": *runID, "batches": batchCounts, "stages": stageCounts}))
}

func hasJSONL(directory string) bool {
	matches, _ := filepath.Glob(filepath.Join(directory, "*.jsonl"))
	return len(matches) > 0
}

func copyFile(source, target string) error {
	input, err := os.Open(source)
	if err != nil {
		return err
	}
	defer input.Close()
	if err := os.MkdirAll(filepath.Dir(target), 0o750); err != nil {
		return err
	}
	output, err := os.CreateTemp(filepath.Dir(target), filepath.Base(target)+"-*.tmp")
	if err != nil {
		return err
	}
	temporary := output.Name()
	defer os.Remove(temporary)
	if _, err := io.Copy(output, input); err != nil {
		output.Close()
		return err
	}
	if err := output.Close(); err != nil {
		return err
	}
	if err := os.Chmod(temporary, 0o600); err != nil {
		return err
	}
	return os.Rename(temporary, target)
}

func check(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

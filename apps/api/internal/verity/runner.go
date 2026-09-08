package verity

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
)

type Engine interface {
	Run(ctx context.Context, runID string) error
	Preview(ctx context.Context, runID, stageID string, limit int) ([]json.RawMessage, error)
	Compare(ctx context.Context, runID, stageID string, limit int) ([]StageComparisonSample, error)
}

type StageProgress struct {
	StageID    string
	Count      int
	InputCount int
}

type progressEngine interface {
	RunWithProgress(context.Context, string, func(StageProgress)) error
}

// LocalEngine runs the complete data plane in-process. It is the dependency-free
// default for laptops, CI, and single-workspace deployments.
type LocalEngine struct {
	RepoRoot     string
	ArtifactsDir string
	RawDir       string
}

func (e LocalEngine) Run(ctx context.Context, runID string) error {
	return e.RunWithProgress(ctx, runID, nil)
}

func (e LocalEngine) RunWithProgress(ctx context.Context, runID string, onStage func(StageProgress)) error {
	rawDir := e.RawDir
	if rawDir == "" {
		rawDir = filepath.Join(e.RepoRoot, "sample_data", "raw")
	}
	_, err := RunPipeline(ctx, PipelineConfig{
		RunID:            runID,
		RawDirs:          []string{rawDir, filepath.Join(e.ArtifactsDir, "staged")},
		ArtifactDir:      filepath.Join(e.ArtifactsDir, runID),
		OnStageCommitted: onStage,
	})
	return err
}

func (e LocalEngine) Preview(
	ctx context.Context,
	runID string,
	stageID string,
	limit int,
) ([]json.RawMessage, error) {
	if !validStageID(stageID) {
		return nil, ErrNotFound
	}
	path := filepath.Join(e.ArtifactsDir, runID, stageID+".jsonl")
	file, err := os.Open(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, ErrNotFound
		}
		return nil, fmt.Errorf("open stage artifact: %w", err)
	}
	defer file.Close()

	limit = max(1, min(limit, 100))
	rows := make([]json.RawMessage, 0, limit)
	reader := bufio.NewReader(file)
	for len(rows) < limit {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		line, readErr := reader.ReadBytes('\n')
		if len(line) > 0 {
			line = bytesTrimSpace(line)
			if len(line) > 0 {
				if !json.Valid(line) {
					return nil, fmt.Errorf("stage artifact contains invalid JSON")
				}
				rows = append(rows, json.RawMessage(append([]byte(nil), line...)))
			}
		}
		if errors.Is(readErr, io.EOF) {
			break
		}
		if readErr != nil {
			return nil, fmt.Errorf("read stage artifact: %w", readErr)
		}
	}
	return rows, nil
}

func (e LocalEngine) Compare(ctx context.Context, runID, stageID string, limit int) ([]StageComparisonSample, error) {
	return compareStageArtifacts(ctx, e.ArtifactsDir, runID, stageID, limit)
}

func validStageID(value string) bool {
	switch value {
	case "raw", "normalize", "privacy", "quality", "signals", "review", "curated":
		return true
	default:
		return false
	}
}

func bytesTrimSpace(value []byte) []byte {
	start, end := 0, len(value)
	for start < end && (value[start] == ' ' || value[start] == '\t' || value[start] == '\r' || value[start] == '\n') {
		start++
	}
	for end > start && (value[end-1] == ' ' || value[end-1] == '\t' || value[end-1] == '\r' || value[end-1] == '\n') {
		end--
	}
	return value[start:end]
}

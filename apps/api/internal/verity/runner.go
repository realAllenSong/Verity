package verity

import (
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"path/filepath"
	"time"
)

type Engine interface {
	Run(ctx context.Context, runID string) error
	Preview(ctx context.Context, runID, stageID string, limit int) ([]json.RawMessage, error)
}

type CommandEngine struct {
	RepoRoot     string
	ArtifactsDir string
	EngineDir    string
	Timeout      time.Duration
}

func (e CommandEngine) Run(ctx context.Context, runID string) error {
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"uv",
		"run",
		"python",
		"-m",
		"app.cli",
		"generate",
		"--root",
		e.RepoRoot,
		"--run-id",
		runID,
		"--artifact-root",
		e.ArtifactsDir,
	)
	command.Dir = e.EngineDir
	output, err := command.CombinedOutput()
	if err != nil {
		return fmt.Errorf("data-plane run failed: %w: %s", err, bounded(output, 1200))
	}
	return nil
}

func (e CommandEngine) Preview(
	ctx context.Context,
	runID string,
	stageID string,
	limit int,
) ([]json.RawMessage, error) {
	ctx, cancel := context.WithTimeout(ctx, e.timeout())
	defer cancel()
	command := exec.CommandContext(
		ctx,
		"uv",
		"run",
		"python",
		"-m",
		"app.cli",
		"preview",
		"--root",
		e.RepoRoot,
		"--run-id",
		runID,
		"--artifact-root",
		e.ArtifactsDir,
		"--stage",
		stageID,
		"--limit",
		fmt.Sprintf("%d", limit),
	)
	command.Dir = e.EngineDir
	output, err := command.CombinedOutput()
	if err != nil {
		return nil, fmt.Errorf("stage preview failed: %w: %s", err, bounded(output, 1200))
	}
	var rows []json.RawMessage
	if err := json.Unmarshal(output, &rows); err != nil {
		return nil, fmt.Errorf("decode stage preview: %w", err)
	}
	return rows, nil
}

func (e CommandEngine) WorkspacePath(runID string) string {
	return filepath.Join(e.ArtifactsDir, runID, "workspace.json")
}

func (e CommandEngine) timeout() time.Duration {
	if e.Timeout <= 0 {
		return 3 * time.Minute
	}
	return e.Timeout
}

func bounded(value []byte, limit int) string {
	if len(value) <= limit {
		return string(value)
	}
	return string(value[:limit]) + "..."
}

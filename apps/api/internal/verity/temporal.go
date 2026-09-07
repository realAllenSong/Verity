package verity

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"time"

	"go.temporal.io/sdk/client"
	"go.temporal.io/sdk/temporal"
	"go.temporal.io/sdk/worker"
	"go.temporal.io/sdk/workflow"
)

const DefaultTemporalTaskQueue = "verity-pipeline"

type TemporalConfig struct {
	Address      string
	Namespace    string
	TaskQueue    string
	RepoRoot     string
	ArtifactsDir string
	RawDir       string
}

type PipelineJob struct {
	RunID       string
	RawDirs     []string
	ArtifactDir string
}

// TemporalEngine submits runs to Temporal while reading previews from the
// shared artifact store. The integration is dormant unless explicitly selected.
type TemporalEngine struct {
	client         client.Client
	taskQueue      string
	localArtifacts LocalEngine
}

func NewTemporalEngine(cfg TemporalConfig) (*TemporalEngine, error) {
	if cfg.Address == "" {
		cfg.Address = client.DefaultHostPort
	}
	if cfg.Namespace == "" {
		cfg.Namespace = client.DefaultNamespace
	}
	if cfg.TaskQueue == "" {
		cfg.TaskQueue = DefaultTemporalTaskQueue
	}
	temporalClient, err := client.Dial(client.Options{HostPort: cfg.Address, Namespace: cfg.Namespace})
	if err != nil {
		return nil, fmt.Errorf("connect to Temporal: %w", err)
	}
	return &TemporalEngine{
		client:         temporalClient,
		taskQueue:      cfg.TaskQueue,
		localArtifacts: LocalEngine{RepoRoot: cfg.RepoRoot, ArtifactsDir: cfg.ArtifactsDir, RawDir: cfg.RawDir},
	}, nil
}

func (e *TemporalEngine) Run(ctx context.Context, runID string) error {
	rawDir := e.localArtifacts.RawDir
	if rawDir == "" {
		rawDir = filepath.Join(e.localArtifacts.RepoRoot, "sample_data", "raw")
	}
	job := PipelineJob{
		RunID:       runID,
		RawDirs:     []string{rawDir, filepath.Join(e.localArtifacts.ArtifactsDir, "staged")},
		ArtifactDir: filepath.Join(e.localArtifacts.ArtifactsDir, runID),
	}
	run, err := e.client.ExecuteWorkflow(ctx, client.StartWorkflowOptions{
		ID: "verity-" + runID, TaskQueue: e.taskQueue,
	}, PipelineWorkflow, job)
	if err != nil {
		return fmt.Errorf("start Temporal workflow: %w", err)
	}
	if err := run.Get(ctx, nil); err != nil {
		return fmt.Errorf("Temporal workflow failed: %w", err)
	}
	return nil
}

func (e *TemporalEngine) Preview(ctx context.Context, runID, stageID string, limit int) ([]json.RawMessage, error) {
	return e.localArtifacts.Preview(ctx, runID, stageID, limit)
}

func (e *TemporalEngine) Compare(ctx context.Context, runID, stageID string, limit int) ([]StageComparisonSample, error) {
	return e.localArtifacts.Compare(ctx, runID, stageID, limit)
}

func (e *TemporalEngine) Close() {
	e.client.Close()
}

func PipelineWorkflow(ctx workflow.Context, job PipelineJob) error {
	ctx = workflow.WithActivityOptions(ctx, workflow.ActivityOptions{
		StartToCloseTimeout: 15 * time.Minute,
		RetryPolicy: &temporal.RetryPolicy{
			InitialInterval: time.Second, BackoffCoefficient: 2,
			MaximumInterval: 30 * time.Second, MaximumAttempts: 3,
		},
	})
	return workflow.ExecuteActivity(ctx, PipelineActivity, job).Get(ctx, nil)
}

func PipelineActivity(ctx context.Context, job PipelineJob) error {
	_, err := RunPipeline(ctx, PipelineConfig{
		RunID: job.RunID, RawDirs: job.RawDirs, ArtifactDir: job.ArtifactDir,
	})
	return err
}

func RunTemporalWorker(temporalClient client.Client, taskQueue string, interrupt <-chan interface{}) error {
	if taskQueue == "" {
		taskQueue = DefaultTemporalTaskQueue
	}
	workflowWorker := worker.New(temporalClient, taskQueue, worker.Options{})
	workflowWorker.RegisterWorkflow(PipelineWorkflow)
	workflowWorker.RegisterActivity(PipelineActivity)
	return workflowWorker.Run(interrupt)
}

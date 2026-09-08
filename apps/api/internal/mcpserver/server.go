package mcpserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	verityclient "github.com/realAllenSong/Verity/apps/api/internal/client"
)

type Server struct {
	api *verityclient.Client
}

type importInput struct {
	Path string `json:"path" jsonschema:"Absolute or working-directory-relative path to a local data file."`
	Wait bool   `json:"wait,omitempty" jsonschema:"Wait for a terminal state instead of returning the queued job."`
}

type jobInput struct {
	JobID string `json:"job_id" jsonschema:"Verity job identifier."`
}

type jobOutput struct {
	JobID    string `json:"job_id"`
	ImportID string `json:"import_id"`
	State    string `json:"state"`
	RunID    string `json:"run_id,omitempty"`
	OutputID string `json:"output_id,omitempty"`
	Action   string `json:"action,omitempty"`
}

type inspectInput struct {
	StageID string `json:"stage_id" jsonschema:"Pipeline stage identifier such as raw, privacy, quality, signals, review, or curated."`
	Cursor  string `json:"cursor,omitempty" jsonschema:"Opaque cursor returned by a previous call."`
	Limit   int    `json:"limit,omitempty" jsonschema:"Number of summaries to return, from 1 to 50."`
}

type inspectOutput struct {
	StageID    string           `json:"stage_id"`
	Returned   int              `json:"returned"`
	NextCursor string           `json:"next_cursor,omitempty"`
	Records    []map[string]any `json:"records"`
}

type reviewListInput struct {
	Limit int `json:"limit,omitempty" jsonschema:"Maximum review summaries to return, from 1 to 50."`
}

type reviewSummary struct {
	RecordID        string  `json:"record_id"`
	SignalType      string  `json:"signal_type,omitempty"`
	ExtractedSignal string  `json:"extracted_signal,omitempty"`
	Confidence      float64 `json:"confidence,omitempty"`
	Reason          string  `json:"reason,omitempty"`
}

type reviewListOutput struct {
	Total    int             `json:"total"`
	Returned int             `json:"returned"`
	Records  []reviewSummary `json:"records"`
}

type reviewInput struct {
	RecordID string `json:"record_id" jsonschema:"Record identifier from verity_list_reviews."`
	Decision string `json:"decision" jsonschema:"One of accepted, modified, or rejected."`
	Reason   string `json:"reason" jsonschema:"Human reason preserved in review history."`
}

type reviewOutput struct {
	RecordID string `json:"record_id"`
	Decision string `json:"decision"`
	Saved    bool   `json:"saved"`
}

type exportInput struct {
	OutputID string `json:"output_id" jsonschema:"Published output identifier from a completed job."`
	Path     string `json:"path" jsonschema:"Local destination path. Existing files are atomically replaced only after checksum validation."`
}

func New(api *verityclient.Client) *mcp.Server {
	service := &Server{api: api}
	server := mcp.NewServer(&mcp.Implementation{Name: "verity", Title: "Verity data workbench", Version: "0.4.0"}, nil)
	readOnly := &mcp.ToolAnnotations{ReadOnlyHint: true, OpenWorldHint: boolPointer(false)}
	additive := &mcp.ToolAnnotations{DestructiveHint: boolPointer(false), IdempotentHint: true, OpenWorldHint: boolPointer(false)}
	fileWrite := &mcp.ToolAnnotations{DestructiveHint: boolPointer(true), IdempotentHint: true, OpenWorldHint: boolPointer(false)}

	mcp.AddTool(server, &mcp.Tool{Name: "verity_import", Title: "Import a local dataset", Description: "Streams a local CSV, TSV, JSON, JSONL, NDJSON, Parquet, or gzip file into Verity and starts its default workflow automatically.", Annotations: additive}, service.importFile)
	mcp.AddTool(server, &mcp.Tool{Name: "verity_job_status", Title: "Read job status", Description: "Returns one bounded import and pipeline lifecycle summary.", Annotations: readOnly}, service.jobStatus)
	mcp.AddTool(server, &mcp.Tool{Name: "verity_inspect_stage", Title: "Inspect a pipeline stage", Description: "Returns a bounded page of privacy-safe record summaries. Raw bodies, prompts, responses, payloads, and file bytes are excluded.", Annotations: readOnly}, service.inspectStage)
	mcp.AddTool(server, &mcp.Tool{Name: "verity_list_reviews", Title: "List review work", Description: "Lists bounded uncertain-record summaries without raw private content.", Annotations: readOnly}, service.listReviews)
	mcp.AddTool(server, &mcp.Tool{Name: "verity_submit_review", Title: "Submit a review decision", Description: "Records a human decision and required reason for one uncertain record.", Annotations: additive}, service.submitReview)
	mcp.AddTool(server, &mcp.Tool{Name: "verity_export", Title: "Export a published result", Description: "Downloads one published output to a local path, atomically replacing that path after SHA-256 validation.", Annotations: fileWrite}, service.export)
	return server
}

func (s *Server) importFile(ctx context.Context, request *mcp.CallToolRequest, input importInput) (*mcp.CallToolResult, jobOutput, error) {
	if strings.TrimSpace(input.Path) == "" {
		return nil, jobOutput{}, errors.New("path is required")
	}
	if token := request.Params.GetProgressToken(); token != nil {
		_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 0, Total: 1, Message: "Uploading and preparing data"})
	}
	job, err := s.api.ImportFile(ctx, input.Path)
	if err != nil {
		return nil, jobOutput{}, err
	}
	if input.Wait {
		job, err = s.api.WaitJob(ctx, job.ID, 350*time.Millisecond)
		if err != nil {
			return nil, jobOutput{}, err
		}
	}
	if token := request.Params.GetProgressToken(); token != nil {
		_ = request.Session.NotifyProgress(ctx, &mcp.ProgressNotificationParams{ProgressToken: token, Progress: 1, Total: 1, Message: "Import accepted"})
	}
	return nil, summarizeJob(job.ID, job.ImportID, string(job.State), job.RunID, job.OutputID), nil
}

func (s *Server) jobStatus(ctx context.Context, _ *mcp.CallToolRequest, input jobInput) (*mcp.CallToolResult, jobOutput, error) {
	job, err := s.api.Job(ctx, input.JobID)
	if err != nil {
		return nil, jobOutput{}, err
	}
	return nil, summarizeJob(job.ID, job.ImportID, string(job.State), job.RunID, job.OutputID), nil
}

func (s *Server) inspectStage(ctx context.Context, _ *mcp.CallToolRequest, input inspectInput) (*mcp.CallToolResult, inspectOutput, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return nil, inspectOutput{}, errors.New("limit must be between 1 and 50")
	}
	page, err := s.api.StageRecords(ctx, input.StageID, input.Cursor, limit)
	if err != nil {
		return nil, inspectOutput{}, err
	}
	rows := make([]map[string]any, 0, len(page.Rows))
	for _, raw := range page.Rows {
		var source map[string]any
		if err := json.Unmarshal(raw, &source); err != nil {
			return nil, inspectOutput{}, fmt.Errorf("decode stage summary: %w", err)
		}
		rows = append(rows, safeRecordSummary(source))
	}
	return nil, inspectOutput{StageID: page.StageID, Returned: len(rows), NextCursor: page.NextCursor, Records: rows}, nil
}

func (s *Server) listReviews(ctx context.Context, _ *mcp.CallToolRequest, input reviewListInput) (*mcp.CallToolResult, reviewListOutput, error) {
	limit := input.Limit
	if limit == 0 {
		limit = 20
	}
	if limit < 1 || limit > 50 {
		return nil, reviewListOutput{}, errors.New("limit must be between 1 and 50")
	}
	queue, err := s.api.Reviews(ctx)
	if err != nil {
		return nil, reviewListOutput{}, err
	}
	count := min(limit, len(queue.Records))
	result := reviewListOutput{Total: queue.Count, Returned: count, Records: make([]reviewSummary, 0, count)}
	for _, record := range queue.Records[:count] {
		result.Records = append(result.Records, reviewSummary{RecordID: record.ID, SignalType: record.SignalType, ExtractedSignal: record.ExtractedSignal, Confidence: record.Confidence, Reason: record.Reason})
	}
	return nil, result, nil
}

func (s *Server) submitReview(ctx context.Context, _ *mcp.CallToolRequest, input reviewInput) (*mcp.CallToolResult, reviewOutput, error) {
	if input.Decision != "accepted" && input.Decision != "modified" && input.Decision != "rejected" {
		return nil, reviewOutput{}, errors.New("decision must be accepted, modified, or rejected")
	}
	if strings.TrimSpace(input.Reason) == "" {
		return nil, reviewOutput{}, errors.New("reason is required")
	}
	if _, err := s.api.SubmitReview(ctx, input.RecordID, input.Decision, input.Reason); err != nil {
		return nil, reviewOutput{}, err
	}
	return nil, reviewOutput{RecordID: input.RecordID, Decision: input.Decision, Saved: true}, nil
}

func (s *Server) export(ctx context.Context, _ *mcp.CallToolRequest, input exportInput) (*mcp.CallToolResult, verityclient.DownloadResult, error) {
	result, err := s.api.DownloadOutput(ctx, input.OutputID, input.Path)
	return nil, result, err
}

func summarizeJob(jobID, importID, state, runID, outputID string) jobOutput {
	result := jobOutput{JobID: jobID, ImportID: importID, State: state, RunID: runID, OutputID: outputID}
	if state == "needs_input" {
		result.Action = "Use verity_list_reviews, then submit explicit decisions."
	}
	return result
}

func safeRecordSummary(record map[string]any) map[string]any {
	allowed := map[string]struct{}{
		"record_id": {}, "event_id": {}, "batch_id": {}, "kind": {}, "occurred_at": {}, "signal_type": {},
		"extracted_signal": {}, "confidence": {}, "quality_score": {}, "decision": {}, "reason": {}, "privacy": {},
	}
	result := make(map[string]any)
	for key, value := range record {
		if _, ok := allowed[key]; ok {
			result[key] = value
		}
	}
	return result
}

func boolPointer(value bool) *bool { return &value }

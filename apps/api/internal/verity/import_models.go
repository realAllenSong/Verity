package verity

import "encoding/json"

type LifecycleState string

const (
	ImportCreated   LifecycleState = "created"
	ImportUploading LifecycleState = "uploading"
	ImportProfiling LifecycleState = "profiling"
	ImportQueued    LifecycleState = "queued"
	ImportRunning   LifecycleState = "running"
	ImportSucceeded LifecycleState = "succeeded"
	ImportFailed    LifecycleState = "failed"
	ImportCanceled  LifecycleState = "canceled"

	JobQueued     LifecycleState = "queued"
	JobRunning    LifecycleState = "running"
	JobNeedsInput LifecycleState = "needs_input"
	JobSucceeded  LifecycleState = "succeeded"
	JobFailed     LifecycleState = "failed"
	JobCanceled   LifecycleState = "canceled"
)

type ImportCreate struct {
	DatasetID string `json:"dataset_id"`
	Filename  string `json:"filename"`
	MediaType string `json:"media_type,omitempty"`
	SizeBytes int64  `json:"size_bytes"`
}

type ImportSummary struct {
	ImportID   string         `json:"import_id"`
	UploadID   string         `json:"upload_id"`
	UploadURL  string         `json:"upload_url"`
	DatasetID  string         `json:"dataset_id"`
	Filename   string         `json:"filename"`
	MediaType  string         `json:"media_type,omitempty"`
	SizeBytes  int64          `json:"size_bytes"`
	Offset     int64          `json:"offset"`
	Format     string         `json:"format,omitempty"`
	State      LifecycleState `json:"state"`
	JobID      string         `json:"job_id,omitempty"`
	Error      string         `json:"error,omitempty"`
	CreatedAt  string         `json:"created_at"`
	FinishedAt string         `json:"finished_at,omitempty"`
}

type JobSummary struct {
	ID        string         `json:"job_id"`
	ImportID  string         `json:"import_id"`
	RunID     string         `json:"run_id,omitempty"`
	OutputID  string         `json:"output_id,omitempty"`
	State     LifecycleState `json:"state"`
	StatusURL string         `json:"status_url"`
	EventsURL string         `json:"events_url"`
	Error     string         `json:"error,omitempty"`
	CreatedAt string         `json:"created_at"`
	UpdatedAt string         `json:"updated_at"`
}

type JobEvent struct {
	JobID            string `json:"job_id"`
	Sequence         uint64 `json:"sequence"`
	EventType        string `json:"event_type"`
	StageID          string `json:"stage_id,omitempty"`
	CompletedRecords int    `json:"completed_records,omitempty"`
	TotalRecords     int    `json:"total_records,omitempty"`
	Timestamp        string `json:"timestamp"`
	Message          string `json:"message,omitempty"`
}

type StagePage struct {
	StageID    string            `json:"stage_id"`
	Rows       []json.RawMessage `json:"rows"`
	Returned   int               `json:"returned"`
	NextCursor string            `json:"next_cursor,omitempty"`
}

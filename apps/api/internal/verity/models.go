package verity

import "encoding/json"

type DatasetSummary struct {
	ID             string         `json:"id"`
	Name           string         `json:"name"`
	Description    string         `json:"description"`
	RecordCount    int            `json:"record_count"`
	FieldCount     int            `json:"field_count"`
	BatchCount     int            `json:"batch_count"`
	UpdatedAt      string         `json:"updated_at"`
	Completeness   float64        `json:"completeness"`
	Validity       float64        `json:"validity"`
	State          string         `json:"state"`
	SchemaContract SchemaContract `json:"schema_contract"`
}

type SchemaContract struct {
	Columns     string `json:"columns"`
	DataTypes   string `json:"data_types"`
	OnViolation string `json:"on_violation"`
}

type BatchSummary struct {
	ID          string `json:"id"`
	Filename    string `json:"filename"`
	AddedAt     string `json:"added_at"`
	RecordCount int    `json:"record_count"`
	FieldCount  int    `json:"field_count"`
	State       string `json:"state"`
	Checksum    string `json:"checksum,omitempty"`
}

type QualityCheck struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	State    string `json:"state"`
	Severity string `json:"severity"`
	Observed string `json:"observed"`
}

type PipelineStage struct {
	ID          string         `json:"id"`
	Label       string         `json:"label"`
	Count       int            `json:"count"`
	InputCount  int            `json:"input_count"`
	Description string         `json:"description"`
	Operator    string         `json:"operator"`
	Status      string         `json:"status"`
	Checks      []QualityCheck `json:"checks,omitempty"`
}

type RecipeOperator struct {
	ID          string `json:"id"`
	Label       string `json:"label"`
	Description string `json:"description"`
	Operator    string `json:"operator"`
	Version     string `json:"version"`
	State       string `json:"state"`
}

type RecipeSummary struct {
	ID        string           `json:"id"`
	Name      string           `json:"name"`
	Version   int              `json:"version"`
	State     string           `json:"state"`
	UpdatedAt string           `json:"updated_at"`
	Operators []RecipeOperator `json:"operators"`
}

type RunSummary struct {
	ID              string `json:"id"`
	RecipeVersion   int    `json:"recipe_version"`
	StartedAt       string `json:"started_at"`
	DurationSeconds int    `json:"duration_seconds"`
	RecordCount     int    `json:"record_count"`
	ReadyCount      int    `json:"ready_count"`
	ReviewCount     int    `json:"review_count"`
	State           string `json:"state"`
	Attempt         int    `json:"attempt,omitempty"`
	FailureReason   string `json:"failure_reason,omitempty"`
}

type RunEvent struct {
	RunID     string `json:"run_id"`
	EventType string `json:"event_type"`
	EventTime string `json:"event_time"`
	Job       string `json:"job"`
	Message   string `json:"message,omitempty"`
}

type OutputSummary struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Format      string `json:"format"`
	RecordCount int    `json:"record_count"`
	CreatedAt   string `json:"created_at"`
	Size        string `json:"size"`
	State       string `json:"state"`
}

type EvidenceRecord struct {
	ID              string            `json:"id"`
	BatchID         string            `json:"batch_id"`
	BeforeFields    map[string]string `json:"before_fields"`
	AfterFields     map[string]string `json:"after_fields"`
	RawEvent        string            `json:"raw_event"`
	ExtractedSignal string            `json:"extracted_signal"`
	SignalType      string            `json:"signal_type"`
	Confidence      float64           `json:"confidence"`
	QualityScore    float64           `json:"quality_score"`
	Decision        string            `json:"decision"`
	Reason          string            `json:"reason"`
	OccurredAt      string            `json:"occurred_at"`
	Privacy         string            `json:"privacy"`
	EvidenceCount   int               `json:"evidence_count"`
	Metadata        map[string]string `json:"metadata"`
}

type DecisionBreakdown struct {
	Accepted int `json:"accepted"`
	Review   int `json:"review"`
	Rejected int `json:"rejected"`
}

type StepSettings struct {
	Operator       string  `json:"operator"`
	Version        string  `json:"version"`
	Policy         string  `json:"policy"`
	Threshold      float64 `json:"threshold"`
	CodeVersion    string  `json:"code_version"`
	InputSnapshot  string  `json:"input_snapshot"`
	OutputSnapshot string  `json:"output_snapshot"`
	RunID          string  `json:"run_id"`
}

type Workspace struct {
	Dataset            DatasetSummary      `json:"dataset"`
	GeneratedAt        string              `json:"generated_at"`
	RunID              string              `json:"run_id"`
	Batches            []BatchSummary      `json:"batches"`
	Recipe             RecipeSummary       `json:"recipe"`
	Runs               []RunSummary        `json:"runs"`
	RunEvents          []RunEvent          `json:"run_events,omitempty"`
	Outputs            []OutputSummary     `json:"outputs"`
	Stages             []PipelineStage     `json:"stages"`
	Records            []EvidenceRecord    `json:"records"`
	DecisionBreakdown  DecisionBreakdown   `json:"decision_breakdown"`
	StepSettings       StepSettings        `json:"step_settings"`
	SignalDistribution map[string]int      `json:"signal_distribution"`
	SchemaBefore       []map[string]string `json:"schema_before"`
	SchemaAfter        []map[string]string `json:"schema_after"`
	CopyPolicy         map[string]string   `json:"copy_policy"`
}

type BatchCreate struct {
	Filename string                   `json:"filename"`
	Records  []map[string]interface{} `json:"records"`
}

type BatchStageResponse struct {
	Batch        BatchSummary `json:"batch"`
	SampleFields []string     `json:"sample_fields"`
	Message      string       `json:"message"`
}

type ReviewUpdate struct {
	Decision string `json:"decision"`
	Note     string `json:"note"`
}

type RunResponse struct {
	RunID       string         `json:"run_id"`
	State       string         `json:"state"`
	StageCounts map[string]int `json:"stage_counts"`
}

type PreviewResponse struct {
	StageID string            `json:"stage_id"`
	Count   int               `json:"count"`
	Rows    []json.RawMessage `json:"rows"`
}

type StageComparisonSample struct {
	RecordID      string          `json:"record_id"`
	Outcome       string          `json:"outcome"`
	Before        json.RawMessage `json:"before,omitempty"`
	After         json.RawMessage `json:"after,omitempty"`
	Reason        string          `json:"reason,omitempty"`
	ChangedFields []string        `json:"changed_fields,omitempty"`
}

type StageComparisonResponse struct {
	StageID       string                  `json:"stage_id"`
	PreviousStage string                  `json:"previous_stage_id,omitempty"`
	InputCount    int                     `json:"input_count"`
	OutputCount   int                     `json:"output_count"`
	RemovedCount  int                     `json:"removed_count"`
	Samples       []StageComparisonSample `json:"samples"`
}

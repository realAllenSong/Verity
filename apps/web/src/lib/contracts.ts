export type Decision = "accepted" | "rejected" | "modified" | "review";
export type PageName = "pipeline" | "data" | "recipes" | "runs" | "review" | "outputs";

export interface DatasetSummary {
  id: string;
  name: string;
  description: string;
  record_count: number;
  field_count: number;
  batch_count: number;
  updated_at: string;
  completeness: number;
  validity: number;
  state: "ready" | "processing" | "attention";
  schema_contract?: SchemaContract;
}

export interface SchemaContract {
  columns: "evolve" | "freeze";
  data_types: "evolve" | "freeze";
  on_violation: "fail run" | "quarantine row" | "discard value";
}

export interface BatchSummary {
  id: string;
  filename: string;
  added_at: string;
  record_count: number;
  field_count: number;
  state: "complete" | "staged" | "failed";
  checksum?: string;
}

export interface QualityCheck {
  id: string;
  label: string;
  state: "passed" | "warning" | "failed";
  severity: "info" | "warning" | "blocking";
  observed: string;
}

export interface PipelineStage {
  id: string;
  label: string;
  count: number;
  input_count: number;
  description: string;
  operator: string;
  status: "complete" | "review" | "idle";
  checks?: QualityCheck[];
}

export interface RecipeOperator {
  id: string;
  label: string;
  description: string;
  operator: string;
  version: string;
  state: "configured" | "draft" | "disabled";
}

export interface RecipeSummary {
  id: string;
  name: string;
  version: number;
  state: "published" | "draft";
  updated_at: string;
  operators: RecipeOperator[];
}

export interface RunSummary {
  id: string;
  recipe_version: number;
  started_at: string;
  duration_seconds: number;
  record_count: number;
  ready_count: number;
  review_count: number;
  state: "succeeded" | "warning" | "failed" | "running";
  attempt?: number;
  failure_reason?: string | null;
}

export interface RunEvent {
  run_id: string;
  event_type: "START" | "RUNNING" | "COMPLETE" | "FAIL";
  event_time: string;
  job: string;
  message?: string | null;
}

export interface OutputSummary {
  id: string;
  name: string;
  format: "parquet" | "jsonl" | "csv";
  record_count: number;
  created_at: string;
  size: string;
  state: "ready" | "building" | "expired";
}

export interface EvidenceRecord {
  id: string;
  batch_id: string;
  before_fields: Record<string, string>;
  after_fields: Record<string, string>;
  raw_event: string;
  extracted_signal: string;
  signal_type: string;
  confidence: number;
  quality_score: number;
  decision: Decision;
  reason: string;
  occurred_at: string;
  privacy: string;
  evidence_count: number;
  metadata: Record<string, string>;
}

export interface StageComparisonSample {
  record_id: string;
  outcome: "input" | "normalized" | "kept" | "filtered" | "routed";
  before?: Record<string, unknown>;
  after?: Record<string, unknown>;
  reason?: string;
  changed_fields?: string[];
}

export interface StageComparison {
  stage_id: string;
  previous_stage_id?: string;
  input_count: number;
  output_count: number;
  removed_count: number;
  samples: StageComparisonSample[];
}

export interface WorkspaceData {
  dataset: DatasetSummary;
  generated_at: string;
  run_id: string;
  batches: BatchSummary[];
  recipe: RecipeSummary;
  runs: RunSummary[];
  outputs: OutputSummary[];
  stages: PipelineStage[];
  records: EvidenceRecord[];
  decision_breakdown: { accepted: number; review: number; rejected: number };
  step_settings: {
    operator: string;
    version: string;
    policy: string;
    threshold: number;
    code_version: string;
    input_snapshot: string;
    output_snapshot: string;
    run_id: string;
  };
  signal_distribution: Record<string, number>;
  schema_before: Array<{ field: string; type: string; policy: string }>;
  schema_after: Array<{ field: string; type: string; policy: string }>;
  copy_policy: { raw: string; cloud: string };
  run_events?: RunEvent[];
}

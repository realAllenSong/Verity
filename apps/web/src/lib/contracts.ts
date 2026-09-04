export type SourceName = string;

export type Decision = "accepted" | "rejected" | "modified" | "review";

export interface SourceSummary {
  id: SourceName;
  label: string;
  count: number;
  state: "healthy" | "warning" | "offline";
}

export interface PipelineStage {
  id: string;
  label: string;
  count: number;
  input_count: number;
  description: string;
  operator: string;
  status: "complete" | "review" | "idle";
}

export interface EvidenceRecord {
  id: string;
  source: SourceName;
  source_label: string;
  raw_event: string;
  extracted_signal: string;
  signal_type: string;
  confidence: number;
  decision: Decision;
  reason: string;
  occurred_at: string;
  privacy: string;
  evidence_count: number;
}

export interface WorkspaceData {
  project: { id: string; name: string; description: string };
  generated_at: string;
  run_id: string;
  sources: SourceSummary[];
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
}

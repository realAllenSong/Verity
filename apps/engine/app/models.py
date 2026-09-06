from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field


class DataRecord(BaseModel):
    """Portable input contract. Origin-specific values belong in metadata."""

    record_id: str = Field(min_length=1)
    dataset_id: str = Field(min_length=1, pattern=r"^[a-z0-9][a-z0-9._-]*$")
    batch_id: str = Field(min_length=1, pattern=r"^[a-z0-9][a-z0-9._-]*$")
    ingested_at: str
    payload: dict[str, Any]
    metadata: dict[str, Any] = Field(default_factory=dict)


class DatasetSummary(BaseModel):
    id: str
    name: str
    description: str
    record_count: int
    field_count: int
    batch_count: int
    updated_at: str
    completeness: float
    validity: float
    state: Literal["ready", "processing", "attention"] = "ready"
    schema_contract: SchemaContract


class SchemaContract(BaseModel):
    columns: Literal["evolve", "freeze"]
    data_types: Literal["evolve", "freeze"]
    on_violation: Literal["fail run", "quarantine row", "discard value"]


class BatchSummary(BaseModel):
    id: str
    filename: str
    added_at: str
    record_count: int
    field_count: int
    state: Literal["complete", "staged", "failed"] = "complete"


class PipelineStage(BaseModel):
    id: str
    label: str
    count: int
    input_count: int
    description: str
    operator: str
    status: Literal["complete", "review", "idle"] = "complete"
    checks: list[QualityCheck] = Field(default_factory=list)


class QualityCheck(BaseModel):
    id: str
    label: str
    state: Literal["passed", "warning", "failed"]
    severity: Literal["info", "warning", "blocking"]
    observed: str


class RecipeOperator(BaseModel):
    id: str
    label: str
    description: str
    operator: str
    version: str
    state: Literal["configured", "draft", "disabled"] = "configured"


class RecipeSummary(BaseModel):
    id: str
    name: str
    version: int
    state: Literal["published", "draft"]
    updated_at: str
    operators: list[RecipeOperator]


class RunSummary(BaseModel):
    id: str
    recipe_version: int
    started_at: str
    duration_seconds: int
    record_count: int
    ready_count: int
    review_count: int
    state: Literal["succeeded", "warning", "failed", "running"]
    attempt: int = 1
    failure_reason: str | None = None


class RunEvent(BaseModel):
    run_id: str
    event_type: Literal["START", "RUNNING", "COMPLETE", "ABORT", "FAIL"]
    event_time: str
    job: str
    message: str | None = None


class OutputSummary(BaseModel):
    id: str
    name: str
    format: Literal["parquet", "jsonl", "csv"]
    record_count: int
    created_at: str
    size: str
    state: Literal["ready", "building", "expired"] = "ready"


class EvidenceRecord(BaseModel):
    id: str
    batch_id: str
    before_fields: dict[str, str]
    after_fields: dict[str, str]
    raw_event: str
    extracted_signal: str
    signal_type: str
    confidence: float
    quality_score: float
    decision: Literal["accepted", "rejected", "modified", "review"]
    reason: str
    occurred_at: str
    privacy: str = "shareable summary"
    evidence_count: int = 1
    metadata: dict[str, str] = Field(default_factory=dict)


class DecisionBreakdown(BaseModel):
    accepted: int
    review: int
    rejected: int


class StepSettings(BaseModel):
    operator: str
    version: str
    policy: str
    threshold: float
    code_version: str
    input_snapshot: str
    output_snapshot: str
    run_id: str


class WorkspaceResponse(BaseModel):
    dataset: DatasetSummary
    generated_at: str
    run_id: str
    batches: list[BatchSummary]
    recipe: RecipeSummary
    runs: list[RunSummary]
    run_events: list[RunEvent] = Field(default_factory=list)
    outputs: list[OutputSummary]
    stages: list[PipelineStage]
    records: list[EvidenceRecord]
    decision_breakdown: DecisionBreakdown
    step_settings: StepSettings
    signal_distribution: dict[str, int]
    schema_before: list[dict[str, str]]
    schema_after: list[dict[str, str]]
    copy_policy: dict[str, str]


class ReviewUpdate(BaseModel):
    decision: Literal["accepted", "rejected", "modified"]
    note: str = Field(default="", max_length=500)


class BatchCreate(BaseModel):
    filename: str = Field(min_length=1, max_length=180)
    records: list[dict[str, Any]] = Field(min_length=1, max_length=10_000)


class BatchStageResponse(BaseModel):
    batch: BatchSummary
    sample_fields: list[str]
    message: str


class RunResponse(BaseModel):
    run_id: str
    state: Literal["succeeded", "failed"]
    stage_counts: dict[str, int]

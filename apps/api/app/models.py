from __future__ import annotations

from typing import Any, Literal

from pydantic import BaseModel, Field

SourceName = str


class RawEvent(BaseModel):
    event_id: str
    source: SourceName = Field(min_length=1, pattern=r"^[a-z0-9][a-z0-9._-]*$")
    kind: str
    occurred_at: str | None = None
    actor: str | None = None
    thread_id: str | None = None
    title: str | None = None
    content: str | None = None
    status: str | None = None
    metadata: dict[str, Any] = Field(default_factory=dict)


class SourceSummary(BaseModel):
    id: SourceName
    label: str
    count: int
    state: Literal["healthy", "warning", "offline"] = "healthy"


class PipelineStage(BaseModel):
    id: str
    label: str
    count: int
    input_count: int
    description: str
    operator: str
    status: Literal["complete", "review", "idle"] = "complete"


class EvidenceRecord(BaseModel):
    id: str
    source: SourceName
    source_label: str
    raw_event: str
    extracted_signal: str
    signal_type: str
    confidence: float
    decision: Literal["accepted", "rejected", "modified", "review"]
    reason: str
    occurred_at: str
    privacy: str = "manager-safe summary"
    evidence_count: int = 1


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
    project: dict[str, str]
    generated_at: str
    run_id: str
    sources: list[SourceSummary]
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


class RunResponse(BaseModel):
    run_id: str
    state: Literal["succeeded", "failed"]
    stage_counts: dict[str, int]

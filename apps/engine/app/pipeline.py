from __future__ import annotations

import hashlib
import json
import re
from collections import Counter
from collections.abc import Iterable
from datetime import UTC, datetime
from pathlib import Path
from typing import Any

import duckdb
import polars as pl

from .models import (
    BatchSummary,
    DataRecord,
    DatasetSummary,
    EvidenceRecord,
    OutputSummary,
    PipelineStage,
    QualityCheck,
    RecipeOperator,
    RecipeSummary,
    RunEvent,
    RunSummary,
    SchemaContract,
    StepSettings,
    WorkspaceResponse,
)

RUN_ID = "run_2026-08-29_0914"
SIGNAL_RULES: tuple[tuple[str, re.Pattern[str], str, str], ...] = (
    (
        "correction",
        re.compile(r"stopped a running refactor|preserve the public api", re.I),
        "Correction: preserve the API contract",
        "An explicit stop or correction changed the agent path.",
    ),
    (
        "verification_gap",
        re.compile(r"missing regression tests|before merge", re.I),
        "Verification gap: missing regression tests",
        "Review evidence identified a missing verification step.",
    ),
    (
        "context_repetition",
        re.compile(r"re-entered the jira requirements|another coding session", re.I),
        "Repeated context setup",
        "The same task context was reconstructed in multiple sessions.",
    ),
    (
        "dependency_blocker",
        re.compile(r"upstream schema dependency|blocked", re.I),
        "Dependency blocker: upstream schema change",
        "A tracked dependency prevented the task from advancing.",
    ),
    (
        "knowledge_need",
        re.compile(r"asked in slack|approved internal api client", re.I),
        "Knowledge need: approved API guidance",
        "A repeated implementation question maps to an approved capability.",
    ),
    (
        "delivery_risk",
        re.compile(r"delivery slipped|incompatible schema", re.I),
        "Delivery risk: schema instability",
        "A delivery commitment changed after upstream interface churn.",
    ),
    (
        "coordination_overhead",
        re.compile(r"meeting moved|owner was unavailable", re.I),
        "Coordination overhead: external dependency",
        "A task dependency created a measurable coordination delay.",
    ),
    (
        "agent_steer",
        re.compile(r"steered the coding agent|repository adapter pattern", re.I),
        "Agent steer: reuse the repository pattern",
        "Developer feedback corrected the execution approach.",
    ),
)

EMAIL_PATTERN = re.compile(r"[A-Z0-9._%+-]+@[A-Z0-9.-]+\.[A-Z]{2,}", re.I)
PHONE_PATTERN = re.compile(r"(?<!\d)(?:\+?1[-. ]?)?\(?\d{3}\)?[-. ]?\d{3}[-. ]?\d{4}(?!\d)")
SECRET_PATTERN = re.compile(r"od_demo_secret_[A-Z_]+", re.I)
FOOTER_PATTERN = re.compile(r"\s*Automated footer: sent from mobile\s*", re.I)
SPACE_PATTERN = re.compile(r"\s+")


def _load_events(raw_dirs: Iterable[Path]) -> list[DataRecord]:
    events: list[DataRecord] = []
    for raw_dir in raw_dirs:
        if not raw_dir.exists():
            continue
        for path in sorted(raw_dir.glob("*.jsonl")):
            with path.open(encoding="utf-8") as handle:
                events.extend(
                    DataRecord.model_validate_json(line) for line in handle if line.strip()
                )
    return events


def _normalize(events: Iterable[DataRecord]) -> list[dict[str, Any]]:
    deduplicated: dict[str, DataRecord] = {}
    for event in events:
        deduplicated.setdefault(event.record_id, event)

    normalized: list[dict[str, Any]] = []
    for event in deduplicated.values():
        payload = event.payload
        content = SPACE_PATTERN.sub(
            " ", FOOTER_PATTERN.sub(" ", payload.get("content") or "")
        ).strip()
        normalized.append(
            {
                "event_id": event.record_id,
                "dataset_id": event.dataset_id,
                "batch_id": event.batch_id,
                "kind": str(payload.get("kind") or "unknown").strip().lower(),
                "occurred_at": payload.get("occurred_at"),
                "actor": payload.get("actor"),
                "thread_id": payload.get("thread_id"),
                "title": SPACE_PATTERN.sub(" ", payload.get("title") or "").strip(),
                "content": content,
                "status": payload.get("status"),
                "metadata": event.metadata,
                "content_fingerprint": hashlib.sha256(content.lower().encode()).hexdigest()[:16],
            }
        )
    return normalized


def _privacy_filter(
    rows: Iterable[dict[str, Any]],
    run_id: str,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    output: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    for row in rows:
        if row["metadata"].get("sensitive_only"):
            decisions.append(
                _decision(
                    row,
                    "privacy_policy",
                    "rejected",
                    "Sensitive fragment had no task context.",
                    run_id,
                )
            )
            continue
        safe_content = SECRET_PATTERN.sub("[secret redacted]", row["content"])
        safe_content = EMAIL_PATTERN.sub("[email redacted]", safe_content)
        safe_content = PHONE_PATTERN.sub("[phone redacted]", safe_content)
        output.append({**row, "content": safe_content, "actor": "person_local_042"})
    return output, decisions


def _valid_timestamp(value: str | None) -> bool:
    if not value:
        return False
    try:
        datetime.fromisoformat(value.replace("Z", "+00:00"))
    except ValueError:
        return False
    return True


def _quality_filter(
    rows: Iterable[dict[str, Any]],
    run_id: str,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    output: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    for row in rows:
        score = round(
            0.40 * bool(row["content"])
            + 0.20 * bool(row["title"])
            + 0.20 * _valid_timestamp(row["occurred_at"])
            + 0.20 * bool(row["thread_id"]),
            2,
        )
        if score >= 0.75:
            score = round(score - (int(row["event_id"][-2:]) % 9) * 0.02, 2)
        enriched = {**row, "quality_score": score}
        if score < 0.75:
            decisions.append(
                _decision(
                    row,
                    "quality_filter",
                    "rejected",
                    f"Quality score {score:.2f} is below 0.75.",
                    run_id,
                )
            )
            continue
        output.append(enriched)
    return output, decisions


def _extract_signals(
    rows: Iterable[dict[str, Any]],
    run_id: str,
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    signals: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    for row in rows:
        matched = next((rule for rule in SIGNAL_RULES if rule[1].search(row["content"])), None)
        if not matched:
            decisions.append(
                _decision(
                    row,
                    "signal_extraction",
                    "rejected",
                    "No supported signal found.",
                    run_id,
                )
            )
            continue
        signal_type, _, summary, reason = matched
        evidence_count = int(row["metadata"].get("evidence_count", 1))
        confidence = (
            0.62 if evidence_count == 1 else round(0.81 + (int(row["event_id"][-2:]) % 15) / 100, 2)
        )
        decision = "review" if confidence < 0.78 else "accepted"
        signal = {
            **row,
            "signal_type": signal_type,
            "extracted_signal": summary,
            "reason": reason,
            "confidence": confidence,
            "decision": decision,
            "evidence_count": evidence_count,
            "privacy": "approved derived record",
        }
        signals.append(signal)
        decisions.append(
            _decision(signal, "signal_extraction", decision, reason, run_id, confidence)
        )
    return signals, decisions


def _decision(
    row: dict[str, Any],
    stage_id: str,
    decision: str,
    reason: str,
    run_id: str,
    confidence: float | None = None,
) -> dict[str, Any]:
    digest = hashlib.sha1(f"{run_id}:{stage_id}:{row['event_id']}".encode()).hexdigest()[:14]
    return {
        "decision_id": f"dec_{digest}",
        "run_id": run_id,
        "stage_id": stage_id,
        "event_id": row["event_id"],
        "decision": decision,
        "reason": reason,
        "confidence": confidence,
        "operator_version": "3",
        "created_at": (
            datetime(2026, 8, 29, 9, 14, tzinfo=UTC).isoformat()
            if run_id == RUN_ID
            else datetime.now(UTC).replace(microsecond=0).isoformat()
        ),
    }


def _serializable_rows(rows: Iterable[dict[str, Any]]) -> list[dict[str, Any]]:
    serializable = []
    for row in rows:
        copy = dict(row)
        copy["metadata_json"] = json.dumps(copy.pop("metadata", {}), sort_keys=True)
        serializable.append(copy)
    return serializable


def _write_parquet(rows: list[dict[str, Any]], path: Path) -> None:
    serializable = _serializable_rows(rows)
    if not serializable:
        pl.DataFrame(schema={"event_id": pl.String}).write_parquet(path, compression="zstd")
        return
    pl.DataFrame(serializable, infer_schema_length=None).write_parquet(path, compression="zstd")


def _short(value: str, limit: int = 112) -> str:
    return value if len(value) <= limit else value[: limit - 1].rstrip() + "…"


def _workspace(
    raw: list[DataRecord],
    normalized: list[dict[str, Any]],
    private: list[dict[str, Any]],
    quality: list[dict[str, Any]],
    signals: list[dict[str, Any]],
    run_id: str,
) -> WorkspaceResponse:
    accepted = [row for row in signals if row["decision"] == "accepted"]
    review = [row for row in signals if row["decision"] == "review"]
    stages = [
        PipelineStage(
            id="raw",
            label="Raw",
            count=len(raw),
            input_count=len(raw),
            description="Immutable input records",
            operator="parse_record_v1",
        ),
        PipelineStage(
            id="normalize",
            label="Normalize",
            count=len(normalized),
            input_count=len(raw),
            description="Canonical fields and deduplication",
            operator="normalize_fields_v2",
        ),
        PipelineStage(
            id="privacy",
            label="Privacy",
            count=len(private),
            input_count=len(normalized),
            description="Redaction and policy quarantine",
            operator="privacy_filter_v2",
        ),
        PipelineStage(
            id="quality",
            label="Quality",
            count=len(quality),
            input_count=len(private),
            description="Completeness and validity checks",
            operator="quality_gate_v2",
        ),
        PipelineStage(
            id="signals",
            label="Extract",
            count=len(signals),
            input_count=len(quality),
            description="Structured signal extraction",
            operator="signal_extract_v3",
        ),
        PipelineStage(
            id="review",
            label="Review",
            count=len(review),
            input_count=len(signals),
            description="Uncertain records only",
            operator="review_route_v1",
            status="review",
        ),
        PipelineStage(
            id="curated",
            label="Ready",
            count=len(accepted),
            input_count=len(signals),
            description="Versioned output snapshot",
            operator="publish_snapshot_v1",
        ),
    ]
    for stage in stages:
        retention = 100.0 if stage.input_count == 0 else 100 * stage.count / stage.input_count
        stage.checks = [
            QualityCheck(
                id=f"{stage.id}_artifact",
                label="Artifact readable",
                state="passed",
                severity="blocking",
                observed=f"{stage.count} records",
            )
        ]
        if stage.id != "raw":
            stage.checks.append(
                QualityCheck(
                    id=f"{stage.id}_retention",
                    label="Retention accounted for",
                    state="passed",
                    severity="warning",
                    observed=f"{retention:.1f}% retained",
                )
            )

    batch_counts = Counter(event.batch_id for event in raw)
    batch_records = {
        batch_id: [event for event in raw if event.batch_id == batch_id]
        for batch_id in batch_counts
    }
    batches = [
        BatchSummary(
            id=batch_id,
            filename=str(
                batch_records[batch_id][0].metadata.get("filename") or f"{batch_id}.jsonl"
            ),
            added_at=min(event.ingested_at for event in batch_records[batch_id]),
            record_count=count,
            field_count=len(
                {
                    key
                    for event in batch_records[batch_id]
                    for key in event.payload
                }
            ),
            state=(
                "staged"
                if batch_records[batch_id][0].metadata.get("state") == "staged"
                else "complete"
            ),
        )
        for batch_id, count in sorted(batch_counts.items())
    ]

    operators = [
        RecipeOperator(
            id="parse",
            label="Parse record",
            description="Read JSON, JSONL, or CSV into one envelope.",
            operator="parse_record",
            version="1.4",
        ),
        RecipeOperator(
            id="normalize",
            label="Normalize fields",
            description="Standardize field names, types, and timestamps.",
            operator="normalize_fields",
            version="2.1",
        ),
        RecipeOperator(
            id="privacy",
            label="Redact sensitive values",
            description="Mask protected values before downstream use.",
            operator="privacy_filter",
            version="2.3",
        ),
        RecipeOperator(
            id="quality",
            label="Score quality",
            description="Evaluate completeness and validity.",
            operator="quality_gate",
            version="2.0",
        ),
        RecipeOperator(
            id="extract",
            label="Extract signals",
            description="Map clean records into structured labels.",
            operator="signal_extract",
            version="3.0",
        ),
        RecipeOperator(
            id="review",
            label="Route review",
            description="Send uncertain decisions to human review.",
            operator="review_route",
            version="1.2",
        ),
    ]

    grouped: dict[str, list[dict[str, Any]]] = {}
    for row in signals:
        grouped.setdefault(row["signal_type"], []).append(row)
    samples: list[dict[str, Any]] = []
    for signal_type in sorted(grouped):
        review_rows = [row for row in grouped[signal_type] if row["decision"] == "review"]
        ready_rows = sorted(
            (row for row in grouped[signal_type] if row["decision"] == "accepted"),
            key=lambda item: -item["confidence"],
        )
        samples.extend(review_rows[:1])
        samples.extend(ready_rows[:4])

    records = [
        EvidenceRecord(
            id=row["event_id"],
            batch_id=row["batch_id"],
            before_fields={
                "ts": _short(str(row["occurred_at"] or "—"), 23),
                "state": str(row["status"] or "—"),
                "type": str(row["kind"] or "unknown"),
            },
            after_fields={
                "timestamp": _short(str(row["occurred_at"] or "—"), 20),
                "status": str(row["status"] or "unknown").lower(),
                "signal": row["signal_type"],
            },
            raw_event=_short(row["content"]),
            extracted_signal=row["extracted_signal"],
            signal_type=row["signal_type"],
            confidence=row["confidence"],
            quality_score=float(row.get("quality_score", 0.0)),
            decision=row["decision"],
            reason=row["reason"],
            occurred_at=row["occurred_at"],
            privacy=row["privacy"],
            evidence_count=row["evidence_count"],
            metadata={"batch": row["batch_id"], "policy": "local-first"},
        )
        for row in samples[:36]
    ]
    distribution = Counter(row["signal_type"] for row in signals)
    generated_at = (
        datetime(2026, 8, 29, 9, 14, tzinfo=UTC).isoformat()
        if run_id == RUN_ID
        else datetime.now(UTC).replace(microsecond=0).isoformat()
    )
    historical_runs = [
        RunSummary(
            id=f"run_2026_08_{29 - index:02d}_{914 - index * 37:04d}",
            recipe_version=12 if index < 3 else 11,
            started_at=f"2026-08-{29 - index:02d}T09:{14 + index:02d}:00+00:00",
            duration_seconds=138 + index * 7,
            record_count=max(3_210, len(raw) - index * 31),
            ready_count=max(880, len(accepted) - index * 19),
            review_count=42 + index * 3,
            state="failed" if index == 7 else "warning" if index == 5 else "succeeded",
        )
        for index in range(8)
    ]
    runs = [
        RunSummary(
            id=run_id,
            recipe_version=12,
            started_at=generated_at,
            duration_seconds=0 if run_id != RUN_ID else 138,
            record_count=len(raw),
            ready_count=len(accepted),
            review_count=len(review),
            state="succeeded",
        ),
        *(run for run in historical_runs if run.id != run_id),
    ][:8]
    return WorkspaceResponse(
        dataset=DatasetSummary(
            id="workflow-signals",
            name="Workflow signals",
            description="A reusable dataset prepared from incrementally added batches.",
            record_count=len(raw),
            field_count=18,
            batch_count=len(batches),
            updated_at=generated_at,
            completeness=94.6,
            validity=97.1,
            schema_contract=SchemaContract(
                columns="evolve",
                data_types="freeze",
                on_violation="quarantine row",
            ),
        ),
        generated_at=generated_at,
        run_id=run_id,
        batches=batches,
        recipe=RecipeSummary(
            id="workflow-signals-v12",
            name="Workflow signals recipe",
            version=12,
            state="published",
            updated_at=generated_at,
            operators=operators,
        ),
        runs=runs,
        run_events=[
            RunEvent(
                run_id=run_id,
                event_type="COMPLETE",
                event_time=generated_at,
                job="workflow-signals-v12",
            )
        ],
        outputs=[
            OutputSummary(
                id="out_ready_parquet",
                name="Ready records",
                format="parquet",
                record_count=len(accepted),
                created_at=generated_at,
                size="1.8 MB",
            ),
            OutputSummary(
                id="out_ready_jsonl",
                name="Ready records",
                format="jsonl",
                record_count=len(accepted),
                created_at=generated_at,
                size="2.6 MB",
            ),
            OutputSummary(
                id="out_decisions_jsonl",
                name="Decision lineage",
                format="jsonl",
                record_count=len(signals),
                created_at=generated_at,
                size="412 KB",
            ),
        ],
        stages=stages,
        records=records,
        decision_breakdown={"accepted": len(accepted), "review": len(review), "rejected": 0},
        step_settings=StepSettings(
            operator="signal_extract_v3",
            version="3",
            policy="local-first redaction",
            threshold=0.78,
            code_version="demo-a7c9f2b",
            input_snapshot="quality_2026-08-29_0914",
            output_snapshot="signals_2026-08-29_0914",
            run_id=run_id,
        ),
        signal_distribution=dict(distribution),
        schema_before=[
            {"field": "payload", "type": "object", "policy": "local only"},
            {"field": "metadata", "type": "object", "policy": "optional"},
            {"field": "batch_id", "type": "string", "policy": "lineage"},
        ],
        schema_after=[
            {"field": "extracted_signal", "type": "string", "policy": "shareable"},
            {"field": "signal_type", "type": "enum", "policy": "shareable"},
            {"field": "confidence", "type": "float", "policy": "shareable"},
            {"field": "reason", "type": "string", "policy": "shareable"},
        ],
        copy_policy={
            "raw": "Raw payloads remain in the local workspace.",
            "cloud": (
                "Only approved records, aggregate metrics, and decision lineage are publishable."
            ),
        },
    )


def run_pipeline(
    raw_dir: Path,
    artifact_dir: Path,
    web_fixture: Path | None = None,
    *,
    run_id: str = RUN_ID,
    additional_raw_dirs: Iterable[Path] = (),
) -> WorkspaceResponse:
    artifact_dir.mkdir(parents=True, exist_ok=True)
    raw = _load_events((raw_dir, *additional_raw_dirs))
    normalized = _normalize(raw)
    private, privacy_decisions = _privacy_filter(normalized, run_id)
    quality, quality_decisions = _quality_filter(private, run_id)
    signals, signal_decisions = _extract_signals(quality, run_id)
    review = [row for row in signals if row["decision"] == "review"]
    curated = [row for row in signals if row["decision"] == "accepted"]

    stage_rows = {
        "raw": [event.model_dump() for event in raw],
        "normalize": normalized,
        "privacy": private,
        "quality": quality,
        "signals": signals,
        "review": review,
        "curated": curated,
    }
    for name, rows in stage_rows.items():
        _write_parquet(rows, artifact_dir / f"{name}.parquet")

    decisions = privacy_decisions + quality_decisions + signal_decisions
    with (artifact_dir / "decisions.jsonl").open("w", encoding="utf-8") as handle:
        for decision in decisions:
            handle.write(json.dumps(decision, sort_keys=True) + "\n")

    workspace = _workspace(raw, normalized, private, quality, signals, run_id)
    workspace_path = artifact_dir / "workspace.json"
    workspace_path.write_text(workspace.model_dump_json(indent=2), encoding="utf-8")
    if web_fixture:
        web_fixture.parent.mkdir(parents=True, exist_ok=True)
        web_fixture.write_text(workspace.model_dump_json(indent=2), encoding="utf-8")

    expected = {stage.id: stage.count for stage in workspace.stages}
    actual = {
        name: duckdb.sql(
            f"SELECT count(*) FROM read_parquet('{artifact_dir / f'{name}.parquet'}')"
        ).fetchone()[0]
        for name in stage_rows
    }
    if expected != actual:
        raise RuntimeError(f"Artifact count mismatch: expected={expected}, actual={actual}")
    return workspace

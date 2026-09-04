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
    EvidenceRecord,
    PipelineStage,
    RawEvent,
    SourceSummary,
    StepSettings,
    WorkspaceResponse,
)

RUN_ID = "run_2026-08-29_0914"
SOURCE_LABELS = {
    "codex": "Codex",
    "claude_code": "Claude Code",
    "github": "GitHub",
    "jira": "Jira",
    "slack": "Slack",
    "outlook": "Outlook",
    "calendar": "Calendar",
    "confluence": "Confluence",
    "powerpoint": "PowerPoint",
}

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


def _load_events(raw_dir: Path) -> list[RawEvent]:
    events: list[RawEvent] = []
    for path in sorted(raw_dir.glob("*.jsonl")):
        with path.open(encoding="utf-8") as handle:
            events.extend(RawEvent.model_validate_json(line) for line in handle if line.strip())
    return events


def _normalize(events: Iterable[RawEvent]) -> list[dict[str, Any]]:
    deduplicated: dict[str, RawEvent] = {}
    for event in events:
        deduplicated.setdefault(event.event_id, event)

    normalized: list[dict[str, Any]] = []
    for event in deduplicated.values():
        content = SPACE_PATTERN.sub(" ", FOOTER_PATTERN.sub(" ", event.content or "")).strip()
        normalized.append(
            {
                "event_id": event.event_id,
                "source": event.source,
                "source_label": SOURCE_LABELS.get(
                    event.source, event.source.replace("_", " ").replace("-", " ").title()
                ),
                "kind": event.kind.strip().lower(),
                "occurred_at": event.occurred_at,
                "actor": event.actor,
                "thread_id": event.thread_id,
                "title": SPACE_PATTERN.sub(" ", event.title or "").strip(),
                "content": content,
                "status": event.status,
                "metadata": event.metadata,
                "content_fingerprint": hashlib.sha256(content.lower().encode()).hexdigest()[:16],
            }
        )
    return normalized


def _privacy_filter(
    rows: Iterable[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    output: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    for row in rows:
        if row["metadata"].get("sensitive_only"):
            decisions.append(
                _decision(
                    row, "privacy_policy", "rejected", "Sensitive fragment had no task context."
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
        enriched = {**row, "quality_score": score}
        if score < 0.75:
            decisions.append(
                _decision(
                    row, "quality_filter", "rejected", f"Quality score {score:.2f} is below 0.75."
                )
            )
            continue
        output.append(enriched)
    return output, decisions


def _extract_signals(
    rows: Iterable[dict[str, Any]],
) -> tuple[list[dict[str, Any]], list[dict[str, Any]]]:
    signals: list[dict[str, Any]] = []
    decisions: list[dict[str, Any]] = []
    for row in rows:
        matched = next((rule for rule in SIGNAL_RULES if rule[1].search(row["content"])), None)
        if not matched:
            decisions.append(
                _decision(row, "signal_extraction", "rejected", "No supported signal found.")
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
            "privacy": "manager-safe summary",
        }
        signals.append(signal)
        decisions.append(_decision(signal, "signal_extraction", decision, reason, confidence))
    return signals, decisions


def _decision(
    row: dict[str, Any],
    stage_id: str,
    decision: str,
    reason: str,
    confidence: float | None = None,
) -> dict[str, Any]:
    digest = hashlib.sha1(f"{RUN_ID}:{stage_id}:{row['event_id']}".encode()).hexdigest()[:14]
    return {
        "decision_id": f"dec_{digest}",
        "run_id": RUN_ID,
        "stage_id": stage_id,
        "event_id": row["event_id"],
        "decision": decision,
        "reason": reason,
        "confidence": confidence,
        "operator_version": "3",
        "created_at": datetime(2026, 8, 29, 9, 14, tzinfo=UTC).isoformat(),
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
    raw: list[RawEvent],
    normalized: list[dict[str, Any]],
    private: list[dict[str, Any]],
    quality: list[dict[str, Any]],
    signals: list[dict[str, Any]],
) -> WorkspaceResponse:
    accepted = [row for row in signals if row["decision"] == "accepted"]
    review = [row for row in signals if row["decision"] == "review"]
    counts = Counter(event.source for event in raw)
    sources = [
        SourceSummary(
            id=source,
            label=SOURCE_LABELS.get(source, source.replace("_", " ").replace("-", " ").title()),
            count=count,
        )
        for source, count in sorted(counts.items())
    ]
    stages = [
        PipelineStage(
            id="raw",
            label="Raw",
            count=len(raw),
            input_count=len(raw),
            description="Immutable connector payloads",
            operator="source_union_v1",
        ),
        PipelineStage(
            id="normalize",
            label="Normalize",
            count=len(normalized),
            input_count=len(raw),
            description="Canonical fields and deduplication",
            operator="normalize_event_v2",
        ),
        PipelineStage(
            id="privacy",
            label="Privacy",
            count=len(private),
            input_count=len(normalized),
            description="Local redaction and policy quarantine",
            operator="local_privacy_v2",
        ),
        PipelineStage(
            id="quality",
            label="Quality",
            count=len(quality),
            input_count=len(private),
            description="Completeness and timestamp checks",
            operator="quality_gate_v2",
        ),
        PipelineStage(
            id="signals",
            label="Extract",
            count=len(signals),
            input_count=len(quality),
            description="Reviewable workflow evidence",
            operator="correction_signal_v3",
        ),
        PipelineStage(
            id="review",
            label="Review",
            count=len(review),
            input_count=len(signals),
            description="Uncertain records only",
            operator="review_queue_v1",
            status="review",
        ),
        PipelineStage(
            id="curated",
            label="Ready",
            count=len(accepted),
            input_count=len(signals),
            description="Manager-safe, model-ready output",
            operator="publish_snapshot_v1",
        ),
    ]
    source_priority = (
        "codex",
        "github",
        "jira",
        "slack",
        "outlook",
        "calendar",
        "claude_code",
        "confluence",
        "powerpoint",
    )
    available_sources = set(counts)
    preferred_sources = tuple(source for source in source_priority if source in available_sources)
    preferred_sources += tuple(sorted(available_sources.difference(source_priority)))
    grouped = {
        source: [row for row in signals if row["source"] == source] for source in preferred_sources
    }
    preferred_signal = {
        "codex": "correction",
        "claude_code": "agent_steer",
        "github": "verification_gap",
        "jira": "dependency_blocker",
        "slack": "knowledge_need",
        "outlook": "delivery_risk",
        "calendar": "coordination_overhead",
        "confluence": "context_repetition",
        "powerpoint": "delivery_risk",
    }
    samples: list[dict[str, Any]] = []
    for source in preferred_sources:
        rows = grouped[source]
        prefer_review = source in {"jira", "slack", "calendar"}
        desired_signal = preferred_signal.get(source)
        rows.sort(
            key=lambda row: (
                bool(desired_signal) and row["signal_type"] != desired_signal,
                row["decision"] != ("review" if prefer_review else "accepted"),
                -row["confidence"],
            )
        )
        if rows:
            samples.append(rows[0])
    for offset in range(1, 4):
        for source in preferred_sources:
            if len(grouped[source]) > offset:
                samples.append(grouped[source][offset])
    records = [
        EvidenceRecord(
            id=row["event_id"],
            source=row["source"],
            source_label=row["source_label"],
            raw_event=_short(row["content"]),
            extracted_signal=row["extracted_signal"],
            signal_type=row["signal_type"],
            confidence=row["confidence"],
            decision=row["decision"],
            reason=row["reason"],
            occurred_at=row["occurred_at"],
            privacy=row["privacy"],
            evidence_count=row["evidence_count"],
        )
        for row in samples[:36]
    ]
    distribution = Counter(row["signal_type"] for row in signals)
    return WorkspaceResponse(
        project={
            "id": "developer-workflow-signals",
            "name": "Developer workflow signals",
            "description": "Turn noisy work events into reviewable signals.",
        },
        generated_at=datetime(2026, 8, 29, 9, 14, tzinfo=UTC).isoformat(),
        run_id=RUN_ID,
        sources=sources,
        stages=stages,
        records=records,
        decision_breakdown={"accepted": len(accepted), "review": len(review), "rejected": 0},
        step_settings=StepSettings(
            operator="correction_signal_v3",
            version="3",
            policy="local-first redaction",
            threshold=0.78,
            code_version="demo-a7c9f2b",
            input_snapshot="quality_2026-08-29_0914",
            output_snapshot="signals_2026-08-29_0914",
            run_id=RUN_ID,
        ),
        signal_distribution=dict(distribution),
        schema_before=[
            {"field": "content", "type": "string", "policy": "local only"},
            {"field": "metadata_json", "type": "json", "policy": "source native"},
            {"field": "actor", "type": "string", "policy": "pseudonymized"},
        ],
        schema_after=[
            {"field": "extracted_signal", "type": "string", "policy": "manager safe"},
            {"field": "signal_type", "type": "enum", "policy": "shareable"},
            {"field": "confidence", "type": "float", "policy": "shareable"},
            {"field": "reason", "type": "string", "policy": "shareable"},
        ],
        copy_policy={
            "raw": "Raw prompts, responses, files, and messages stay local.",
            "cloud": (
                "Only pseudonymized conclusions, counts, and evidence strength are publishable."
            ),
        },
    )


def run_pipeline(
    raw_dir: Path, artifact_dir: Path, web_fixture: Path | None = None
) -> WorkspaceResponse:
    artifact_dir.mkdir(parents=True, exist_ok=True)
    raw = _load_events(raw_dir)
    normalized = _normalize(raw)
    private, privacy_decisions = _privacy_filter(normalized)
    quality, quality_decisions = _quality_filter(private)
    signals, signal_decisions = _extract_signals(quality)
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

    workspace = _workspace(raw, normalized, private, quality, signals)
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

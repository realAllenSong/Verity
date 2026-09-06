from __future__ import annotations

import json
import random
from datetime import UTC, datetime, timedelta
from pathlib import Path
from typing import Any

SOURCES = (
    "codex",
    "claude_code",
    "github",
    "jira",
    "slack",
    "outlook",
    "calendar",
    "confluence",
    "powerpoint",
)

UNIQUE_EVENTS = 3_611
DUPLICATE_EVENTS = 231
POLICY_QUARANTINE = 335
LOW_QUALITY = 362
SIGNAL_EVENTS = 1_086
REVIEW_EVENTS = 42

PEOPLE = (
    "Mina Okafor",
    "Ravi Narayanan",
    "Elena Petrova",
    "Theo Martin",
    "Naomi Brooks",
    "Lucas Ferreira",
)

ORDINARY_MESSAGES = (
    "Updated implementation notes after the design review.",
    "Synced the branch and resolved a small merge conflict.",
    "Shared the weekly project status with the working group.",
    "Opened the repository and inspected the existing test layout.",
    "Added a source link to the delivery notes.",
    "Reviewed the ticket history before starting the next task.",
)

SIGNAL_PATTERNS = (
    (
        "correction",
        (
            "User stopped a running refactor after type errors appeared and said to preserve "
            "the public API contract."
        ),
        "Correction: preserve the API contract",
    ),
    (
        "verification_gap",
        (
            "PR review requested missing regression tests for null tenant IDs and error handling "
            "before merge."
        ),
        "Verification gap: missing regression tests",
    ),
    (
        "context_repetition",
        (
            "Re-entered the Jira requirements and acceptance criteria in another coding session "
            "for the same task."
        ),
        "Repeated context setup",
    ),
    (
        "dependency_blocker",
        (
            "Jira issue moved to Blocked because an upstream schema dependency changed without "
            "a migration path."
        ),
        "Dependency blocker: upstream schema change",
    ),
    (
        "knowledge_need",
        "Asked in Slack which approved internal API client should handle retries and tenant "
        "headers.",
        "Knowledge need: approved API guidance",
    ),
    (
        "delivery_risk",
        "Email thread flags that the dataset delivery slipped after an incompatible schema "
        "revision.",
        "Delivery risk: schema instability",
    ),
    (
        "coordination_overhead",
        "Calendar meeting moved to next week because the external dependency owner was "
        "unavailable.",
        "Coordination overhead: external dependency",
    ),
    (
        "agent_steer",
        (
            "Developer steered the coding agent away from a new abstraction and toward the "
            "repository adapter pattern."
        ),
        "Agent steer: reuse the repository pattern",
    ),
)

SOURCE_SIGNAL_COPY = {
    (
        "confluence",
        "context_repetition",
    ): (
        "Confluence notes show that engineers re-entered the Jira requirements in another "
        "coding session for the same task."
    ),
    (
        "powerpoint",
        "delivery_risk",
    ): (
        "A PowerPoint delivery review slide flags that the dataset delivery slipped after an "
        "incompatible schema revision."
    ),
}


def _source_metadata(source: str, i: int, signal_position: int | None) -> dict[str, Any]:
    ticket = f"DATA-{1_200 + (i % 67)}"
    base: dict[str, Any] = {
        "workspace": "synthetic-lab",
        "evidence_count": 1
        if signal_position is not None and signal_position < REVIEW_EVENTS
        else 3 + (i % 5),
        "synthetic": True,
    }
    if source in {"codex", "claude_code"}:
        interaction = ("interrupt", "steer", "queue", "complete")[i % 4]
        base.update(
            {
                "session_id": f"sess_{i // 3:05d}",
                "turn_id": i % 19,
                "message_type": "assistant_turn",
                "user_prompt": (
                    "Keep the existing API surface and verify the edge cases before editing."
                ),
                "assistant_response": (
                    "I inspected the adapters and will update the narrow implementation path."
                ),
                "intermediate_steps": [
                    "read repository instructions",
                    "search symbol references",
                    "run focused tests",
                ],
                "tool_calls": ["search", "read_file", "test"],
                "interaction": interaction,
                "token_usage": {"input": 820 + (i % 970), "output": 310 + (i % 530)},
            }
        )
    elif source == "github":
        base.update(
            {
                "repository": "verity/sample-service",
                "pr_number": 240 + (i % 81),
                "commit_sha": f"demo{i:036x}"[-40:],
                "review_state": ("changes_requested", "approved", "commented")[i % 3],
                "ci_state": ("passed", "failed", "running")[i % 3],
                "additions": 8 + (i % 241),
                "deletions": 2 + (i % 89),
                "review_agent_fix_status": ("fixed", "open", "not_applicable")[i % 3],
            }
        )
    elif source == "jira":
        base.update(
            {
                "issue_key": ticket,
                "issue_type": ("Story", "Task", "Bug")[i % 3],
                "status_from": ("To Do", "In Progress", "Review")[i % 3],
                "status_to": ("In Progress", "Review", "Done")[i % 3],
                "story_points": 1 + (i % 8),
                "labels": ["data-platform", "synthetic"],
            }
        )
    elif source == "slack":
        base.update(
            {
                "channel": ("#data-platform", "#ml-research", "direct-message")[i % 3],
                "message_role": ("author", "reply", "mention")[i % 3],
                "thread_ts": f"1790{i:06d}.000100",
                "mentions_actor": i % 3 == 2,
            }
        )
    elif source == "outlook":
        base.update(
            {
                "folder": ("inbox", "sent", "archive")[i % 3],
                "direction": ("incoming", "outgoing", "reply")[i % 3],
                "conversation_id": f"mail_{i // 4:05d}",
                "participants": ["mina.okafor@example.invalid", "ravi.n@example.invalid"],
            }
        )
    elif source == "calendar":
        base.update(
            {
                "event_type": ("focus", "project_sync", "review")[i % 3],
                "duration_minutes": (30, 45, 60)[i % 3],
                "attendee_count": 2 + (i % 7),
                "response": ("accepted", "tentative", "organizer")[i % 3],
            }
        )
    elif source == "confluence":
        base.update(
            {
                "space": "Data Platform",
                "page_id": f"wiki_{i % 113:04d}",
                "revision": 1 + (i % 12),
                "edit_type": ("create", "update", "comment")[i % 3],
            }
        )
    elif source == "powerpoint":
        base.update(
            {
                "document_id": f"deck_{i % 41:03d}",
                "slide_count": 6 + (i % 23),
                "edit_type": ("content", "speaker_notes", "review_comment")[i % 3],
                "coauthor_count": 1 + (i % 5),
            }
        )
    return base


def _build_event(i: int, rng: random.Random) -> dict[str, Any]:
    source = SOURCES[i % len(SOURCES)]
    occurred_at = datetime(2026, 8, 1, 9, tzinfo=UTC) + timedelta(minutes=i * 19)
    signal_position = i - POLICY_QUARANTINE - LOW_QUALITY

    if i < POLICY_QUARANTINE:
        title = "Credential fragment captured without surrounding task context"
        content = "od_demo_secret_SHOULD_BE_REDACTED"
        kind = "unscoped_content"
    elif i < POLICY_QUARANTINE + LOW_QUALITY:
        title = None
        content = "  " if i % 2 else None
        occurred = "not-a-timestamp" if i % 3 else None
        kind = "unknown"
    elif signal_position < SIGNAL_EVENTS:
        signal_type, content, _ = SIGNAL_PATTERNS[signal_position % len(SIGNAL_PATTERNS)]
        content = SOURCE_SIGNAL_COPY.get((source, signal_type), content)
        title = f"{source.replace('_', ' ').title()} event related to DATA-{1_200 + (i % 67)}"
        occurred = occurred_at.isoformat()
        kind = signal_type
    else:
        title = f"{source.replace('_', ' ').title()} activity"
        content = rng.choice(ORDINARY_MESSAGES)
        occurred = occurred_at.isoformat()
        kind = "activity"

    if i < POLICY_QUARANTINE:
        occurred = occurred_at.isoformat()

    metadata = _source_metadata(
        source, i, signal_position if 0 <= signal_position < SIGNAL_EVENTS else None
    )
    if i < POLICY_QUARANTINE:
        metadata["sensitive_only"] = True
    if i % 43 == 0 and i >= POLICY_QUARANTINE + LOW_QUALITY:
        content = f"{content} Contact owner: developer{i % 17}@example.invalid."
    if i % 71 == 0 and i >= POLICY_QUARANTINE + LOW_QUALITY:
        content = f"  {content}\n\nAutomated footer: sent from mobile  "

    return {
        "event_id": f"evt_{i:05d}",
        "source": source,
        "kind": kind,
        "occurred_at": occurred,
        "actor": PEOPLE[i % len(PEOPLE)] if i >= POLICY_QUARANTINE else "Local User",
        "thread_id": f"thread_{i // 4:05d}",
        "title": title,
        "content": content,
        "status": ("open", "closed", "in_progress")[i % 3],
        "metadata": metadata,
    }


def generate_noisy_fixtures(output_dir: Path) -> dict[str, int]:
    """Create deterministic noisy batches using the generic record-envelope contract."""
    output_dir.mkdir(parents=True, exist_ok=True)
    rng = random.Random(24_082_026)
    for existing in output_dir.glob("*.jsonl"):
        existing.unlink()

    unique = [_build_event(i, rng) for i in range(UNIQUE_EVENTS)]
    duplicates = []
    for i in range(DUPLICATE_EVENTS):
        duplicate = json.loads(json.dumps(unique[(i * 13) % UNIQUE_EVENTS]))
        duplicate["metadata"]["duplicate_ingest"] = True
        duplicate["metadata"]["ingested_at"] = (
            datetime(2026, 8, 29, tzinfo=UTC) + timedelta(seconds=i)
        ).isoformat()
        duplicates.append(duplicate)

    all_events = unique + duplicates
    rng.shuffle(all_events)
    batch_names = (
        "batch_2026_08_24_01",
        "batch_2026_08_25_01",
        "batch_2026_08_26_01",
        "batch_2026_08_27_01",
        "batch_2026_08_28_01",
        "batch_2026_08_29_01",
    )
    batches: dict[str, list[dict[str, Any]]] = {name: [] for name in batch_names}
    for index, event in enumerate(all_events):
        batch_id = batch_names[index % len(batch_names)]
        native_metadata = event.pop("metadata")
        origin = event.pop("source")
        envelope = {
            "record_id": event["event_id"],
            "dataset_id": "workflow-signals",
            "batch_id": batch_id,
            "ingested_at": (
                datetime(2026, 8, 24, 9, tzinfo=UTC) + timedelta(days=index % 6, seconds=index)
            ).isoformat(),
            "payload": event,
            "metadata": {"origin": origin, **native_metadata},
        }
        batches[batch_id].append(envelope)

    for batch_id, records in batches.items():
        target = output_dir / f"{batch_id}.jsonl"
        with target.open("w", encoding="utf-8") as handle:
            for record in records:
                handle.write(json.dumps(record, ensure_ascii=False, sort_keys=True) + "\n")

    return {batch_id: len(records) for batch_id, records in batches.items()}

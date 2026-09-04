from __future__ import annotations

import json

import duckdb

from app.pipeline import run_pipeline
from app.sample_generator import generate_noisy_fixtures


def test_pipeline_produces_expected_white_box_funnel(tmp_path):
    raw_dir = tmp_path / "raw"
    artifacts = tmp_path / "artifacts"
    generate_noisy_fixtures(raw_dir)
    workspace = run_pipeline(raw_dir, artifacts)

    assert {stage.id: stage.count for stage in workspace.stages} == {
        "raw": 3_842,
        "normalize": 3_611,
        "privacy": 3_276,
        "quality": 2_914,
        "signals": 1_086,
        "review": 42,
        "curated": 1_044,
    }
    assert len(workspace.sources) == 9
    assert workspace.decision_breakdown.accepted == 1_044
    assert workspace.decision_breakdown.review == 42


def test_shareable_artifacts_remove_secret_and_email_patterns(tmp_path):
    raw_dir = tmp_path / "raw"
    artifacts = tmp_path / "artifacts"
    generate_noisy_fixtures(raw_dir)
    run_pipeline(raw_dir, artifacts)

    private_content = duckdb.sql(
        f"SELECT string_agg(content, ' ') FROM read_parquet('{artifacts / 'privacy.parquet'}')"
    ).fetchone()[0]
    curated_content = duckdb.sql(
        f"SELECT string_agg(content, ' ') FROM read_parquet('{artifacts / 'curated.parquet'}')"
    ).fetchone()[0]
    assert "od_demo_secret" not in private_content.lower()
    assert "@example.invalid" not in private_content
    assert "@example.invalid" not in curated_content
    assert "[email redacted]" in private_content


def test_agent_fixture_contains_turns_tools_and_steering(tmp_path):
    raw_dir = tmp_path / "raw"
    generate_noisy_fixtures(raw_dir)
    lines = (raw_dir / "codex.jsonl").read_text().splitlines()
    rows = [json.loads(line) for line in lines]

    assert any(row["metadata"].get("tool_calls") for row in rows)
    assert any(row["metadata"].get("intermediate_steps") for row in rows)
    assert {row["metadata"].get("interaction") for row in rows} >= {"interrupt", "steer", "queue"}


def test_pipeline_accepts_a_source_not_known_by_the_demo(tmp_path):
    raw_dir = tmp_path / "raw"
    raw_dir.mkdir()
    event = {
        "event_id": "evt_custom_001",
        "source": "lab_sensor.v2",
        "kind": "correction",
        "occurred_at": "2026-08-20T12:00:00+00:00",
        "actor": "Local Person",
        "thread_id": "trial_001",
        "title": "Unexpected measurement",
        "content": "User stopped a running refactor and asked to preserve the public API contract.",
        "status": "open",
        "metadata": {"evidence_count": 3},
    }
    (raw_dir / "lab_sensor.jsonl").write_text(json.dumps(event) + "\n")

    workspace = run_pipeline(raw_dir, tmp_path / "artifacts")

    assert workspace.sources[0].id == "lab_sensor.v2"
    assert workspace.sources[0].label == "Lab Sensor.V2"
    assert workspace.records[0].source == "lab_sensor.v2"

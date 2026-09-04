from __future__ import annotations

import json
from pathlib import Path
from threading import RLock
from typing import Any

import duckdb

from .models import ReviewUpdate, WorkspaceResponse
from .pipeline import RUN_ID, run_pipeline
from .sample_generator import generate_noisy_fixtures
from .settings import Settings


class WorkspaceStore:
    def __init__(self, settings: Settings) -> None:
        self.settings = settings
        self._lock = RLock()
        self._workspace: WorkspaceResponse | None = None

    @property
    def run_dir(self) -> Path:
        return self.settings.artifacts / RUN_ID

    def ensure(self) -> WorkspaceResponse:
        with self._lock:
            if self._workspace is not None:
                return self._workspace
            workspace_path = self.run_dir / "workspace.json"
            if workspace_path.exists():
                self._workspace = WorkspaceResponse.model_validate_json(workspace_path.read_text())
                return self._workspace
            return self.run()

    def run(self) -> WorkspaceResponse:
        with self._lock:
            generate_noisy_fixtures(self.settings.samples)
            self._workspace = run_pipeline(
                raw_dir=self.settings.samples,
                artifact_dir=self.run_dir,
                web_fixture=self.settings.web_fixture,
            )
            return self._workspace

    def review_queue(self) -> list[dict[str, Any]]:
        workspace = self.ensure()
        return [record.model_dump() for record in workspace.records if record.decision == "review"]

    def update_review(self, record_id: str, update: ReviewUpdate) -> WorkspaceResponse:
        with self._lock:
            workspace = self.ensure().model_copy(deep=True)
            record = next((item for item in workspace.records if item.id == record_id), None)
            if record is None:
                raise KeyError(record_id)
            previous = record.decision
            record.decision = update.decision
            if update.note:
                record.reason = update.note
            if previous == "review":
                workspace.decision_breakdown.review -= 1
            if update.decision == "accepted":
                workspace.decision_breakdown.accepted += 1
            elif update.decision == "rejected":
                workspace.decision_breakdown.rejected += 1

            override_path = self.run_dir / "review-overrides.jsonl"
            with override_path.open("a", encoding="utf-8") as handle:
                handle.write(
                    json.dumps(
                        {
                            "record_id": record_id,
                            "previous": previous,
                            "decision": update.decision,
                            "note": update.note,
                        },
                        sort_keys=True,
                    )
                    + "\n"
                )
            self._workspace = workspace
            return workspace

    def stage_preview(self, stage_id: str, limit: int = 20) -> list[dict[str, Any]]:
        allowed = {stage.id for stage in self.ensure().stages}
        if stage_id not in allowed:
            raise KeyError(stage_id)
        parquet = self.run_dir / f"{stage_id}.parquet"
        relation = duckdb.sql(
            f"SELECT * FROM read_parquet('{parquet}') LIMIT {max(1, min(limit, 100))}"
        )
        columns = [description[0] for description in relation.description]
        return [dict(zip(columns, row, strict=True)) for row in relation.fetchall()]

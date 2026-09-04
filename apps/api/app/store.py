from __future__ import annotations

import hashlib
import json
from datetime import UTC, datetime
from pathlib import Path
from threading import RLock
from typing import Any

import duckdb
from pydantic import ValidationError

from .models import BatchCreate, BatchStageResponse, BatchSummary, ReviewUpdate, WorkspaceResponse
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
                try:
                    self._workspace = WorkspaceResponse.model_validate_json(
                        workspace_path.read_text()
                    )
                    return self._workspace
                except ValidationError:
                    # Generated projections are disposable and rebuilt after contract upgrades.
                    pass
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

    def stage_batch(self, dataset_id: str, request: BatchCreate) -> BatchStageResponse:
        """Persist an input batch as generic envelopes without running a recipe implicitly."""
        with self._lock:
            workspace = self.ensure().model_copy(deep=True)
            if dataset_id != workspace.dataset.id:
                raise KeyError(dataset_id)
            now = datetime.now(UTC)
            digest = hashlib.sha1(f"{request.filename}:{now.isoformat()}".encode()).hexdigest()[:8]
            batch_id = f"batch_{now:%Y_%m_%d}_{digest}"
            fields = sorted({str(key) for record in request.records for key in record})
            upload_dir = self.settings.artifacts / "staged"
            upload_dir.mkdir(parents=True, exist_ok=True)
            target = upload_dir / f"{batch_id}.jsonl"
            with target.open("w", encoding="utf-8") as handle:
                for index, payload in enumerate(request.records):
                    envelope = {
                        "record_id": f"rec_{digest}_{index:06d}",
                        "dataset_id": dataset_id,
                        "batch_id": batch_id,
                        "ingested_at": now.isoformat(),
                        "payload": payload,
                        "metadata": {"filename": Path(request.filename).name, "state": "staged"},
                    }
                    handle.write(json.dumps(envelope, ensure_ascii=False, sort_keys=True) + "\n")

            batch = BatchSummary(
                id=batch_id,
                filename=Path(request.filename).name,
                added_at=now.isoformat(),
                record_count=len(request.records),
                field_count=len(fields),
                state="staged",
            )
            workspace.batches.insert(0, batch)
            workspace.dataset.batch_count += 1
            workspace.dataset.record_count += len(request.records)
            workspace.dataset.updated_at = now.isoformat()
            self._workspace = workspace
            return BatchStageResponse(
                batch=batch,
                sample_fields=fields[:12],
                message="Batch staged. Map its fields before the next run.",
            )

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

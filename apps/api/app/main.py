from __future__ import annotations

from contextlib import asynccontextmanager
from typing import Annotated

from fastapi import Depends, FastAPI, HTTPException, Query
from fastapi.middleware.cors import CORSMiddleware

from .models import ReviewUpdate, RunResponse, WorkspaceResponse
from .settings import get_settings
from .store import WorkspaceStore

store = WorkspaceStore(get_settings())


@asynccontextmanager
async def lifespan(_: FastAPI):
    store.ensure()
    yield


app = FastAPI(
    title="Verity API",
    summary="Language-neutral control-plane reference for white-box data curation.",
    version="0.1.0",
    lifespan=lifespan,
)
app.add_middleware(
    CORSMiddleware,
    allow_origins=["http://127.0.0.1:3000", "http://localhost:3000"],
    allow_credentials=True,
    allow_methods=["GET", "POST", "PATCH"],
    allow_headers=["*"],
)


def get_store() -> WorkspaceStore:
    return store


StoreDependency = Annotated[WorkspaceStore, Depends(get_store)]


@app.get("/health")
def health() -> dict[str, str]:
    return {"status": "ok"}


@app.get("/api/v1/workspace", response_model=WorkspaceResponse)
def workspace(current: StoreDependency) -> WorkspaceResponse:
    return current.ensure()


@app.post("/api/v1/runs", response_model=RunResponse)
def run_recipe(current: StoreDependency) -> RunResponse:
    result = current.run()
    return RunResponse(
        run_id=result.run_id,
        state="succeeded",
        stage_counts={stage.id: stage.count for stage in result.stages},
    )


@app.get("/api/v1/review-queue")
def review_queue(current: StoreDependency) -> dict[str, object]:
    records = current.review_queue()
    return {"count": len(records), "records": records}


@app.patch("/api/v1/reviews/{record_id}", response_model=WorkspaceResponse)
def update_review(
    record_id: str, update: ReviewUpdate, current: StoreDependency
) -> WorkspaceResponse:
    try:
        return current.update_review(record_id, update)
    except KeyError as error:
        raise HTTPException(status_code=404, detail="Review record not found") from error


@app.get("/api/v1/stages/{stage_id}/preview")
def stage_preview(
    stage_id: str,
    current: StoreDependency,
    limit: Annotated[int, Query(ge=1, le=100)] = 20,
) -> dict[str, object]:
    try:
        rows = current.stage_preview(stage_id, limit)
    except KeyError as error:
        raise HTTPException(status_code=404, detail="Pipeline stage not found") from error
    return {"stage_id": stage_id, "count": len(rows), "rows": rows}

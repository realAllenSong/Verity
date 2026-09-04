from fastapi.testclient import TestClient

from app.main import app


def test_workspace_and_stage_preview_are_available():
    with TestClient(app) as client:
        health = client.get("/health")
        workspace = client.get("/api/v1/workspace")
        preview = client.get("/api/v1/stages/signals/preview?limit=3")

    assert health.json() == {"status": "ok"}
    assert workspace.status_code == 200
    assert workspace.json()["stages"][0]["count"] == 3_842
    assert preview.status_code == 200
    assert preview.json()["count"] == 3

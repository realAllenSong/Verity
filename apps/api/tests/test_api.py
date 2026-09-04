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
    assert workspace.json()["dataset"]["batch_count"] == 6
    assert preview.status_code == 200
    assert preview.json()["count"] == 3


def test_generic_batch_can_be_staged_without_running_pipeline():
    with TestClient(app) as client:
        response = client.post(
            "/api/v1/datasets/workflow-signals/batches",
            json={
                "filename": "measurements.json",
                "records": [{"timestamp": "2026-09-04T12:00:00Z", "value": 42}],
            },
        )

    assert response.status_code == 201
    assert response.json()["batch"]["state"] == "staged"
    assert response.json()["sample_fields"] == ["timestamp", "value"]

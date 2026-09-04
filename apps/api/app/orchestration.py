"""Optional Dagster adapter.

The core recipe runner is deliberately orchestration-neutral. This module exposes
the same contract to Dagster when the optional dependency group is installed,
while local development and the future Go control plane use identical artifacts.
"""

from __future__ import annotations

from pathlib import Path

from .pipeline import RUN_ID, run_pipeline
from .sample_generator import generate_noisy_fixtures


def execute_demo_recipe(root: Path) -> str:
    raw = root / "sample_data" / "raw"
    generate_noisy_fixtures(raw)
    run_pipeline(
        raw,
        root / "artifacts" / RUN_ID,
        root / "apps" / "web" / "src" / "data" / "demo-workspace.json",
    )
    return RUN_ID


try:
    from dagster import Definitions, asset
except ImportError:  # Optional in the lightweight local runtime.
    definitions = None
else:

    @asset(group_name="verity_demo")
    def curated_developer_workflow_signals() -> str:
        return execute_demo_recipe(Path(__file__).resolve().parents[3])

    definitions = Definitions(assets=[curated_developer_workflow_signals])

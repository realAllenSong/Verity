from __future__ import annotations

import argparse
from pathlib import Path

from .pipeline import RUN_ID, run_pipeline
from .sample_generator import generate_noisy_fixtures


def main() -> None:
    parser = argparse.ArgumentParser(prog="verity")
    subparsers = parser.add_subparsers(dest="command", required=True)
    generate = subparsers.add_parser(
        "generate", help="Generate synthetic fixtures and run the demo recipe"
    )
    generate.add_argument("--root", type=Path, required=True)
    args = parser.parse_args()

    if args.command == "generate":
        root = args.root.resolve()
        raw_dir = root / "sample_data" / "raw"
        artifact_dir = root / "artifacts" / RUN_ID
        web_fixture = root / "apps" / "web" / "src" / "data" / "demo-workspace.json"
        source_counts = generate_noisy_fixtures(raw_dir)
        workspace = run_pipeline(raw_dir, artifact_dir, web_fixture)
        stage_counts = {stage.id: stage.count for stage in workspace.stages}
        print(f"sources={source_counts}")
        print(f"stages={stage_counts}")
        print(f"fixture={web_fixture}")


if __name__ == "__main__":
    main()

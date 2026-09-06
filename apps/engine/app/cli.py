from __future__ import annotations

import argparse
import json
from pathlib import Path

import duckdb

from .pipeline import RUN_ID, run_pipeline
from .sample_generator import generate_noisy_fixtures


def _generate(args: argparse.Namespace) -> None:
    root = args.root.resolve()
    raw_dir = root / "sample_data" / "raw"
    artifacts = args.artifact_root.resolve() if args.artifact_root else root / "artifacts"
    staged_dir = artifacts / "staged"
    artifact_dir = artifacts / args.run_id
    web_fixture = (
        root / "apps" / "web" / "src" / "data" / "demo-workspace.json"
        if args.write_web_fixture
        else None
    )
    batch_counts: dict[str, int] = {}
    if args.refresh_fixtures or not any(raw_dir.glob("*.jsonl")):
        batch_counts = generate_noisy_fixtures(raw_dir)
    workspace = run_pipeline(
        raw_dir,
        artifact_dir,
        web_fixture,
        run_id=args.run_id,
        additional_raw_dirs=(staged_dir,),
    )
    stage_counts = {stage.id: stage.count for stage in workspace.stages}
    print(json.dumps({"run_id": args.run_id, "batches": batch_counts, "stages": stage_counts}))


def _preview(args: argparse.Namespace) -> None:
    root = args.root.resolve()
    artifacts = args.artifact_root.resolve() if args.artifact_root else root / "artifacts"
    parquet = artifacts / args.run_id / f"{args.stage}.parquet"
    if not parquet.exists():
        raise SystemExit(f"stage artifact not found: {parquet}")
    relation = duckdb.sql(
        f"SELECT * FROM read_parquet('{parquet}') LIMIT {max(1, min(args.limit, 100))}"
    )
    columns = [description[0] for description in relation.description]
    rows = [dict(zip(columns, row, strict=True)) for row in relation.fetchall()]
    print(json.dumps(rows, default=str, ensure_ascii=False))


def main() -> None:
    parser = argparse.ArgumentParser(prog="verity-engine")
    subparsers = parser.add_subparsers(dest="command", required=True)

    generate = subparsers.add_parser("generate", help="Run the local data-plane recipe")
    generate.add_argument("--root", type=Path, required=True)
    generate.add_argument("--artifact-root", type=Path)
    generate.add_argument("--run-id", default=RUN_ID)
    generate.add_argument("--refresh-fixtures", action="store_true")
    generate.add_argument(
        "--write-web-fixture",
        action="store_true",
        help="Update the checked-in demo workspace; intended for fixture generation only.",
    )
    generate.set_defaults(handler=_generate)

    preview = subparsers.add_parser("preview", help="Read a bounded stage artifact preview")
    preview.add_argument("--root", type=Path, required=True)
    preview.add_argument("--artifact-root", type=Path)
    preview.add_argument("--run-id", required=True)
    preview.add_argument("--stage", required=True)
    preview.add_argument("--limit", type=int, default=20)
    preview.set_defaults(handler=_preview)

    args = parser.parse_args()
    args.handler(args)


if __name__ == "__main__":
    main()

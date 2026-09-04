# Verity

Verity is a white-box data preparation workbench. It turns noisy, heterogeneous events into reviewable, reproducible datasets for analytics, recommendation systems, traditional ML, transformer training, and future post-training workflows.

![Verity workbench](docs/screenshots/verity-complete-pipeline.png)

The repository is dataset-first. Input arrives as independent batches of generic records; an optional origin can live in metadata, but it never changes the platform contract or primary UI. Any file, service, model harness, database, document system, or internal application can emit the small `data-record` envelope and enter the same pipeline.

## What works now

- 3,842 deterministic, deliberately noisy demo records across six independently traceable batches
- versioned stages for normalization, privacy, quality, extraction, review, and publication
- Parquet stage snapshots, JSONL decision lineage, DuckDB inspection, and Polars transforms
- a human review loop for uncertain records
- complete Pipeline, Data, Recipes, Runs, Review, and Outputs workspaces
- local JSON, JSONL, and CSV inspection with explicit batch staging
- a clean Next.js workbench with progressive disclosure and responsive views
- a FastAPI control plane with checked-in JSON Schema and OpenAPI contracts
- an optional Dagster adapter without coupling the core recipe runner to one orchestrator

The demo funnel is reproducible: `3,842 raw -> 3,611 normalized -> 3,276 privacy-safe -> 2,914 quality-passed -> 1,086 extracted -> 42 review + 1,044 ready`.

## Run locally

Requirements: Node.js 22+, Python 3.11+, and [uv](https://docs.astral.sh/uv/).

```bash
make bootstrap
make generate
make api
```

In a second terminal:

```bash
make web
```

Open `http://127.0.0.1:3000`. The web app falls back to the checked-in synthetic snapshot when the API is not running.

Run unit, contract, lint, and production-build verification with:

```bash
make verify
```

Run the browser-level product flows with:

```bash
npm run test:e2e --workspace @verity/web
```

Or start both services with Docker:

```bash
docker compose up --build
```

## Repository map

```text
apps/web                 Next.js workbench
apps/api                 FastAPI reference control plane and local recipe runner
packages/contracts       Language-neutral event, operator, decision, and API contracts
sample_data/raw          Generated local JSONL inputs (ignored by Git)
artifacts                Generated Parquet and decision artifacts (ignored by Git)
docs                     Architecture, privacy, reuse decisions, and visual QA
```

The FastAPI service is a replaceable reference implementation. A future Go control plane can implement `packages/contracts/openapi.json` and reuse the same JSON schemas and artifact conventions.

## Design principles

- White-box by default: every stage exposes input, output, retention, operator version, and decision reason.
- Progressive disclosure: each workspace shows only its primary decision; schema, lineage, evidence, and configuration open only when requested.
- Local-first privacy: raw prompts, files, and message bodies are treated as local-only. Shareable layers contain redacted events, derived signals, provenance, and policy decisions.
- Human judgment at uncertainty: low-confidence records branch to review instead of silently entering training data.
- Portable contracts: batch producers and operators depend on schemas, not on Python or a specific orchestrator.

See [architecture](docs/architecture.md), [privacy boundary](docs/privacy.md), and [open-source reuse](docs/oss-reuse.md) for the implementation rationale.

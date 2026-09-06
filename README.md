# Verity

Verity is a white-box data preparation workbench. It turns noisy, heterogeneous events into reviewable, reproducible datasets for analytics, recommendation systems, traditional ML, transformer training, and future post-training workflows.

![Verity pipeline workbench](docs/screenshots/verity-go-pipeline.png)

The repository is dataset-first. Inputs arrive as independent batches of generic records. Origin can be carried in optional metadata, but the pipeline and UI do not depend on a fixed list of sources.

## What works now

- 3,842 deterministic, deliberately noisy demo records across six traceable batches
- versioned normalization, privacy, quality, extraction, review, and publication stages
- Parquet snapshots, JSONL decision lineage, DuckDB inspection, and Polars transforms
- a persistent human review loop for uncertain decisions
- complete Pipeline, Data, Recipes, Runs, Review, and Outputs workspaces
- local JSON, JSONL, and CSV batch staging with checksums and idempotent retries
- a Go control plane with strict requests, atomic local state, ETags, readiness, optional bearer auth, structured errors, request IDs, access logs, and graceful shutdown
- a replaceable Python data-plane worker behind a process contract
- checked-in JSON Schema and OpenAPI contracts
- optional Dagster orchestration without coupling the core engine to one scheduler

The demo funnel is reproducible: `3,842 raw -> 3,611 normalized -> 3,276 privacy-safe -> 2,914 quality-passed -> 1,086 extracted -> 42 review + 1,044 ready`.

This is a robust local reference product, not an enterprise GA claim. Production connectors, SSO/RBAC, managed secrets, tenant isolation, distributed durable execution, retention enforcement, and scale/security testing remain explicit next phases. See [production readiness](docs/production-readiness.md).

## Run locally

Requirements: Node.js 22+, Go 1.24+, Python 3.11+, and [uv](https://docs.astral.sh/uv/).

```bash
make bootstrap
make generate
make api
```

In a second terminal:

```bash
make web
```

Open `http://127.0.0.1:3000`. The web app uses the checked-in synthetic snapshot when the API is unavailable.

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
apps/api                 Go control plane and HTTP API
apps/engine              Python/Polars/DuckDB data-plane worker
packages/contracts       Language-neutral event, operator, decision, and API contracts
sample_data/raw          Generated local JSONL inputs (ignored by Git)
artifacts                Generated state, Parquet, and decision artifacts (ignored by Git)
docs                     Architecture, privacy, reuse, readiness, and visual QA
```

The Go service owns API behavior, state, idempotency, lifecycle, and observability. The Python worker owns columnar transformation and artifact generation. Their boundary is intentionally language-neutral so the worker can later be replaced or invoked by Temporal, Dagster, or the firm's Go agent framework without redesigning the product.

## Design principles

- White-box by default: each stage exposes input, output, retention, operator version, checks, and decision reason.
- Progressive disclosure: the first view answers what happened; schema, lineage, evidence, and configuration open on demand.
- Local-first privacy: raw prompts, files, and message bodies remain local-only. Shareable layers contain approved derived signals and audit metadata.
- Human judgment at uncertainty: low-confidence records branch to review instead of silently entering model data.
- Portable contracts: producers and operators depend on schemas, not a source logo, language, or scheduler.

See [architecture](docs/architecture.md), [privacy boundary](docs/privacy.md), [open-source reuse](docs/oss-reuse.md), and [production readiness](docs/production-readiness.md).

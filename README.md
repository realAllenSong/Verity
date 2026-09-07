# Verity

Verity is a white-box data preparation workbench. It turns noisy, heterogeneous events into reviewable, reproducible datasets for analytics, recommendation systems, traditional ML, transformer training, and future post-training workflows.

![Verity pipeline workbench](docs/screenshots/verity-go-pipeline.png)

The repository is dataset-first. Inputs arrive as independent batches of generic records. Origin can be carried in optional metadata, but the pipeline and UI do not depend on a fixed list of sources.

## What works now

- 3,842 deterministic, deliberately noisy demo records across six traceable batches
- versioned normalization, privacy, quality, extraction, review, and publication stages
- atomic JSONL stage snapshots, CSV and Parquet exports, and JSONL decision lineage
- a persistent human review loop for uncertain decisions
- complete Pipeline, Data, Recipes, Runs, Review, and Outputs workspaces
- per-stage artifact comparisons showing representative input, output, changed fields, and filtering reasons
- local JSON, JSONL, and CSV batch staging with checksums and idempotent retries
- one Go service for the control plane and in-process data plane, with strict requests, atomic local state and artifacts, ETags, readiness, optional bearer auth, structured errors, request IDs, access logs, and graceful shutdown
- checked-in JSON Schema and OpenAPI contracts
- a native optional Temporal workflow/worker for durable execution
- an optional Airbyte HTTP adapter for triggering and tracking managed ingestion jobs

The demo funnel is reproducible: `3,842 raw -> 3,611 normalized -> 3,276 privacy-safe -> 2,914 quality-passed -> 1,086 extracted -> 42 review + 1,044 ready`.

This is a robust local reference product, not an enterprise GA claim. Production connectors, SSO/RBAC, managed secrets, tenant isolation, distributed durable execution, retention enforcement, and scale/security testing remain explicit next phases. See [production readiness](docs/production-readiness.md).

## Run locally

Requirements: Node.js 22+ and Go 1.24+.

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

To exercise the complete white-box flow:

1. Open **Data**, choose **Add data**, and upload `sample_data/examples/noisy-workflow-events.json`.
2. Confirm the raw preview, then choose **Stage batch** and **Done**.
3. Return to **Pipeline** and choose **Run pipeline**.
4. Open Raw, Normalize, Privacy, Quality, Extract, Review, and Ready in order. Each stage reads the current run artifact and shows representative records, transformations, and removal reasons.
5. Click any record to compare its fields before and after the selected boundary. Use **Filtered out** when available to isolate rejected examples.

The sample deliberately contains duplicate IDs, schema aliases, sensitive values, malformed records, unsupported content, low-confidence signals, and accepted signals. It is safe synthetic data and is intended for hands-on validation.

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

The default deployment remains two containers: the all-Go API/engine and the web app.
For durable local orchestration through Temporal, add the optional overlay:

```bash
docker compose -f compose.yaml -f compose.temporal.yaml up --build
```

Temporal is not required for normal local use. To connect an existing Airbyte Cloud or
self-managed instance, set `VERITY_AIRBYTE_BASE_URL` and `VERITY_AIRBYTE_TOKEN`; Verity
then exposes bounded endpoints to trigger a connection sync and read its job state. See
[integrations](docs/integrations.md).

## Repository map

```text
apps/web                 Next.js workbench
apps/api                 All-Go API, pipeline engine, fixtures, and optional adapters
packages/contracts       Language-neutral event, operator, decision, and API contracts
sample_data/raw          Generated local JSONL inputs (ignored by Git)
artifacts                Generated state, JSONL/CSV/Parquet, and decision artifacts (ignored by Git)
docs                     Architecture, privacy, reuse, readiness, and visual QA
```

The Go service owns API behavior, state, idempotency, lifecycle, transformations, artifact generation, and observability. Local runs execute in-process. The same engine can run as a Temporal activity through `verity-worker`, without changing the frontend or public API. Airbyte remains a producer-side adapter rather than becoming Verity's core runtime.

## Design principles

- White-box by default: each stage exposes input, output, retention, operator version, checks, and decision reason.
- Progressive disclosure: the first view answers what happened; schema, lineage, evidence, and configuration open on demand.
- Local-first privacy: raw prompts, files, and message bodies remain local-only. Shareable layers contain approved derived signals and audit metadata.
- Human judgment at uncertainty: low-confidence records branch to review instead of silently entering model data.
- Portable contracts: producers and operators depend on schemas, not a source logo, language, or scheduler.

See [architecture](docs/architecture.md), [integrations](docs/integrations.md), [privacy boundary](docs/privacy.md), [open-source reuse](docs/oss-reuse.md), and [production readiness](docs/production-readiness.md).

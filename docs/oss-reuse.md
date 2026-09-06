# Open-source reuse

Verity composes maintained libraries and adopts proven architectural patterns. It does not copy another platform's product surface or vendor-lock its core contracts.

## Used directly

| Component | Purpose | Boundary |
| --- | --- | --- |
| Go `net/http` | control-plane API, lifecycle, and middleware | HTTP layer |
| Polars | deterministic columnar transforms | operator implementation |
| DuckDB | bounded local artifact inspection | query adapter |
| PyArrow / Parquet | portable stage snapshots | artifact format |
| Pydantic | data-plane envelope validation | worker boundary |
| Radix Themes | accessible dialogs and menus | UI primitives, visually customized |
| Phosphor Icons | interaction symbols | visual assets |
| Dagster | optional orchestration adapter | not required by the core runner |

## Patterns absorbed

| Source | Pattern reflected in Verity | What Verity does differently |
| --- | --- | --- |
| dlt | incremental batches, persistent ingestion state, schema contracts | one minimal generic record envelope and a local-first UI |
| OpenLineage | explicit run lifecycle and job/run identity | compact local START/COMPLETE/FAIL events; a full transport adapter is deferred |
| Dagster asset checks / Great Expectations | named, visible quality checks attached to data stages | checks are part of the same workbench and evidence path |
| Label Studio | pre-annotation plus human correction and provenance | review is record-level progressive disclosure, not a general annotation suite |
| Temporal | durable-workflow boundary and retry-aware design | not embedded until distributed recovery is actually required |

## Deliberately deferred adapters

- dlt or Airbyte producers for managed ingestion
- Temporal or Dagster for distributed durable execution
- Great Expectations or Soda for larger declarative quality suites
- Data-Juicer and Cleanlab for specialized cleaning and label-quality operators
- Label Studio for complex annotation projects
- Hugging Face Datasets for publishing and transformer workflows

These are integration candidates, not claimed dependencies. Each should sit behind a producer, operator, review, orchestration, or exporter interface so the product keeps one white-box experience and decision lineage.

## Why not merge entire projects

Cloning and combining full platforms would create overlapping schedulers, metadata stores, permission systems, and UIs. Verity takes the useful contracts and operational ideas while keeping a small core. This makes the eventual migration to the firm's internal Go framework feasible and keeps optional systems replaceable.

## Deliberate non-goals

- hard-coding the platform to coding-agent telemetry
- storing raw employee content in a central service
- training an LLM inside the data-preparation control plane
- presenting one opaque quality or employee-performance score without dimensions and evidence
- claiming distributed or enterprise robustness before those systems are implemented and tested

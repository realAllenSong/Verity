# Open-source reuse

Verity composes maintained libraries and adopts proven architectural patterns. It does not
copy another platform's product surface or force every deployment to operate multiple
schedulers, metadata stores, and UIs.

## Used directly

| Component | Purpose | Boundary |
| --- | --- | --- |
| Go `net/http` | API, lifecycle, middleware, and Airbyte adapter | service boundary |
| parquet-go | pure-Go typed Parquet publication | curated analytics/ML artifact only |
| Temporal Go SDK | optional durable workflow and worker | orchestration adapter |
| Radix Themes | accessible dialogs and menus | UI primitives, visually customized |
| Phosphor Icons | interaction symbols | visual assets |

The default pipeline uses the Go standard library for streaming JSONL, CSV, redaction,
hashing, validation, and atomic filesystem publication. `parquet-go` adds columnar output
without introducing Python or CGO.

## Patterns absorbed

| Source | Pattern reflected in Verity | What Verity does differently |
| --- | --- | --- |
| dlt | incremental batches, persistent ingestion state, schema contracts | one minimal generic record envelope and a local-first UI |
| OpenLineage | explicit run lifecycle and job/run identity | compact local START/COMPLETE/FAIL events; transport remains optional |
| Dagster asset checks / Great Expectations | named checks attached to data stages | checks are part of the same workbench and evidence path |
| Label Studio | pre-annotation plus human correction and provenance | record-level progressive review, not a general annotation suite |
| Temporal | durable workflows, retries, worker recovery | native optional backend running the exact same Go pipeline |
| Airbyte | managed source connectors and incremental sync | API adapter only; Airbyte stays outside the default runtime |

## Deliberately optional or deferred

- Airbyte Cloud or self-managed for sources that justify its operational footprint
- Temporal Cloud or self-hosted Temporal for durable distributed execution
- Arrow streaming for high-volume or zero-copy consumers
- Great Expectations or Soda for larger declarative quality suites
- Data-Juicer and Cleanlab for specialized cleaning and label-quality operators
- Label Studio for complex annotation projects
- Hugging Face Datasets for publishing and transformer workflows

Dagster is not embedded. Its official OSS production model requires Python code locations
and long-running webserver and daemon services. Reintroducing that stack would contradict
the all-Go core and duplicate Temporal's role.

DuckDB is also not a default dependency. Its official Go client is strong, but requires
CGO for builds and cross-compilation. It remains a valid downstream consumer of Verity's
Parquet, JSONL, or CSV outputs, or a future opt-in analytics adapter.

Primary references: [Temporal self-hosting](https://docs.temporal.io/self-hosted-guide),
[Temporal Go SDK](https://pkg.go.dev/go.temporal.io/sdk/workflow),
[Airbyte job API](https://reference.airbyte.com/reference/createjob),
[Airbyte OSS quickstart](https://docs.airbyte.com/platform/using-airbyte/getting-started/oss-quickstart),
[Dagster deployment architecture](https://docs.dagster.io/deployment/oss/deployment-architecture),
[parquet-go](https://github.com/parquet-go/parquet-go), and
[DuckDB Go client](https://duckdb.org/docs/current/clients/go).

## Deliberate non-goals

- hard-coding the platform to coding-agent telemetry
- storing raw employee content in a central service
- training an LLM inside the data-preparation control plane
- presenting one opaque quality or employee-performance score without dimensions and evidence
- claiming multi-tenant enterprise robustness before security and scale criteria are passed

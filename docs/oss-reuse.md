# Open-source reuse

This implementation composes maintained libraries rather than copying source from other data platforms.

## Used now

| Component | Purpose | Boundary |
| --- | --- | --- |
| Polars | deterministic columnar transforms | operator implementation only |
| DuckDB | local artifact inspection and preview | query adapter only |
| PyArrow / Parquet | portable stage snapshots | artifact format |
| FastAPI + Pydantic | reference control plane and contracts | replaceable by OpenAPI-compatible Go service |
| Radix Themes | accessible dialogs and menus | UI primitives, customized visually |
| Phosphor Icons | interaction and selected brand glyphs | visual assets |
| Simple Icons | Anthropic, Jira, and Confluence brand marks | visual assets |
| Dagster | optional orchestration adapter | not required by the core runner |

## Good future adapters

- dlt or Airbyte for production ingestion connectors
- Data-Juicer for large-scale dataset cleaning operators
- Cleanlab for label-quality and issue detection
- Hugging Face Datasets for dataset publishing and transformer workflows
- Great Expectations or Soda for declarative data-quality checks
- Label Studio for richer annotation projects

These are integration candidates, not claimed dependencies. The platform should adopt each behind a connector, operator, review, or exporter interface so users keep one white-box experience and one decision lineage.

## Deliberate non-goals

- rebuilding a general scheduler before production orchestration requirements are known
- hard-coding the platform to coding-agent telemetry
- storing raw employee content in a central service
- training an LLM inside the data-preparation control plane
- presenting one opaque "quality score" without underlying dimensions and evidence

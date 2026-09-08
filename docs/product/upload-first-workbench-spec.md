# Upload-first automated workbench product spec

**Status:** Approved direction; core flow implemented, remaining criteria tracked below

**Date:** 2026-09-06

## Implementation checkpoint (2026-09-08)

The upload → automatic processing → inspect → review → download loop is implemented and tested. This document also contains target behavior that is not yet implemented; it is not a completion certificate.

- Delivered: streaming formats, offset-based resumable uploads, automatic jobs, committed stage-event replay, bounded comparison samples, full-stage pagination, immutable review-output revisions, REST/CLI/MCP, local 100k/1m tests, and a generic Kubernetes base.
- Deliberate implementation choices: a documented custom HEAD/PATCH upload protocol instead of tusd; 25-row pagination instead of a virtualization dependency. It is not tus-compatible. bbolt, parquet-go, the official MCP SDK, and Radix are reused directly.
- Still open: browser pause/cancel controls and job recovery after API restart; pause/resume workflow gates for schema/format ambiguity; progress restoration after browser reload; live inspection of a not-yet-completed run; paste/source-connection entry points; editable/custom recipe UI; full-dataset search; enterprise authorization and Curvatus runtime validation.
- Human review currently branches: already accepted records become downloadable immediately; uncertain records remain excluded until reviewed. Decisions create a new immutable result revision. Automated stage counts remain the original run snapshot.
- General envelope/format support does not make the bundled domain recipe universal. The current recipe extracts workflow signals. Other data domains need their own Go operators; no model training or external model calls happen automatically.

See [verification](../verification.md) for executed tests and [production readiness](../production-readiness.md) for deployment boundaries.

## Product promise

Verity accepts a data file or automated data feed, starts a safe default preparation
workflow immediately, and shows how the data changes at every step. A person intervenes
only when the format, schema, policy, or result is genuinely ambiguous. The final result
can be downloaded or consumed through an API, CLI, or MCP server.

The primary mental model is not “manage datasets, recipes, and runs.” It is:

> Give Verity data → watch it become usable → resolve exceptions → take the result.

## Primary user flow

### First visit

1. The first screen is a large, functional drop zone with one primary action: **Drop data or choose files**.
2. The same screen offers three quiet alternatives: paste data, connect an automated source, or copy an API/CLI command.
3. Dropping a file creates an import and starts the default workflow without a separate staging or run action.
4. The screen transitions into the live pipeline view. The uploaded file remains the first node.
5. Each completed stage becomes inspectable. The current stage shows truthful activity and aggregate progress.
6. If a stage needs input, the pipeline pauses at a clearly marked review node.
7. When no input is needed, the workflow reaches **Ready** and exposes download and integration actions.

### Returning visit

1. Open directly to the latest workspace and its latest pipeline state.
2. Show **Add data** as the primary action when the current run is complete.
3. Keep run history, recipe configuration, schema policy, and integration setup behind drawers or an overflow menu.
4. A contextual **Retry** or **Re-run with changes** action may appear after failure or configuration changes. There is no global **Run pipeline** button.

## Information architecture

The persistent left navigation is removed. The product uses one main workspace.

The top bar contains:

- Verity identity
- current workspace or dataset switcher
- current status
- **Add data**
- one overflow menu for history, workflow settings, integrations, API/CLI/MCP access, and workspace settings

The workspace contains:

- input node
- ordered transformation stages
- optional human-review branch
- ready output node
- a progressive inspection surface for the selected stage

## Inspection experience

Selecting a stage shows four levels of information without navigating away:

1. Summary: input, output, changed, removed, warnings, duration.
2. Readable rows: a virtualized sample/table with search and filters.
3. Transformation explanation: before/after values, highlighted changes, reason, operator version, and lineage.
4. Raw source: available only through explicit disclosure and subject to privacy policy.

Transitions use restrained motion:

- removed rows strike through and fade from the sample
- changed fields highlight and settle into their new value
- retained rows move forward
- aggregate counters animate to the committed result

Animation represents committed stage events. It must not invent per-record progress.

## Input formats

Core supported inputs:

- CSV with quoted fields, embedded delimiters, escaped quotes, UTF-8 BOM, and multiline values
- TSV
- JSON array, one JSON object, or a portable Verity envelope
- JSONL and NDJSON
- Parquet
- gzip-compressed CSV, TSV, JSONL, NDJSON, and JSON

Excel `.xlsx` is a convenience adapter after the core formats pass parity and load tests.
Unsupported or ambiguous files are not silently guessed. Verity shows the detected type,
the reason it cannot proceed, and the smallest corrective action.

## Scale behavior

Files are never parsed in full in browser memory. The browser performs a resumable upload;
the Go service streams the source into a canonical batch and returns bounded previews.

Test tiers:

- 14 records: hand-auditable edge-case fixture
- 10,000 records: format parity and ordinary browser E2E
- 100,000 records: standard local load gate
- 1,000,000 records: nightly stress and bounded-memory gate
- 10,000,000 records: explicit breakpoint test, not a normal CI requirement

Large tables never render every DOM row. The UI uses server pagination and row virtualization.

## Automation surfaces

The web app, CLI, and MCP server are clients of the same versioned REST API.

### REST lifecycle

- create an import
- upload or resume bytes
- finalize the import
- automatically create and start a run
- stream job events
- inspect stage summaries, paginated rows, and before/after comparisons
- resolve review items
- download an output or manifest

### CLI lifecycle

```bash
verity import ./events.csv --wait --output ./ready.parquet
```

Additional commands expose status, stage inspection, review, and export without creating
a second data-processing implementation.

### MCP lifecycle

The MCP server exposes small control-plane tools. Large file bytes are not embedded in MCP
tool arguments. Tools accept a local file path for a local stdio server or an existing import ID.

## Human-in-the-loop rules

The workflow pauses only for:

- ambiguous format or encoding
- unresolved schema mapping required by the selected workflow
- privacy/policy decisions that cannot be made deterministically
- low-confidence extraction or classification
- explicit workflow policy requiring approval

Review decisions are append-only overrides with actor, timestamp, reason, prior result,
and resulting output lineage.

## Privacy and trust

- Raw source remains local by default.
- Browser previews are bounded and never become a second durable copy.
- Stage explanations show exactly what changed and why.
- Published outputs exclude raw private content unless an approved workflow explicitly allows it.
- API, CLI, MCP, and web actions share the same authorization and audit policy.

## Acceptance criteria

1. A first-time user can drop a supported file from the first screen and reach a ready or review state without visiting another page.
2. Drag-and-drop, file picker, keyboard activation, multi-file selection, cancellation, retry, and upload resume work.
3. Equivalent logical records in CSV, TSV, JSON, JSONL, NDJSON, and Parquet produce equivalent canonical fingerprints and stage counts.
4. The 100,000-record test completes with bounded memory and the UI stays responsive.
5. The 1,000,000-record stress test completes without data loss, duplicate publication, or unbounded memory growth.
6. Every stage exposes a readable summary, bounded row view, before/after explanation, and decision lineage.
7. The REST API, Go CLI, MCP tools, and web app can each complete the same import-to-output flow.
8. No primary navigation item is required to complete the first-run workflow.

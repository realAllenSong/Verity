# Upload-first Automated Data Workbench Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (- [ ]) syntax for tracking.

**Goal:** Replace Verity's navigation-heavy manual workflow with a single upload-first, auto-running, inspectable data-preparation workspace that also works through REST, CLI, and MCP at 1,000,000-record scale.

**Architecture:** The Go service owns streaming ingestion, canonicalization, asynchronous run state, review state, and artifact publication. Next.js is an API client that displays one workspace state machine instead of separate Data, Pipeline, Runs, and Outputs pages. Resumable uploads, virtualized tables, the official MCP Go SDK, and repeatable load tooling are reused rather than reimplemented.

**Tech Stack:** Go 1.24, Next.js 16/React 19, tusd v2, tus-js-client, parquet-go, bbolt, TanStack Virtual, official MCP Go SDK, Playwright, Vitest, Go test/benchmarks, Grafana k6.

**Spec:** docs/product/upload-first-workbench-spec.md

## Global Constraints

- The first screen's primary action is **Drop data or choose files**.
- A successful import starts the default workflow automatically; there is no primary **Run pipeline** action.
- Raw data remains local by default and published projections exclude raw private content.
- Core inputs are CSV, TSV, JSON, JSONL, NDJSON, Parquet, and the specified gzip variants.
- The web app, CLI, and MCP server use the same REST contract and Go data plane.
- Large inputs are streamed; browser and API code may not materialize the entire file in memory.
- Human review pauses only at ambiguous or policy-controlled decisions.
- Stage UI must remain readable through progressive disclosure and bounded row rendering.
- Existing review overrides and immutable run snapshots remain durable and traceable.

---

### Task 1: Freeze the lifecycle contract and deterministic fixture matrix

**Files:**
- Modify: packages/contracts/openapi.json
- Create: packages/contracts/schemas/import.schema.json
- Create: packages/contracts/schemas/job-event.schema.json
- Modify: apps/api/cmd/verity-generate/main.go
- Create: apps/api/internal/verity/fixture_formats.go
- Create: apps/api/internal/verity/fixture_formats_test.go
- Create: sample_data/examples/noisy-workflow-events.csv
- Create: sample_data/examples/noisy-workflow-events.jsonl
- Create: sample_data/examples/noisy-workflow-events.parquet
- Create: sample_data/manifests/format-parity.json
- Modify: Makefile

**Interfaces:**
- Produces: Import, Job, JobEvent, StagePage, and OutputDownload OpenAPI schemas.
- Produces: GenerateFixture(FixtureConfig) (FixtureManifest, error).
- Produces: make fixtures-small, make fixtures-load, and make fixtures-stress; only small fixtures are committed.

- [ ] **Step 1: Write failing contract tests**

Require these operations:

~~~text
POST   /api/v1/imports
HEAD   /api/v1/uploads/{upload_id}
PATCH  /api/v1/uploads/{upload_id}
POST   /api/v1/imports/{import_id}/complete
GET    /api/v1/jobs/{job_id}
GET    /api/v1/jobs/{job_id}/events
GET    /api/v1/stages/{stage_id}/records
GET    /api/v1/outputs/{output_id}
~~~

- [ ] **Step 2: Prove the contract test fails**

~~~bash
cd apps/api
go test ./internal/verity -run TestOpenAPIContainsEveryPublicOperation -count=1
~~~

Expected: missing import, upload, job-event, stage-record, and output operations.

- [ ] **Step 3: Add exact lifecycle schemas**

Use states created, uploading, profiling, queued, running, needs_input, succeeded, failed, and canceled. Every mutating request requires Idempotency-Key. Every async creation response returns HTTP 202, job_id, and status_url.

- [ ] **Step 4: Extend the fixture generator**

Support this command:

~~~bash
go run ./cmd/verity-generate --root ../.. --records 10000 --format csv --seed 20260906 --noise-profile mixed --output /tmp/verity-fixtures
~~~

The format flag accepts csv, tsv, json, jsonl, ndjson, and parquet. The records flag accepts 1 through 10,000,000.

- [ ] **Step 5: Prove cross-format parity**

Generate 10,000 equivalent records in every core format and assert identical canonical fingerprints, stage counts, and decision counts.

- [ ] **Step 6: Run the focused suite and commit**

~~~bash
cd apps/api
go test ./internal/verity -run 'Test(OpenAPI|Fixture|FormatParity)' -count=1
git add ../../packages/contracts cmd/verity-generate internal/verity/fixture_formats* ../../sample_data/manifests ../../Makefile
git commit -m "Define automated import lifecycle and fixture matrix"
~~~

### Task 2: Add streaming, resumable, multi-format ingestion

**Files:**
- Create: apps/api/internal/verity/import_models.go
- Create: apps/api/internal/verity/import_service.go
- Create: apps/api/internal/verity/import_store.go
- Create: apps/api/internal/verity/import_csv.go
- Create: apps/api/internal/verity/import_json.go
- Create: apps/api/internal/verity/import_parquet.go
- Create: apps/api/internal/verity/import_compression.go
- Create: apps/api/internal/verity/import_test.go
- Modify: apps/api/internal/verity/http.go
- Modify: apps/api/internal/verity/models.go
- Modify: apps/api/go.mod

**Interfaces:**
- Produces: RecordStream with Next(context.Context) (map[string]any, error) and Close() error.
- Produces: DetectFormat(filename string, header []byte) (InputFormat, error).
- Produces: OpenRecordStream(io.Reader, InputFormat) (RecordStream, error).
- Produces: ImportService.Create, ImportService.Complete, and tus completion callbacks.

- [ ] **Step 1: Write parser failures for real-world formats**

Cover quoted commas, escaped quotes, embedded newlines, BOM, CRLF, blank rows, duplicate headers, malformed JSONL line numbers, nested JSON, Parquet type preservation, gzip corruption, and a 16 MiB single record.

- [ ] **Step 2: Implement streaming readers**

Use encoding/csv with FieldsPerRecord = -1, json.Decoder for JSON arrays and objects, buffered iteration for JSONL/NDJSON, and parquet-go row groups for Parquet. Emit one portable envelope at a time.

- [ ] **Step 3: Mount tusd v2**

Store incomplete uploads under artifacts/uploads/incomplete and completed sources under artifacts/uploads/complete. A completion hook creates one import record but never parses the complete file in the HTTP request goroutine.

- [ ] **Step 4: Enforce safe limits**

Default maximum source size is 5 GiB via VERITY_MAX_UPLOAD_BYTES. Reject traversal, MIME/extension contradictions, archive bombs, unsupported compression, and input outside the configured upload store.

- [ ] **Step 5: Stream to canonical staged JSONL**

Maintain SHA-256 while reading, write a temporary artifact, fsync, rename atomically, then persist the completed import.

- [ ] **Step 6: Verify and commit**

~~~bash
cd apps/api
go test ./internal/verity -run 'TestImport|TestUpload|TestFormat' -count=1
go test -race ./internal/verity -run 'TestImport|TestUpload' -count=1
git add internal/verity go.mod go.sum
git commit -m "Add resumable streaming multi-format imports"
~~~

### Task 3: Make import-to-output an asynchronous automatic workflow

**Files:**
- Create: apps/api/internal/verity/job_manager.go
- Create: apps/api/internal/verity/job_events.go
- Create: apps/api/internal/verity/job_manager_test.go
- Modify: apps/api/internal/verity/store.go
- Modify: apps/api/internal/verity/runner.go
- Modify: apps/api/internal/verity/temporal.go
- Modify: apps/api/internal/verity/http.go
- Modify: apps/api/internal/verity/workspace_builder.go

**Interfaces:**
- Produces: StartImportRun(ctx context.Context, importID string) (jobID string, error).
- Produces: Subscribe(jobID string, after uint64) (<-chan JobEvent, func(), error).
- Produces: one JobRunner interface shared by local and Temporal execution.

- [ ] **Step 1: Test state transitions**

~~~text
uploading → profiling → queued → running → succeeded
uploading → profiling → needs_input → running → succeeded
running → needs_input → running → succeeded
running → failed → queued → running → succeeded
~~~

Also prove duplicate completion cannot create a second run and failed runs cannot replace the last successful output.

- [ ] **Step 2: Implement the durable local job manager**

Persist before publishing events, use monotonic sequence numbers, replay after reconnect, enforce one active workspace job, and honor cancellation.

- [ ] **Step 3: Auto-start after import completion**

POST /api/v1/imports/{id}/complete returns the created job and starts the default workflow. No second run request is required.

- [ ] **Step 4: Add truthful Server-Sent Events**

Emit only acknowledged upload progress and committed stage events. Include job_id, sequence, event_type, stage_id, completed_records, total_records when known, and timestamp.

- [ ] **Step 5: Map optional Temporal execution to the same states**

Temporal uses the same Go operators and persisted events; it does not create another data-plane implementation.

- [ ] **Step 6: Verify restart, reconnect, and commit**

~~~bash
cd apps/api
go test ./internal/verity -run 'TestJob|TestAutoRun|TestEventReplay' -count=1
git add internal/verity
git commit -m "Auto-run imports with durable job events"
~~~

### Task 4: Convert the Go data plane to bounded-memory execution

**Files:**
- Create: apps/api/internal/verity/record_iterator.go
- Create: apps/api/internal/verity/stage_executor.go
- Create: apps/api/internal/verity/dedup_index.go
- Create: apps/api/internal/verity/stage_page.go
- Create: apps/api/internal/verity/stage_executor_test.go
- Create: apps/api/internal/verity/pipeline_benchmark_test.go
- Modify: apps/api/internal/verity/pipeline.go
- Modify: apps/api/internal/verity/comparison.go
- Modify: apps/api/internal/verity/runner.go
- Modify: apps/api/go.mod

**Interfaces:**
- Produces: Operator.Apply(context.Context, CanonicalRecord) (OperatorResult, error).
- Produces: StageExecutor.Run(context.Context, RecordIterator, []Operator) (StageManifest, error).
- Produces: cursor-based StagePage with at most 200 rows.
- Uses: bbolt as a disk-backed fingerprint and dedup index scoped to one run.

- [ ] **Step 1: Benchmark current allocations**

Measure 10,000, 100,000, and 1,000,000 rows with wall time, rows/second, bytes/op, allocations/op, peak RSS, input bytes, and output bytes.

- [ ] **Step 2: Replace whole-stage slices with iterators**

Each stage reads one record, writes one result and decision, updates counters, and releases the record. Temporary stage artifacts publish only after successful completion.

- [ ] **Step 3: Move deduplication to bbolt**

Use canonical fingerprints as keys and retain first record ID and source offset. Duplicate lineage preserves both identities without a full in-memory Go map.

- [ ] **Step 4: Write Parquet in bounded row groups**

Remove the whole-output copy, flush deterministic row groups, and verify output count against the manifest.

- [ ] **Step 5: Add cursor pagination**

Stage inspection returns no more than 200 rows. Before/after comparison accepts explicit record IDs or a deterministic sample and does not scan a full artifact per UI request.

- [ ] **Step 6: Verify scale and commit**

~~~bash
cd apps/api
go test ./internal/verity -run 'TestStageExecutor|TestMillionRecordPipeline' -count=1
go test ./internal/verity -bench BenchmarkPipeline -benchmem -run '^$'
git add internal/verity go.mod go.sum
git commit -m "Stream pipeline stages with bounded memory"
~~~

Acceptance: 1,000,000 records complete without OOM, all manifest counts match, and peak RSS is below 2 GiB on the recorded reference machine.

### Task 5: Replace the navigation-heavy UI with one upload-first workspace

**Files:**
- Modify: apps/web/src/components/workbench/workbench.tsx
- Create: apps/web/src/components/workbench/workspace-machine.ts
- Create: apps/web/src/components/workbench/empty-workspace.tsx
- Create: apps/web/src/components/workbench/import-dropzone.tsx
- Create: apps/web/src/components/workbench/job-progress.tsx
- Create: apps/web/src/components/workbench/workspace-toolbar.tsx
- Create: apps/web/src/components/workbench/workspace-drawers.tsx
- Modify: apps/web/src/components/workbench/pipeline-flow.tsx
- Modify: apps/web/src/components/workbench/stage-comparison.tsx
- Modify: apps/web/src/components/workbench/dialogs.tsx
- Modify: apps/web/src/components/workbench/pages.tsx
- Modify: apps/web/src/app/globals.css
- Modify: apps/web/src/lib/contracts.ts
- Modify: apps/web/e2e/workbench.spec.ts
- Modify: apps/web/package.json

**Interfaces:**
- Produces: WorkspaceScreenState = empty | uploading | processing | needs_input | ready | failed.
- Produces: ImportDropzone with click, keyboard, drag/drop, multi-file, cancel, retry, and resume.
- Consumes: Task 3 jobs/SSE and Task 4 cursor-based stage pages.
- Uses: tus-js-client and @tanstack/react-virtual.

- [ ] **Step 1: Rewrite E2E around the intended flow**

~~~text
open / → drop noisy.csv → upload starts → pipeline appears → stages commit
→ review appears → accept item → ready output appears → download manifest
~~~

The test fails before redesign because the existing app requires Data navigation and a second Run action.

- [ ] **Step 2: Remove primary sidebar and global Run pipeline**

Keep one top bar with workspace switcher, status, Add data, and overflow. Put history, workflow settings, integrations, automation, and workspace settings in drawers. Retry appears only for a failed job.

- [ ] **Step 3: Build the real drop surface**

The empty screen is the drop target. A populated workspace has visible Add data plus a whole-window drag overlay. Drag, unsupported file, progress, pause, resume, cancel, and retry states work with keyboard and assistive technology.

- [ ] **Step 4: Remove browser-memory parsing**

Delete file.text(), parseCsv(), and the 10,000-row slice. Upload first, then render the server's format detection, schema profile, row count, and bounded sample.

- [ ] **Step 5: Transition directly into pipeline state**

Show the input node when upload begins. Activate stages from committed SSE events. On review_required, focus Review N items. On output_ready, make Download result primary.

- [ ] **Step 6: Add progressive inspection**

Virtualize rows. Highlight changed fields, strike and fade removed sample rows, and show reason and lineage behind disclosure. Respect prefers-reduced-motion and never animate uncommitted counts.

- [ ] **Step 7: Verify and commit**

~~~bash
npm run lint --workspace @verity/web
npm run test --workspace @verity/web
npm run test:e2e --workspace @verity/web
npm run build --workspace @verity/web
git add apps/web
git commit -m "Make Verity upload-first and automatically progressive"
~~~

### Task 6: Ship an API-backed Go CLI

**Files:**
- Create: apps/api/cmd/verity/main.go
- Create: apps/api/internal/client/client.go
- Create: apps/api/internal/client/uploads.go
- Create: apps/api/internal/client/jobs.go
- Create: apps/api/internal/client/reviews.go
- Create: apps/api/internal/client/outputs.go
- Create: apps/api/internal/client/client_test.go
- Modify: apps/api/go.mod
- Modify: Makefile
- Create: docs/cli.md

**Interfaces:**
- Produces: verity import, verity status, verity inspect, verity review, and verity export.
- Consumes: only the REST lifecycle from Tasks 1–4.

- [ ] **Step 1: Test commands against httptest.Server**

Assert exit codes, JSON output, resumable retries, bearer token handling, idempotency, SSE reconnect, review submission, and output checksum verification.

- [ ] **Step 2: Implement the full automation command**

~~~bash
verity import ./events.csv --server http://127.0.0.1:8000 --wait --output ./ready.parquet
~~~

The command uploads, finalizes, follows events, prints review instructions when needed, downloads output, and verifies its checksum.

- [ ] **Step 3: Make it automation-safe**

Every command supports --json. Progress goes to stderr and final structured output goes to stdout. Credentials come from VERITY_API_TOKEN or an explicit credential provider, not command history.

- [ ] **Step 4: Verify and commit**

~~~bash
cd apps/api
go test ./internal/client -count=1
go run ./cmd/verity import ../../sample_data/examples/noisy-workflow-events.csv --wait --output /tmp/verity-ready.parquet
git add cmd/verity internal/client go.mod go.sum ../../Makefile ../../docs/cli.md
git commit -m "Add automated Verity CLI workflows"
~~~

### Task 7: Add a thin MCP server over the REST client

**Files:**
- Create: apps/api/cmd/verity-mcp/main.go
- Create: apps/api/internal/mcpserver/server.go
- Create: apps/api/internal/mcpserver/tools.go
- Create: apps/api/internal/mcpserver/server_test.go
- Modify: apps/api/go.mod
- Create: docs/mcp.md

**Interfaces:**
- Produces: verity_import, verity_job_status, verity_inspect_stage, verity_list_reviews, verity_submit_review, and verity_export.
- Consumes: apps/api/internal/client; MCP never invokes the data plane directly.
- Uses: github.com/modelcontextprotocol/go-sdk/mcp.

- [ ] **Step 1: Test official SDK transport and schemas**

Use the official Go SDK client over stdio. Assert discovery, JSON schema validation, cancellation, progress, pagination, API error mapping, and one complete small import-to-export flow.

- [ ] **Step 2: Keep tools bounded**

verity_import accepts a local path in local stdio mode or an existing import ID remotely. It never puts file bytes, raw prompts, or full stage artifacts into model context.

- [ ] **Step 3: Protect review mutation**

Read tools return bounded summaries. verity_submit_review requires record ID, decision, and reason and returns the append-only lineage ID.

- [ ] **Step 4: Verify and commit**

~~~bash
cd apps/api
go test ./internal/mcpserver ./internal/client -count=1
git add cmd/verity-mcp internal/mcpserver go.mod go.sum ../../docs/mcp.md
git commit -m "Expose Verity automation through MCP"
~~~

### Task 8: Add load, resilience, Curvatus preparation, and acceptance gates

**Files:**
- Create: tests/load/import-smoke.js
- Create: tests/load/import-stress.js
- Create: tests/load/job-read-load.js
- Create: tests/resilience/restart-run.sh
- Create: tests/acceptance/format-parity.sh
- Create: deploy/kubernetes/base/api.yaml
- Create: deploy/kubernetes/base/web.yaml
- Create: deploy/kubernetes/base/services.yaml
- Create: deploy/kubernetes/base/pvc.yaml
- Create: deploy/kubernetes/base/kustomization.yaml
- Create: .github/workflows/verify.yml
- Modify: Makefile
- Modify: README.md
- Modify: docs/production-readiness.md

**Interfaces:**
- Produces: make verify, make e2e, make load, make stress, and make acceptance.
- Produces: a generic Kubernetes base for mapping to Curvatus once its internal template is available.
- Consumes: web, REST, CLI, and MCP flows from Tasks 1–7.

- [ ] **Step 1: Add k6 profiles**

Cover smoke, expected load, stress, spike, breakpoint, and soak. Assert job-state monotonicity, idempotency, output checksum, and no duplicate publication. Record Curvatus baseline latency before converting latency measurements into release thresholds.

- [ ] **Step 2: Add scale gates**

~~~text
Pull request: 10,000 rows in every core format
Nightly:      100,000 rows in every core format plus 1,000,000 mixed-noise JSONL
Manual:       10,000,000 mixed-noise JSONL breakpoint run
~~~

Large files are generated from seed 20260906 and are never committed. Test artifacts retain manifests and checksums.

- [ ] **Step 3: Add resilience tests**

Interrupt and resume upload, restart API during a local job, restart a Temporal worker during a stage, reconnect SSE, repeat completion, cancel a run, fill artifact storage, corrupt one source, and prove the last successful output remains readable.

- [ ] **Step 4: Add generic Kubernetes manifests**

Web is stateless and scalable. API remains one replica with persistent artifacts until control state and locking become distributed. Configure health, readiness, upload limits, timeouts, non-root containers, resource limits, secrets, and network policy.

- [ ] **Step 5: Run full cross-surface acceptance**

From an empty workspace, complete the same import through web drag/drop, REST, CLI, and MCP. Compare counts, sampled transformations, lineage, review outcomes, and output checksums. Inspect desktop, mobile, keyboard, focus, reduced motion, console health, and failure recovery.

- [ ] **Step 6: Run release gates**

~~~bash
make verify
make e2e
make load
make acceptance
~~~

make stress runs nightly or in a dedicated load environment.

- [ ] **Step 7: Update readiness from evidence**

Record reference machine or cluster, fixture manifest, throughput, wall time, peak RSS, storage growth, latency, error rate, and breaking point. Production-ready status requires Curvatus restart, persistence, authorization, and load gates.

- [ ] **Step 8: Commit and push after acceptance review**

~~~bash
git add tests deploy .github Makefile README.md docs/production-readiness.md
git commit -m "Add production acceptance and scale gates"
git push
~~~

## Self-review

- Spec coverage: upload-first flow, automatic execution, human review, progressive inspection, format breadth, large-data behavior, REST, CLI, MCP, privacy, load testing, and Curvatus preparation each map to a task.
- Placeholder scan: no implementation placeholder remains. Curvatus-specific packaging is separated because its internal contract is unavailable; the generic Kubernetes base is fully defined.
- Type consistency: Import, Job, JobEvent, RecordStream, StagePage, and the REST client are defined before consumers.

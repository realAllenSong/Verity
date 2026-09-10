# Verification report

Verified on 2026-09-07 against the all-Go local data plane and the Next.js production build.

## End-to-end evidence

| Profile | Source | Records | Source bytes | Wall time | Peak API RSS | Result |
| --- | --- | ---: | ---: | ---: | ---: | --- |
| Load | generated noisy JSONL | 100,000 | 37,613,557 | 11 s | 40,036 KiB | passed |
| Stress (latest, cached inspection) | generated noisy JSONL | 1,000,000 | 377,116,497 | 104 s | 45,956 KiB | passed |

Each profile exercised fixture generation, resumable chunk upload, automatic job creation, every pipeline stage, Parquet publication, download, record-count validation, and server/client SHA-256 agreement. Reports are written to `artifacts/load/` when the commands run.

The near-flat memory profile between 100,000 and 1,000,000 records confirms bounded stage execution for these workloads; it is not a distributed-scale or concurrency SLO.

On the latest million-row run, all seven stage-comparison HTTP reads completed in 1.0–1.5 ms locally. Before/after samples are captured during execution and committed with each stage, so opening a stage does not scan a million rows. This measures bounded sample requests, not arbitrary full-dataset searches or deep pagination. The 100k timing above predates that inspection-cache addition.

## Reproduce

```bash
make verify
make e2e
make load
make stress
```

`make acceptance` also runs Go race detection, API/CLI/MCP tests, frontend unit tests, lint, production build, and the browser suite.

## Format coverage

CSV, TSV, JSON arrays/objects, JSONL, NDJSON, Parquet, and gzip-compressed structured text are covered by parser and type-preservation tests. The checked-in 10,000-record CSV, JSONL, and Parquet fixtures have matching deterministic manifests. Browser acceptance imports CSV; the load and stress profiles import JSONL; Parquet publication and checksum verification run in all full lifecycle profiles.

## Product acceptance

The Playwright suite verifies the upload-first entry point, real drag-and-drop, automatic execution, inspection dialogs across all seven stages, result download, contextual human review beyond the original eight samples, working overflow actions, no legacy run button or navigation rail, no browser console errors in the upload flow, and a 390-pixel viewport without horizontal overflow. Desktop and mobile screenshots are generated under `output/playwright/` during visual QA and are intentionally not source artifacts.

Go regression tests verify that reviewing a previously unsampled record publishes a new JSONL/CSV/Parquet revision, an exclusion removes it, old snapshots remain addressable and unchanged, and decisions survive restart. Immutable run-stage counts describe the original automated execution; the review badge and downloaded revision describe the current human decisions.

## Visual refinement verification (2026-09-10)

The upload-first workspace was refined using the applicable Taste audit principles and frontend-design guidance, retaining Radix Themes and Geist rather than introducing a second component system. Language-shaped sources now open in a bounded, text-first evidence reader: titles and prose stay readable, inline word-level changes mark the actual transformation, and structured Table inspection remains one click away. All fields and source JSON remain accessible. No data-processing or API contracts changed in this refinement.

Fresh verification after the refinement:

- ESLint passed, and all eleven frontend unit tests passed.
- The Next.js production build and all eleven Chromium end-to-end tests passed. These include the default text reader with inline word changes, the Table fallback, real CSV drag/drop, automatic processing, every stage, human decisions reflected in downloaded results, full-record pagination, technical-field disclosure, keyboard inspection, 390px mobile and 900px tablet layouts, system appearance changes, and reduced-motion mode.
- Light/dark desktop, mobile, tablet, and before/after dialog screenshots were visually inspected. Artifacts are generated in `output/playwright/` and are not committed.
- Token-pair contrast checks: light secondary text on canvas 4.68:1, primary button text 5.97:1, dark secondary text on canvas 6.78:1. These spot checks are not a full accessibility certification.
- A test initially selected Next.js's temporary hidden streamed fragment as well as the visible page. The regression now waits for the single settled workspace before inspection; no hydration errors occurred in the completed checks.

The backend load results above were not rerun for this frontend-only change. This pass does not add evidence for other browsers or enterprise deployment.

## Boundaries

- These are local Chromium and synthetic-data tests, not a claim that every browser, connector, or enterprise policy has been validated.
- No 10-million-row run, sustained concurrent workload, or cluster failover was executed.
- Kustomize renders seven resources and Compose configuration validates. Docker runtime and Kubernetes/Curvatus deployment were not tested because those services were unavailable.

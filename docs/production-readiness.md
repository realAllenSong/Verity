# Production readiness

## Current classification

Verity is a productized local reference implementation. It is robust for a single trusted workspace and synthetic or approved local datasets. It is not yet an enterprise multi-tenant production service.

This distinction is intentional. A green build does not prove connector consent, tenant isolation, regulatory approval, or distributed recovery.

## Readiness matrix

| Area | Current state | Enterprise next step |
| --- | --- | --- |
| API contract | Go API, strict payloads, bounded bodies, structured errors, OpenAPI | contract compatibility and version-deprecation tests |
| Local persistence | atomic `0600` state, durable reviews, checksummed batches | transactional database, backups, migrations, tenant keys |
| Idempotency | stable batch keys prevent duplicate staging | distributed idempotency store and expiry policy |
| Execution | in-process Go engine, serialized run queue, per-stage committed events; native optional Temporal workflow | API-restart reconciliation, production namespace, worker isolation, cancellation and chaos tests |
| Data quality | named stage checks plus reproducible JSONL, CSV, and typed Parquet artifacts | declarative suites, drift monitors, quality SLOs |
| Observability | request IDs, JSON logs, readiness, run events | metrics, traces, alerting, OpenLineage transport |
| Security | optional bearer token, CORS allowlist, local file permissions | SSO, RBAC/ABAC, secrets broker, encryption keys, audit export |
| Privacy | raw stays in the configured Verity runtime; synthetic redaction rules and bounded projections | field-level authorization, policy enforcement, retention/deletion, DPIA/legal/employee review |
| UI | responsive upload-first flow, bounded/full-stage inspection, versioned reviews, and error feedback | pause/cancel/recovery, configurable recipes, user studies and accessibility audit |
| Verification | Go/unit/Temporal/browser/build tests, deterministic noisy fixtures, and 100k/1m end-to-end load profiles | sustained concurrency, chaos, penetration, restore, upgrade, and compliance tests |

## Failure behavior already covered

- malformed and unknown JSON fields are rejected
- large or empty batches are rejected
- duplicate staging requests return the existing batch
- only one pipeline run can execute at a time
- data-plane or Temporal failures create FAIL lifecycle events and do not publish a successful workspace
- review decisions and control state survive service restart
- the frontend commits review changes only after server success and shows failed-save errors
- runtime runs do not mutate checked-in fixture data

See the [implementation checkpoint](product/upload-first-workbench-spec.md#implementation-checkpoint-2026-09-08) for open product criteria. Local API jobs are not yet recovered automatically after process interruption. A successful job means the accepted branch is ready; it does not mean every uncertain row was approved.

## Exit criteria for enterprise pilot

1. One supported connector completes consent, incremental sync, revocation, and deletion tests.
2. SSO and role-scoped evidence access are enforced server-side.
3. Control state is moved to a transactional store with migrations and restore testing.
4. The Temporal worker is isolated and retry/cancellation semantics pass failure-injection tests.
5. Threat modeling, privacy review, retention rules, and audit export are approved.
6. Load targets and service-level objectives are defined and passed.

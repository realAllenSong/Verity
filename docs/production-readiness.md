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
| Execution | bounded worker process, concurrency guard, lifecycle events | durable queue, retries, heartbeats, cancellation, worker isolation |
| Data quality | named stage checks and reproducible artifacts | declarative suites, drift monitors, quality SLOs |
| Observability | request IDs, JSON logs, readiness, run events | metrics, traces, alerting, OpenLineage transport |
| Security | optional bearer token, CORS allowlist, local file permissions | SSO, RBAC/ABAC, secrets broker, encryption keys, audit export |
| Privacy | local-only raw boundary and safe workspace projection | policy enforcement, retention/deletion, DPIA/legal/employee review |
| UI | complete responsive workbench and error feedback | real-user usability and accessibility studies |
| Verification | Python/Go/unit/browser/build tests and deterministic noisy fixtures | load, chaos, penetration, restore, upgrade, and compliance tests |

## Failure behavior already covered

- malformed and unknown JSON fields are rejected
- large or empty batches are rejected
- duplicate staging requests return the existing batch
- only one pipeline run can execute at a time
- data-plane failures create FAIL lifecycle events and do not publish a successful workspace
- review decisions and control state survive service restart
- the frontend rolls back a failed review mutation and shows an explicit operation result
- runtime runs do not mutate checked-in fixture data

## Exit criteria for enterprise pilot

1. One supported connector completes consent, incremental sync, revocation, and deletion tests.
2. SSO and role-scoped evidence access are enforced server-side.
3. Control state is moved to a transactional store with migrations and restore testing.
4. The worker is isolated and orchestrated durably with retry/cancellation semantics.
5. Threat modeling, privacy review, retention rules, and audit export are approved.
6. Load targets and service-level objectives are defined and passed.

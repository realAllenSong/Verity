# Integrations

Verity keeps its default deployment small. Integrations are enabled through configuration,
not required for the core API, pipeline, UI, or local artifacts.

## Temporal

Use Temporal when worker execution needs activity retries or a separate worker pool. The
integration uses the Temporal Go SDK and runs the same pipeline function as local mode.

Local development:

```bash
docker compose -f compose.yaml -f compose.temporal.yaml up --build
```

Existing Temporal Cloud or self-hosted service:

```bash
export VERITY_ORCHESTRATOR=temporal
export VERITY_TEMPORAL_ADDRESS=temporal.example.internal:7233
export VERITY_TEMPORAL_NAMESPACE=verity
export VERITY_TEMPORAL_TASK_QUEUE=verity-pipeline
make temporal-worker
make api
```

The API and worker must see the same artifact store. For multi-node production this should
be an approved object or network store rather than the local filesystem.

The current HTTP run endpoint waits for completion. Temporal preserves workflow and
activity state across worker failures, but Verity does not yet reconcile an orphaned
workflow after the API process itself restarts; that remains an enterprise-pilot gate.

## Airbyte

Verity does not install or operate Airbyte. Configure an existing Airbyte Cloud or
self-managed API:

```bash
export VERITY_AIRBYTE_BASE_URL=https://api.airbyte.com/v1
export VERITY_AIRBYTE_TOKEN=replace-with-an-airbyte-token
make api
```

Trigger a configured connection:

```bash
curl -X POST http://127.0.0.1:8000/api/v1/integrations/airbyte/syncs \
  -H 'Content-Type: application/json' \
  -d '{"connection_id":"your-connection-id"}'
```

Read the returned job:

```bash
curl http://127.0.0.1:8000/api/v1/integrations/airbyte/jobs/1234
```

Airbyte should write into a destination adapter that emits the generic Verity envelope.
Verity never receives or stores the source connector's OAuth credentials.

## Why Dagster is not embedded

Dagster remains interoperable at the HTTP and artifact boundaries, but it is not part of
the supported runtime. Embedding it would require Python code locations plus additional
webserver and daemon services while duplicating the durable orchestration role already
served by Temporal.

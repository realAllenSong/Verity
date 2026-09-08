# Kubernetes and Curvatus handoff

The base keeps the web tier stateless and the local-control API at one replica with a `ReadWriteOnce` persistent volume. This is the safe topology until the control store and run lock move to a distributed database.

Replace both images and the allowed origin in an environment overlay, then apply with the Curvatus-approved wrapper around Kustomize:

```bash
kubectl apply -k deploy/kubernetes/base
```

Build `verity-web` with `NEXT_PUBLIC_API_URL` set to the browser-reachable API or ingress URL. `VERITY_API_URL=http://verity-api:8000` is separately used for server-side rendering inside the cluster.

The base relies on cluster NetworkPolicy and the firm's ingress identity layer so the browser can use the product without receiving a shared service secret. `VERITY_API_TOKEN` remains available for CLI/MCP-only or gateway-to-service deployments; do not enable it on a browser-direct API route.

Curvatus-specific workload identity, ingress, secret references, storage class, image registry, admission labels, and observability annotations belong in a private overlay once its internal template is available. Temporal and Airbyte remain optional adapters and are not required by this base.

The base deliberately does **not** expose API port 8000 to other namespaces. The private overlay must allow only the authenticated ingress/gateway pods to reach that port. Both `/` and `/api/v1/*` need ingress routes (including `/openapi.json` if desired), TLS, upload/body-size limits, and streaming-friendly timeouts. CORS is not authentication. Do not publish this base to an untrusted network without that gateway policy.

The API uses UID/GID 65532 and a matching PVC fsGroup; the web container uses UID/GID 1000 with writable temporary/cache mounts. Kustomize rendering and Compose configuration were validated locally. Container startup and deployment were not tested because no Docker daemon or Kubernetes cluster was available.

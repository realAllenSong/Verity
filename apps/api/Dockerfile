FROM golang:1.24-bookworm AS api-build

WORKDIR /src/apps/api
COPY apps/api/go.mod ./
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verity-api ./cmd/verity-api

FROM ghcr.io/astral-sh/uv:python3.12-bookworm-slim

WORKDIR /app/engine
COPY apps/engine/pyproject.toml apps/engine/uv.lock ./
RUN uv sync --frozen --no-dev
COPY apps/engine/app ./app
COPY packages/contracts/openapi.json /app/contracts/openapi.json
COPY apps/web/src/data/demo-workspace.json /app/seed/demo-workspace.json
COPY --from=api-build /out/verity-api /usr/local/bin/verity-api

ENV VERITY_API_ADDR=0.0.0.0:8000 \
    VERITY_REPO_ROOT=/workspace \
    VERITY_ENGINE_DIR=/app/engine \
    VERITY_SEED_WORKSPACE=/app/seed/demo-workspace.json \
    VERITY_OPENAPI_PATH=/app/contracts/openapi.json

EXPOSE 8000
CMD ["verity-api"]

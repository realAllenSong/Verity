FROM golang:1.24-bookworm AS build

WORKDIR /src/apps/api
COPY apps/api/go.mod apps/api/go.sum ./
RUN go mod download
COPY apps/api/ ./
RUN CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verity-api ./cmd/verity-api \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verity-worker ./cmd/verity-worker \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verity ./cmd/verity \
    && CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o /out/verity-mcp ./cmd/verity-mcp
RUN mkdir -p /out/workspace/artifacts /out/workspace/sample_data/raw

FROM gcr.io/distroless/static-debian12:nonroot

COPY --from=build /out/verity-api /usr/local/bin/verity-api
COPY --from=build /out/verity-worker /usr/local/bin/verity-worker
COPY --from=build /out/verity /usr/local/bin/verity
COPY --from=build /out/verity-mcp /usr/local/bin/verity-mcp
COPY --from=build --chown=65532:65532 /out/workspace /workspace
COPY --chown=65532:65532 packages/contracts/openapi.json /app/contracts/openapi.json
COPY --chown=65532:65532 apps/web/src/data/demo-workspace.json /app/seed/demo-workspace.json

ENV VERITY_API_ADDR=0.0.0.0:8000 \
    VERITY_REPO_ROOT=/workspace \
    VERITY_SEED_WORKSPACE=/app/seed/demo-workspace.json \
    VERITY_OPENAPI_PATH=/app/contracts/openapi.json

EXPOSE 8000
USER nonroot:nonroot
CMD ["/usr/local/bin/verity-api"]

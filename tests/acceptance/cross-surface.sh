#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/../.." && pwd)"

cd "$repo_root/apps/api"
go test ./... -count=1
go test -race ./internal/verity ./internal/client ./internal/mcpserver -count=1

cd "$repo_root"
npm run lint
npm run test:web
npm run build
npm run test:e2e --workspace @verity/web

echo "REST, CLI, MCP, Go pipeline, and browser acceptance passed."

.PHONY: bootstrap generate fixtures-small fixtures-load fixtures-stress contracts api temporal-worker web cli mcp test lint build verify e2e acceptance load stress

bootstrap:
	npm install
	cd apps/api && go mod download

generate:
	cd apps/api && go run ./cmd/verity-generate --root ../.. --refresh-fixtures --write-web-fixture

fixtures-small:
	mkdir -p sample_data/examples sample_data/manifests
	cd apps/api && go run ./cmd/verity-generate --records 10000 --format csv --seed 20260906 --noise-profile mixed --output ../../sample_data/examples/noisy-workflow-events.csv
	cd apps/api && go run ./cmd/verity-generate --records 10000 --format jsonl --seed 20260906 --noise-profile mixed --output ../../sample_data/examples/noisy-workflow-events.jsonl
	cd apps/api && go run ./cmd/verity-generate --records 10000 --format parquet --seed 20260906 --noise-profile mixed --output ../../sample_data/examples/noisy-workflow-events.parquet

fixtures-load:
	mkdir -p artifacts/load
	cd apps/api && go run ./cmd/verity-generate --records 1000000 --format jsonl --seed 20260906 --noise-profile mixed --output ../../artifacts/load/noisy-workflow-events-1m.jsonl

fixtures-stress:
	mkdir -p artifacts/stress
	cd apps/api && go run ./cmd/verity-generate --records 10000000 --format jsonl --seed 20260906 --noise-profile mixed --output ../../artifacts/stress/noisy-workflow-events-10m.jsonl

contracts:
	cd apps/api && go test ./internal/verity -run TestOpenAPIContainsEveryPublicOperation

api:
	cd apps/api && go run ./cmd/verity-api

temporal-worker:
	cd apps/api && go run ./cmd/verity-worker

web:
	VERITY_API_URL=http://127.0.0.1:8000 NEXT_PUBLIC_API_URL=http://127.0.0.1:8000 npm run dev

cli:
	mkdir -p bin
	cd apps/api && go build -o ../../bin/verity ./cmd/verity

mcp:
	mkdir -p bin
	cd apps/api && go build -o ../../bin/verity-mcp ./cmd/verity-mcp

test:
	cd apps/api && go test ./...
	npm run test:web

lint:
	cd apps/api && go vet ./...
	npm run lint

build:
	npm run build

verify: generate contracts test lint build

e2e:
	npm run test:e2e --workspace @verity/web

acceptance:
	bash tests/acceptance/cross-surface.sh

load:
	VERITY_LOAD_RECORDS=100000 bash tests/load/import-e2e.sh

stress:
	VERITY_LOAD_RECORDS=1000000 bash tests/load/import-e2e.sh

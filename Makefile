.PHONY: bootstrap generate contracts api web test lint build verify

bootstrap:
	npm install
	cd apps/engine && uv sync --extra dev
	cd apps/api && go mod download

generate:
	cd apps/engine && uv run python -m app.cli generate --root ../.. --refresh-fixtures --write-web-fixture

contracts:
	python3 -m json.tool packages/contracts/openapi.json >/dev/null

api:
	cd apps/api && go run ./cmd/verity-api

web:
	npm run dev

test:
	cd apps/engine && uv run pytest
	cd apps/api && go test ./...
	npm run test:web

lint:
	cd apps/engine && uv run ruff check app tests
	cd apps/api && go vet ./...
	npm run lint

build:
	npm run build

verify: generate contracts test lint build

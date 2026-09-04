.PHONY: bootstrap generate contracts api web test lint build verify

bootstrap:
	npm install
	cd apps/api && uv sync --extra dev

generate:
	cd apps/api && uv run python -m app.cli generate --root ../..

contracts:
	cd apps/api && uv run python -m app.export_openapi

api:
	cd apps/api && uv run uvicorn app.main:app --reload --host 127.0.0.1 --port 8000

web:
	npm run dev

test:
	cd apps/api && uv run pytest
	npm run test:web

lint:
	cd apps/api && uv run ruff check app tests
	npm run lint

build:
	npm run build

verify: generate contracts test lint build

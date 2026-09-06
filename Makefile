.PHONY: bootstrap generate contracts api temporal-worker web test lint build verify

bootstrap:
	npm install
	cd apps/api && go mod download

generate:
	cd apps/api && go run ./cmd/verity-generate --root ../.. --refresh-fixtures --write-web-fixture

contracts:
	cd apps/api && go test ./internal/verity -run TestOpenAPIContainsEveryPublicOperation

api:
	cd apps/api && go run ./cmd/verity-api

temporal-worker:
	cd apps/api && go run ./cmd/verity-worker

web:
	npm run dev

test:
	cd apps/api && go test ./...
	npm run test:web

lint:
	cd apps/api && go vet ./...
	npm run lint

build:
	npm run build

verify: generate contracts test lint build

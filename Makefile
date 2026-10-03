GO ?= go

export DOCKER_HOST ?= unix:///run/user/$(shell id -u)/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED ?= true

.PHONY: run test lint fmt migrate gen seed up down podman-env

run:
	$(GO) run ./cmd/api

test:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...
	@test -z "$$($(GO) tool gofumpt -l . | tee /dev/stderr)" || (echo "gofumpt: files need formatting" && exit 1)

fmt:
	$(GO) tool gofumpt -w .

migrate:
	$(GO) tool goose -dir migrations up

gen:
	$(GO) tool sqlc generate

seed:
	$(GO) run ./cmd/seed

up:
	podman-compose -f deploy/compose.yml up -d

down:
	podman-compose -f deploy/compose.yml down

podman-env:
	@echo "export DOCKER_HOST=$(DOCKER_HOST)"
	@echo "export TESTCONTAINERS_RYUK_DISABLED=$(TESTCONTAINERS_RYUK_DISABLED)"

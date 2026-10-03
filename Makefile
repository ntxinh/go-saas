GO ?= go

export DOCKER_HOST ?= unix:///run/user/$(shell id -u)/podman/podman.sock
export TESTCONTAINERS_RYUK_DISABLED ?= true
export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE ?= /run/user/$(shell id -u)/podman/podman.sock

.PHONY: run worker test lint arch sec fmt migrate gen seed up down up-app podman-env

run:
	$(GO) run ./cmd/api

worker:
	$(GO) run ./cmd/worker

test:
	$(GO) test -race ./...

lint:
	$(GO) vet ./...
	@test -z "$$($(GO) tool gofumpt -l . | tee /dev/stderr)" || (echo "gofumpt: files need formatting" && exit 1)
	$(GO) run github.com/golangci/golangci-lint/v2/cmd/golangci-lint@v2.14.0 run

arch:
	$(GO) run github.com/fe3dback/go-arch-lint@v1.19.0 check

sec:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...

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

up-app:
	podman-compose -f deploy/compose.yml -f deploy/compose.app.yml up -d --build

podman-env:
	@echo "export DOCKER_HOST=$(DOCKER_HOST)"
	@echo "export TESTCONTAINERS_RYUK_DISABLED=$(TESTCONTAINERS_RYUK_DISABLED)"
	@echo "export TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE=$(TESTCONTAINERS_DOCKER_SOCKET_OVERRIDE)"
	@echo "systemctl --user enable --now podman.socket"

# go-saas

Multi-tenant B2B workspace backend: organizations, memberships, invites,
RBAC roles. Supabase Auth for identity, Postgres + Redis, asynq jobs,
Watermill events, OpenTelemetry to Grafana Cloud.

## Quickstart

```sh
mise install                     # pinned Go toolchain
cp .env.example .env             # fill in Supabase + key values
make up                          # local deps (postgres, redis, mailpit)
make migrate run                 # schema, then API on :8080
```

Tests run against Podman containers; `make test` exports the needed env
(`DOCKER_HOST` + `TESTCONTAINERS_RYUK_DISABLED`) automatically:

```sh
make test                        # go test -race ./...
make podman-env                  # print the exports, e.g. for your shell
```

## Commands

| Command      | What |
|--------------|------|
| `make run` / `make worker` | API on :8080 / asynq worker |
| `make seed`  | Fake users+orgs ( `-local` skips Supabase: `go run ./cmd/seed -local` ) |
| `make lint`  | go vet + gofumpt check + golangci-lint |
| `make arch`  | go-arch-lint: enforces the feature-slice boundary |
| `make sec`   | govulncheck |
| `make up-app`| api+worker containers on the deps stack (compose overlay) |

CI (`.github/workflows/ci.yml`): lint → arch → test(-race, containers) →
govulncheck → container build; images push to GHCR on `main`.

## Docs

- [DESIGN.md](DESIGN.md) — one-page architecture summary
- [docs/](docs/README.md) — design doc index
- [docs/superpowers/specs/](docs/superpowers/specs/) — the approved spec

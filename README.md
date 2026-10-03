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

## Docs

- [DESIGN.md](DESIGN.md) — one-page architecture summary
- [docs/](docs/README.md) — design doc index
- [docs/superpowers/specs/](docs/superpowers/specs/) — the approved spec

# sso

[![Go CI](https://github.com/Mas4trt/sso/actions/workflows/ci.yml/badge.svg)](https://github.com/Mas4trt/sso/actions/workflows/ci.yml)
[![Go Report Card](https://goreportcard.com/badge/github.com/Mas4trt/sso)](https://goreportcard.com/report/github.com/Mas4trt/sso)
[![Go Reference](https://pkg.go.dev/badge/github.com/Mas4trt/sso.svg)](https://pkg.go.dev/github.com/Mas4trt/sso)

A single sign-on / auth service exposing a plain gRPC API (`auth.v1.Auth`).
Issues and verifies JWT access tokens, manages rotating opaque refresh
tokens, and lets multiple downstream services authenticate their users
against one shared identity store.

> Looking for how to call this service from your own backend? See
> [`docs/Connecting.md`](docs/Connecting.md).

## Table of contents

- [Architecture](#architecture)
- [Quick start](#quick-start)
- [Configuration](#configuration)
- [API surface](#api-surface)
- [Security model](#security-model)
- [Observability](#observability)
- [Testing](#testing)
- [Deployment](#deployment)
- [Known limitations & roadmap](#known-limitations--roadmap)
- [Contributing](#contributing)

## Architecture

```
                 ┌──────────────┐        ┌─────────────┐
 consuming svc → │  sso (gRPC)  │ ─────→ │  PostgreSQL │
 (private net)   │  :44044      │        │  users/apps/│
                 └──────┬───────┘        │  refresh_tk │
                         │                └─────────────┘
                         │
                 ┌───────┴───────┐
 browser  →  Envoy (grpc-web) ───┘
             :8080
```

- **Transport**: plaintext gRPC on the private network (`insecure.NewCredentials()`
  today — see [Security model](#security-model)). Browsers can't speak raw
  gRPC, so `envoy.yaml` translates grpc-web ↔ gRPC for `sso-web` and any
  other browser client.
- **State**: PostgreSQL only. No cache/queue dependency in the base
  deployment (see the app-secret in-memory cache under
  [Observability](#observability) — that's process-local, not shared
  state).
- **Identity boundary**: each consuming service is an "app" (`apps` table)
  with its own HMAC secret. Access tokens are signed per-app, so one
  service's tokens are never valid against another's secret. See
  `make new-app` and [`docs/Connecting.md`](docs/Connecting.md).

## Quick start

```bash
git clone https://github.com/Mas4trt/sso.git
cd sso
cp .env.example .env          # fill in STORAGE_DSN if not using docker-compose
make docker-up                # postgres + migrate + sso + envoy
make new-app NAME=my-service  # prints application_id + secret
```

Health check:

```bash
grpc_health_probe -addr=localhost:44044
```

Local (non-container) dev loop:

```bash
docker compose up -d postgres
make migrate-up
make run   # go run ./cmd/sso --config=./config/local.yaml
```

## Configuration

Config is layered: YAML file (`--config` flag or `CONFIG_PATH` env) as the
base, environment variables override it (`env-default`/`env` tags via
[cleanenv](https://github.com/ilyakaznacheev/cleanenv)), `.env` is loaded
for local dev only.

| Key | Env var | Default | Notes |
|---|---|---|---|
| `env` | `ENV` | `local` | `local` \| `dev` \| `prod` — controls log format/level |
| `storage.driver` | `STORAGE_DRIVER` | *(required)* | only `postgres` is implemented |
| `storage.dsn` | `STORAGE_DSN` | *(required)* | postgres connection string |
| `grpc.port` | `GRPC_PORT` | `44044` | |
| `grpc.timeout` | `GRPC_TIMEOUT` | `5s` | |
| `token.ttl` | `TOKEN_TTL` | `1h` | access token lifetime |
| `token.refresh_ttl` | `TOKEN_REFRESH_TTL` | `720h` (30d) | refresh token lifetime |
| `migrations.path` | `MIGRATIONS_PATH` | *(required)* | |

All values are validated at startup (`internal/config.Config.Validate`);
the process refuses to start rather than run with a nonsensical config
(zero timeout, empty DSN, unsupported driver, ...).

## API surface

Contract lives in a separate repo,
[`Mas4trt/protos`](https://github.com/Mas4trt/protos) — this service only
depends on the generated Go client/server code from there, never a local
copy of the `.proto`.

| RPC | Auth required | Notes |
|---|---|---|
| `Register` | no | rate-limited (1 req/s/IP, burst 5) |
| `Authenticate` | no | rate-limited (1 req/s/IP, burst 5) |
| `RefreshTokens` | no (refresh token *is* the credential) | single-use, rotates on every call |
| `Logout` | no | idempotent |
| `GetRole` | yes (`Authorization: Bearer <access_token>`) | self, or admin-on-any-user |

Status code contract for consumers is documented in
[`docs/Connecting.md`](docs/Connecting.md#2-dial-sso-and-call-the-auth-service).

## Security model

- **Password storage**: bcrypt, default cost.
- **Access tokens**: JWT (HS256), signed with a per-app secret, 1h default
  TTL. Not revocable before expiry by design — keep the TTL short if that
  matters for your threat model.
- **Refresh tokens**: opaque 256-bit random values, stored server-side as
  a SHA-256 hash (never the raw token), single-use with rotation-on-refresh,
  explicitly revocable (`Logout`, and `RevokeAllUserTokens` for
  "sign out everywhere" on compromise).
- **Transport**: gRPC is plaintext (`insecure.NewCredentials()`) by
  default, intended for a private network / service mesh. **Terminate TLS
  in front of this service (sidecar, ingress, or mTLS mesh) before
  exposing it beyond a trusted network.** This is a deliberate scope
  decision documented in `docs/Connecting.md#6-tls`, not an oversight —
  but it's on you to close it in your environment.
- **Per-app isolation**: a compromised secret for service A cannot forge
  tokens accepted by service B.

Found a vulnerability? See [`SECURITY.md`](SECURITY.md).

## Observability

- **Logging**: structured (`log/slog`), JSON in `dev`/`prod`, human text in
  `local`. Every RPC is logged (method, duration, error) via
  `interceptors.Logging`.
- **Health**: standard gRPC health service
  (`grpc.health.v1.Health`), flips to `NOT_SERVING` during shutdown so a
  load balancer stops routing before the process stops accepting work.
- **Panics**: recovered per-RPC (`interceptors.Recovery`) and turned into
  `Internal` instead of crashing the process.
- **App-secret cache**: `internal/cache.AppCache` — read-through, 5 min
  TTL for hits / 30s for misses, in front of the `apps` table lookup that
  every authenticated RPC needs to verify a token's signature. This is
  process-local (not shared across replicas); rotating a secret in the DB
  takes up to the TTL to be picked up by an already-running pod unless you
  call `Invalidate`.
- **Metrics**: not yet wired up — see
  [Known limitations](#known-limitations--roadmap).

## Testing

```bash
make test-unit          # go test -race -cover -short ./...
make test-integration   # spins up real postgres via testcontainers, needs a docker daemon
make test               # both
```

CI runs unit and integration tests as separate jobs
(`.github/workflows/ci.yml`) so a missing docker daemon in some runner
doesn't block the fast unit suite.

## Deployment

- Container: multi-stage `Dockerfile`, distroless-adjacent `alpine`
  runtime, runs as a non-root user, built with `-trimpath` and stripped
  symbols.
- `docker-compose.yaml` is a reference stack (postgres + migrate + sso +
  envoy), not a production topology — no replicas, no external secrets
  manager, single postgres instance.
- Build metadata (`version`/`commit`/`build_date`) is stamped via
  `-ldflags` (see `Makefile`) and logged on startup — check it matches
  what you expect before debugging "is this the new build?" issues.
- Graceful shutdown: `SIGTERM`/`SIGINT` → mark `NOT_SERVING` → drain
  in-flight RPCs → hard stop after `grpcapp.ShutdownTimeout` (20s) if
  something's stuck. Set your orchestrator's termination grace period
  above that.

## Known limitations & roadmap

Documented on purpose, not hidden:

- No metrics/tracing (Prometheus/OpenTelemetry) yet — logging only.
- No TLS/mTLS built in; must be terminated externally.
- Access tokens aren't revocable before expiry (no denylist).
- Single-region PostgreSQL, no read replicas or connection pool tuning
  beyond pgx defaults.
- App-secret cache is per-process; rotation isn't instantly consistent
  across replicas (bounded by TTL).

## Contributing

See [`CONTRIBUTING.md`](CONTRIBUTING.md). `make ci` locally before
opening a PR — it's the same lint → unit-test → build sequence CI runs.

## License

See [`LICENSE`](LICENSE).

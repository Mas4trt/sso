# Security Policy

## Supported versions

Only the latest tagged release and `main` receive security fixes.

## Reporting a vulnerability

Please **do not** open a public GitHub issue for security reports.

Instead, use GitHub's private vulnerability reporting
(`Security` tab → `Report a vulnerability`) or email the maintainers
directly. Include:

- A description of the vulnerability and its impact.
- Steps to reproduce (minimal repro if possible).
- Affected version/commit.

We aim to acknowledge reports within 3 business days and to ship a fix or
mitigation plan within 30 days for confirmed high-severity issues.

## Scope

In scope:

- The `sso` gRPC service itself (`cmd/`, `internal/`, `pkg/`).
- Token issuance, verification, and refresh-token rotation logic.
- The Docker image and `docker-compose.yaml` reference deployment.

Out of scope (by design, documented in the README's
[Security model](README.md#security-model)):

- Lack of TLS on the raw gRPC listener — this service is designed to run
  behind TLS termination (mesh, sidecar, or ingress) you control. Running
  it exposed without TLS is a deployment misconfiguration, not a service
  vulnerability — though we're happy to hear if you think the docs don't
  make this clear enough.
- Non-revocability of access tokens before their TTL expires — a known,
  documented tradeoff (see README → Known limitations).

## Known-sensitive areas for reviewers

- `pkg/jwt` — token minting/verification.
- `internal/grpc/interceptors/auth.go` — bearer token extraction and
  per-request authentication.
- `internal/services/auth/auth.go` — refresh-token rotation and replay
  detection.
- `internal/cache/appcache.go` — app-secret caching; a bug here that
  serves a stale *wrong* secret (not just stale) would be high severity.

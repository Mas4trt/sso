# Contributing to sso

## Before you start

- For anything bigger than a typo fix, open an issue first describing the
  change. Saves everyone a wasted PR when the approach needs discussion.
- Check [`docs/Connecting.md`](docs/Connecting.md) and the README's
  [Security model](README.md#security-model) before touching auth flows —
  token issuance/verification and refresh-token rotation are
  security-sensitive; changes there get closer review.

## Dev setup

```bash
git clone https://github.com/Mas4trt/sso.git
cd sso
docker compose up -d postgres
make migrate-up
cp .env.example .env
make run
```

Requires Go (see `go.mod` for the exact version), Docker, and
[`golang-migrate`](https://github.com/golang-migrate/migrate) CLI for
`make migrate-*`.

## Before opening a PR

```bash
make ci   # lint + unit tests + build — exactly what CI runs
make test-integration   # if you touched internal/storage
```

- `gofmt -w .` — CI fails on unformatted code.
- `golangci-lint run` — see `.golangci.yml` for the enabled linters
  (`govet`, `staticcheck`, `errcheck`, `gosec`, `unused`, `ineffassign`,
  `bodyclose`, `unconvert`, `misspell`). `gosec` findings in `_test.go`
  files are excluded by config; everywhere else, fix or justify with a
  `//nolint:gosec // reason` comment, don't blanket-disable.
- New exported behavior needs a test. Table-driven tests preferred for
  anything with more than 2-3 cases (see `internal/config/config_test.go`
  for the house style).
- Interfaces stay narrow and defined at the *consumer* (see
  `services/auth.UserSaver`/`UserProvider`/etc.) — don't add a method to
  an existing interface unless every implementer needs it.

## Database changes

Migrations are one-way-committed once merged to `main` — never edit a
migration that's already shipped, add a new one:

```bash
make migrate-new NAME=add_something
```

Always provide both `.up.sql` and `.down.sql`, and use `IF NOT EXISTS` /
`IF EXISTS` guards (see existing migrations) so re-running is safe.

## Commit / PR style

- Small, reviewable PRs over one big one.
- Conventional-ish commit subjects (`fix:`, `feat:`, `refactor:`,
  `test:`, `docs:`) are appreciated but not enforced by CI.
- Link the issue the PR addresses.

## Reporting security issues

Do **not** open a public issue. See [`SECURITY.md`](SECURITY.md).

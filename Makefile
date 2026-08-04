CONFIG         ?= ./config/local.yaml
DB_DSN         ?= postgres://sso:sso@localhost:5432/sso?sslmode=disable
MIGRATIONS_DIR := ./migrations
BIN_DIR        := ./bin

# NOTE: kept distinct from proto-update's VERSION=vX.Y.Z argument on purpose
# (that one pins a *dependency* version; this one stamps *this binary*).
APP_VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
COMMIT      ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo unknown)
BUILD_DATE  ?= $(shell date -u +%Y-%m-%dT%H:%M:%SZ)
LDFLAGS     := -s -w -X main.version=$(APP_VERSION) -X main.commit=$(COMMIT) -X main.buildDate=$(BUILD_DATE)

.PHONY: run build test test-unit test-integration lint fmt vet ci \
        migrate-up migrate-down migrate-new \
        docker-up docker-down docker-logs docker-build \
        envoy-validate proto-update new-app

## --- local dev ---

run:
	go run ./cmd/sso --config=$(CONFIG)

build:
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o $(BIN_DIR)/sso ./cmd/sso

## --- quality gates, mirrors .github/workflows/ci.yml ---

fmt:
	gofmt -w .

lint:
	@fmt_out="$$(gofmt -l .)"; \
	if [ -n "$$fmt_out" ]; then \
		echo "not gofmt'ed:"; echo "$$fmt_out"; exit 1; \
	fi
	go vet ./...
	golangci-lint run --timeout=3m

vet:
	go vet ./...

test-unit:
	go test -race -cover -short ./...

# Needs a docker daemon (testcontainers spins up real postgres).
test-integration:
	go test -race -v ./internal/storage/...

test: test-unit test-integration

ci: lint test-unit build

## --- database ---

migrate-up:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_DSN)" up

migrate-down:
	migrate -path $(MIGRATIONS_DIR) -database "$(DB_DSN)" down 1

migrate-new:
	@test -n "$(NAME)" || { echo "usage: make migrate-new NAME=add_something"; exit 1; }
	migrate create -ext sql -dir $(MIGRATIONS_DIR) -seq $(NAME)

## --- docker-compose stack (postgres + migrate + sso) ---

docker-build:
	docker compose build --build-arg VERSION=$(APP_VERSION) --build-arg COMMIT=$(COMMIT) --build-arg BUILD_DATE=$(BUILD_DATE)

docker-up: docker-build
	docker compose up -d

docker-down:
	docker compose down -v

docker-logs:
	docker compose logs -f sso

envoy-validate:
	docker run --rm -v "$$(pwd)/envoy.yaml:/envoy.yaml:ro" envoyproxy/envoy:v1.31-latest --mode validate -c /envoy.yaml

## --- dependency management ---

# Bumps the shared auth.v1 contract to a new tagged version and tidies.
# Usage: make proto-update VERSION=v1.0.0
proto-update:
	@test -n "$(VERSION)" || { echo "usage: make proto-update VERSION=vX.Y.Z"; exit 1; }
	go get github.com/Mas4trt/protos@$(VERSION)
	go mod tidy

## --- onboarding a new consuming application ---

# Every service that authenticates through sso needs its own row in `apps`
# with its own secret (that secret signs *that service's* access tokens —
# services must not share one). This inserts a fresh row and prints the
# application_id + secret to hand to the consuming team.
# Usage: make new-app NAME=billing-service DB_DSN=postgres://...
new-app:
	@test -n "$(NAME)" || { echo "usage: make new-app NAME=billing-service"; exit 1; }
	@SECRET=$$(openssl rand -hex 32); \
	if command -v psql >/dev/null 2>&1; then \
		psql "$(DB_DSN)" -c "INSERT INTO apps (name, secret) VALUES ('$(NAME)', '$$SECRET') RETURNING id, name, secret;"; \
	else \
		docker compose exec -T postgres psql -U sso -d sso -c "INSERT INTO apps (name, secret) VALUES ('$(NAME)', '$$SECRET') RETURNING id, name, secret;"; \
	fi

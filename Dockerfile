# syntax=docker/dockerfile:1

ARG GO_VERSION=1.26-alpine
FROM golang:${GO_VERSION} AS builder

WORKDIR /app

# Cache modules separately from source so `go mod download` only reruns
# when go.mod/go.sum actually change.
COPY go.mod go.sum ./
RUN --mount=type=cache,target=/go/pkg/mod \
    go mod download

COPY . .

ARG VERSION=dev
ARG COMMIT=unknown
ARG BUILD_DATE=unknown

RUN --mount=type=cache,target=/go/pkg/mod \
    --mount=type=cache,target=/root/.cache/go-build \
    CGO_ENABLED=0 GOOS=linux go build \
    -trimpath \
    -ldflags="-s -w \
      -X main.version=${VERSION} \
      -X main.commit=${COMMIT} \
      -X main.buildDate=${BUILD_DATE}" \
    -o /sso-server ./cmd/sso

FROM alpine:3.20

RUN apk add --no-cache ca-certificates && \
    addgroup -S sso && adduser -S sso -G sso

# grpc_health_probe — маленький статический бинарник для docker-compose /
# k8s liveness-readiness проб, т.к. у контейнера нет curl/grpcurl.
ARG GRPC_HEALTH_PROBE_VERSION=v0.4.35
RUN wget -qO/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 \
    && chmod +x /bin/grpc_health_probe

WORKDIR /app

COPY --from=builder --chown=sso:sso /sso-server /app/sso-server
COPY --from=builder --chown=sso:sso /app/migrations /app/migrations
COPY --from=builder --chown=sso:sso /app/config /app/config

USER sso

EXPOSE 44044

HEALTHCHECK --interval=10s --timeout=3s --start-period=5s --retries=5 \
    CMD ["/bin/grpc_health_probe", "-addr=:44044"]

ENTRYPOINT ["/app/sso-server"]
CMD ["--config=/app/config/docker.yaml"]

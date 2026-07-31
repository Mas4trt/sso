FROM golang:1.26-alpine AS builder

WORKDIR /app

COPY go.mod go.sum ./
RUN go mod download

COPY . .
RUN CGO_ENABLED=0 GOOS=linux go build -o /sso-server ./cmd/sso

FROM alpine:3.20

RUN apk add --no-cache ca-certificates

# grpc_health_probe — маленький статический бинарник для docker-compose
# healthcheck, т.к. у контейнера нет curl/grpcurl по умолчанию.
ARG GRPC_HEALTH_PROBE_VERSION=v0.4.35
RUN wget -qO/bin/grpc_health_probe \
    https://github.com/grpc-ecosystem/grpc-health-probe/releases/download/${GRPC_HEALTH_PROBE_VERSION}/grpc_health_probe-linux-amd64 \
    && chmod +x /bin/grpc_health_probe

WORKDIR /app

COPY --from=builder /sso-server /app/sso-server
COPY --from=builder /app/migrations /app/migrations
COPY --from=builder /app/config /app/config

EXPOSE 44044

ENTRYPOINT ["/app/sso-server"]
CMD ["--config=/app/config/docker.yaml"]
# Connecting a service to sso

sso exposes a plain gRPC service (`auth.v1.Auth`, contract in
[`Mas4trt/protos`](https://github.com/Mas4trt/protos)) plus a standard gRPC
health check on the same port. Consuming services do **not** need to import
the `sso` module — only `github.com/Mas4trt/protos` for the generated
client, and `google.golang.org/grpc` to dial.

## 1. Get an `application_id` + `secret`

Every consuming service needs its own row in the `apps` table. The secret
is what signs *that service's* users' access tokens — don't share one
across services.

```bash
make new-app NAME=billing-service DB_DSN=postgres://sso:sso@<host>:5432/sso?sslmode=disable
```

This prints `id`, `name`, `secret`. Keep the secret out of source control —
put it in that service's own secret manager / `.env`.

## 2. Dial sso and call the Auth service

```go
import (
    authv1 "github.com/Mas4trt/protos/gen/go/auth/v1"
    "google.golang.org/grpc"
    "google.golang.org/grpc/credentials/insecure" // swap for real TLS creds outside a trusted network
)

conn, err := grpc.NewClient("sso:44044",
    grpc.WithTransportCredentials(insecure.NewCredentials()),
)
if err != nil {
    // handle
}
defer conn.Close()

client := authv1.NewAuthClient(conn)

resp, err := client.Authenticate(ctx, &authv1.LoginRequest{
    Email:         email,
    Password:      password,
    ApplicationId: yourApplicationID,
})
```

Map gRPC status codes to what the service actually did:

| Code | Meaning |
|---|---|
| `InvalidArgument` | bad email/password, or a required field missing |
| `AlreadyExists` | `Register`: email already taken |
| `Unauthenticated` | `RefreshTokens`: refresh token invalid/expired/revoked — force re-login |
| `NotFound` | unknown `application_id` or `user_id` |
| `Internal` | sso-side failure, safe to retry with backoff |

## 3. Browser clients (grpc-web via Envoy)

Browser-based clients cannot speak plaintext gRPC directly. In that case,
put Envoy in front of sso and use grpc-web over HTTP/1.1:

```text
browser (grpc-web) → Envoy :8080 → sso :44044 (h2c gRPC)
```

The repository already contains a ready-to-use Envoy config in
`envoy.yaml` and a compose service in `docker-compose.yaml`.
For production, keep a separate `envoy.prod.yaml` with TLS termination and
restrictive CORS origins instead of exposing the admin port externally.

## 4. Health checks

sso registers the standard gRPC health service on the same port
(`grpc.health.v1.Health`), used by `grpc_health_probe` in the container
healthcheck. Consuming services can watch it the same way instead of
polling `Authenticate` to check liveness.

## 5. Refresh token rotation

Refresh tokens are single-use — each `RefreshTokens` call revokes the old
token and issues a new pair. If a client uses a refresh token twice
(replay), the second call gets `Unauthenticated`. Don't cache/reuse a
refresh token after exchanging it.

## 6. TLS

The dial example above is intentionally insecure and fine on a private
network or service mesh. Before exposing sso beyond that, terminate TLS
(either at sso itself or at a sidecar/ingress) and swap
`insecure.NewCredentials()` for real transport credentials on the client
side.

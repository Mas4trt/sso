package interceptors

import (
	"context"
	"errors"
	"strings"

	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/metadata"
	"google.golang.org/grpc/status"

	"sso/internal/domain/models"
	"sso/pkg/jwt"
)

type ctxKey int

const uidCtxKey ctxKey = 0

// AppSecretLookup resolves an app's signing secret by app_id. Satisfied by
// postgres.Storage / authsvc.AppProvider; declared narrowly here to avoid
// pulling in internal/services/auth.
type AppSecretLookup interface {
	App(ctx context.Context, appID uint64) (models.App, error)
}

// NewAuthInterceptor rejects calls to the listed methods unless they carry a
// valid "authorization: Bearer <access_token>" header, and injects the
// authenticated user id for handlers via UIDFromContext. Methods not listed
// pass through unauthenticated by design (Register/Authenticate are public).
func NewAuthInterceptor(apps AppSecretLookup, methods ...string) grpc.UnaryServerInterceptor {
	protected := make(map[string]bool, len(methods))
	for _, m := range methods {
		protected[m] = true
	}

	return func(ctx context.Context, req interface{}, info *grpc.UnaryServerInfo, handler grpc.UnaryHandler) (interface{}, error) {
		if !protected[info.FullMethod] {
			return handler(ctx, req)
		}

		token, err := bearerToken(ctx)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "missing bearer token")
		}

		appID, err := jwt.ParseUnverifiedAppID(token)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}

		app, err := apps.App(ctx, appID)
		if err != nil {
			// Don't distinguish "unknown app" from "bad token" — both just
			// mean this caller isn't authenticated for this call.
			return nil, status.Error(codes.Unauthenticated, "invalid access token")
		}

		uid, err := jwt.VerifyAccessToken(token, app.Secret)
		if err != nil {
			return nil, status.Error(codes.Unauthenticated, "invalid or expired access token")
		}

		return handler(context.WithValue(ctx, uidCtxKey, uid), req)
	}
}

func UIDFromContext(ctx context.Context) (uint64, bool) {
	uid, ok := ctx.Value(uidCtxKey).(uint64)
	return uid, ok
}

func bearerToken(ctx context.Context) (string, error) {
	md, ok := metadata.FromIncomingContext(ctx)
	if !ok {
		return "", errors.New("no metadata")
	}
	vals := md.Get("authorization")
	if len(vals) == 0 {
		return "", errors.New("no authorization header")
	}
	const prefix = "Bearer "
	if !strings.HasPrefix(vals[0], prefix) {
		return "", errors.New("malformed authorization header")
	}
	return strings.TrimPrefix(vals[0], prefix), nil
}

package app

import (
	"context"
	"log/slog"
	"time"

	grpcapp "sso/internal/app/grpc"
	authgrpc "sso/internal/grpc/auth"
	authsvc "sso/internal/services/auth"
	"sso/internal/storage/postgres"
)

type App struct {
	GRPCSrv *grpcapp.App
	storage *postgres.Storage
}

func New(
	ctx context.Context,
	log *slog.Logger,
	grpcPort int,
	storageDSN string,
	tokenTTL time.Duration,
	RefreshTTL time.Duration,
) (*App, error) {
	storage, err := postgres.New(ctx, storageDSN)
	if err != nil {
		return nil, err
	}

	authService := authsvc.New(log, storage, storage, storage, storage, tokenTTL, RefreshTTL)

	grpcApp := grpcapp.New(log, toGRPCAuthService(authService), grpcPort)

	return &App{
		GRPCSrv: grpcApp,
		storage: storage,
	}, nil
}

func toGRPCAuthService(a *authsvc.Auth) authgrpc.AuthService {
	return a
}

func (a *App) Close() {
	a.storage.Close()
}

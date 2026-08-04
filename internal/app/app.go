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

	grpcApp := grpcapp.New(log, toGRPCAuthService(authService), storage, grpcPort)

	return &App{
		GRPCSrv: grpcApp,
		storage: storage,
	}, nil
}

func toGRPCAuthService(a *authsvc.Auth) authgrpc.AuthService {
	return a
}

func (a *App) Run(ctx context.Context) error {
	if a == nil || a.GRPCSrv == nil {
		return nil
	}

	return a.GRPCSrv.Run(ctx)
}

func (a *App) Close() error {
	if a == nil {
		return nil
	}
	if a.storage != nil {
		a.storage.Close()
	}
	return nil
}

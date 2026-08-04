package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"sso/internal/app"
	"sso/internal/config"
)

const (
	envLocal = "local"
	envDev   = "dev"
	envProd  = "prod"
)

func main() {
	cfg := config.MustLoad()

	log := setupLogger(cfg.Env)
	log.Info("starting application", slog.String("env", cfg.Env))

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	application, err := app.New(ctx, log, cfg.GRPC.Port, cfg.Storage.DSN, cfg.Token.TTL, cfg.Token.RefreshTTL)
	if err != nil {
		log.Error("failed to init app", slog.Any("error", err))
		os.Exit(1)
	}

	if err := application.Run(ctx); err != nil {
		log.Error("grpc server stopped with error", slog.Any("error", err))
		os.Exit(1)
	}

	log.Info("shutting down application")
	if err := application.Close(); err != nil {
		log.Error("failed to close application resources", slog.Any("error", err))
	}

	log.Info("application stopped")
}

func setupLogger(env string) *slog.Logger {
	switch env {
	case envLocal:
		return slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envDev:
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelDebug}))
	case envProd:
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	default:
		// fail-safe: не паникуем на nil-логгере
		return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	}
}

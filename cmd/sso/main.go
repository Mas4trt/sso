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

// Set at build time via -ldflags "-X main.version=... -X main.commit=... -X main.buildDate=...".
// See Dockerfile. Left as "dev"/"unknown" for `go run`/local builds.
var (
	version   = "dev"
	commit    = "unknown"
	buildDate = "unknown"
)

func main() {
	cfg := config.MustLoad()

	log := setupLogger(cfg.Env)
	log.Info("starting application",
		slog.String("env", cfg.Env),
		slog.String("version", version),
		slog.String("commit", commit),
		slog.String("build_date", buildDate),
	)

	ctx, cancel := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer cancel()

	application, err := app.New(ctx, log, cfg.GRPC.Port, cfg.Storage.DSN, cfg.Token.TTL, cfg.Token.RefreshTTL)
	if err != nil {
		log.Error("failed to init app", slog.Any("error", err))
		os.Exit(1)
	}

	runErr := application.Run(ctx)

	log.Info("shutting down application")
	if closeErr := application.Close(); closeErr != nil {
		log.Error("failed to close application resources", slog.Any("error", closeErr))
	}

	if runErr != nil {
		log.Error("grpc server stopped with error", slog.Any("error", runErr))
		os.Exit(1)
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

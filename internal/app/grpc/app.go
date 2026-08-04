package grpcapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"

	authgrpc "sso/internal/grpc/auth"
	"sso/internal/grpc/interceptors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

type App struct {
	log        *slog.Logger
	gRPCServer *grpc.Server
	healthSrv  *health.Server
	port       int
}

func New(log *slog.Logger, authService authgrpc.AuthService, port int) *App {
	gRPCServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.Recovery(log),
			interceptors.Logging(log),
		),
	)

	authgrpc.Register(gRPCServer, authService)

	healthSrv := health.NewServer()
	healthpb.RegisterHealthServer(gRPCServer, healthSrv)
	healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_SERVING)

	return &App{
		log:        log,
		gRPCServer: gRPCServer,
		healthSrv:  healthSrv,
		port:       port,
	}
}

func (a *App) MustRun(ctx context.Context) {
	if err := a.Run(ctx); err != nil {
		panic(err)
	}
}

func (a *App) Run(ctx context.Context) error {
	const op = "grpcapp.Run"
	log := a.log.With(slog.String("op", op), slog.Int("port", a.port))

	l, err := net.Listen("tcp", fmt.Sprintf(":%d", a.port))
	if err != nil {
		return fmt.Errorf("%s: %w", op, err)
	}

	log.Info("grpc server is running", slog.String("addr", l.Addr().String()))

	go func() {
		<-ctx.Done()
		a.Stop()
	}()

	if err := a.gRPCServer.Serve(l); err != nil {
		if errors.Is(err, grpc.ErrServerStopped) {
			return nil
		}
		return fmt.Errorf("%s: %w", op, err)
	}

	return nil
}

func (a *App) Stop() {
	a.log.With(slog.String("op", "grpcapp.Stop")).Info("stopping grpc server", slog.Int("port", a.port))

	// Сначала помечаем сервис NOT_SERVING
	a.healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	a.gRPCServer.GracefulStop()
}

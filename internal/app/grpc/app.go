package grpcapp

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	authgrpc "sso/internal/grpc/auth"
	"sso/internal/grpc/interceptors"

	"google.golang.org/grpc"
	"google.golang.org/grpc/health"
	healthpb "google.golang.org/grpc/health/grpc_health_v1"
)

const (
	authRateLimit   = 1.0 // запросов/сек с одного peer IP
	authRateBurst   = 5.0
	limiterEntryTTL = 10 * time.Minute

	// ShutdownTimeout bounds how long GracefulStop waits for in-flight RPCs
	// to finish before we fall back to a hard Stop(). Without this, a
	// single stuck stream (client that stopped reading, broken network
	// path, etc.) can hang container termination indefinitely — which in
	// k8s means an eventual SIGKILL after the pod's terminationGracePeriod,
	// but with no logs explaining why the shutdown never completed.
	ShutdownTimeout = 20 * time.Second
)

type App struct {
	log        *slog.Logger
	gRPCServer *grpc.Server
	healthSrv  *health.Server
	port       int
}

func New(log *slog.Logger, authService authgrpc.AuthService, apps interceptors.AppSecretLookup, port int) *App {
	authLimiter := interceptors.NewMethodRateLimiter(
		authRateLimit, authRateBurst, limiterEntryTTL,
		"/auth.v1.Auth/Register",
		"/auth.v1.Auth/Authenticate",
	)

	authInterceptor := interceptors.NewAuthInterceptor(apps,
		"/auth.v1.Auth/GetRole",
	)

	gRPCServer := grpc.NewServer(
		grpc.ChainUnaryInterceptor(
			interceptors.Recovery(log),
			authLimiter.Unary(),
			authInterceptor,
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

// Stop marks the service NOT_SERVING (so a load balancer's health check
// starts routing away before we stop accepting work), then waits up to
// ShutdownTimeout for in-flight RPCs to drain before forcing a hard stop.
func (a *App) Stop() {
	log := a.log.With(slog.String("op", "grpcapp.Stop"))
	log.Info("stopping grpc server", slog.Int("port", a.port), slog.Duration("timeout", ShutdownTimeout))

	a.healthSrv.SetServingStatus("", healthpb.HealthCheckResponse_NOT_SERVING)

	stopped := make(chan struct{})
	go func() {
		a.gRPCServer.GracefulStop()
		close(stopped)
	}()

	select {
	case <-stopped:
		log.Info("grpc server stopped gracefully")
	case <-time.After(ShutdownTimeout):
		log.Warn("graceful shutdown timed out, forcing stop")
		a.gRPCServer.Stop()
	}
}

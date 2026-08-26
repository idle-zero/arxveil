package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"

	"github.com/idle-zero/arxveil/server/internal/agentgrpc"
	"github.com/idle-zero/arxveil/server/internal/config"
	"github.com/idle-zero/arxveil/server/internal/enrollment"
	agentv1 "github.com/idle-zero/arxveil/server/internal/gen/agent/v1"
	"github.com/idle-zero/arxveil/server/internal/httpapi"
	"github.com/idle-zero/arxveil/server/internal/presence"
	"github.com/idle-zero/arxveil/server/internal/repository/postgres"
	"github.com/idle-zero/arxveil/server/internal/telemetry"
	"github.com/jackc/pgx/v5/pgxpool"
	"google.golang.org/grpc"
)

const shutdownTimeout = 10 * time.Second

type application struct {
	logger       *slog.Logger
	httpServer   *http.Server
	grpcServer   *grpc.Server
	grpcListener net.Listener
	httpAddress  string
	grpcAddress  string
}

func newApplication(cfg config.Config, database *pgxpool.Pool, logger *slog.Logger) (*application, error) {
	enrollmentStore := postgres.NewEnrollmentStore(database)
	enrollmentService := enrollment.New(enrollmentStore)

	presenceStore := postgres.NewPresenceStore(database)
	presenceService := presence.New(presenceStore)

	telemetryStore := postgres.NewTelemetryStore(database)
	telemetryService := telemetry.New(telemetryStore)

	grpcListener, err := net.Listen("tcp", cfg.GRPCAddress)
	if err != nil {
		return nil, fmt.Errorf("listen for gRPC: %w", err)
	}

	grpcServer := grpc.NewServer()
	agentv1.RegisterAgentServiceServer(
		grpcServer,
		agentgrpc.NewServer(
			enrollmentService,
			presenceService,
			telemetryService,
			logger,
		),
	)

	return &application{
		logger: logger,
		httpServer: &http.Server{
			Addr:              cfg.ServerAddress,
			Handler:           httpapi.NewRouter(database),
			ReadHeaderTimeout: 5 * time.Second,
		},
		grpcServer:   grpcServer,
		grpcListener: grpcListener,
		httpAddress:  cfg.ServerAddress,
		grpcAddress:  cfg.GRPCAddress,
	}, nil
}

func (a *application) serve(ctx context.Context) error {
	serverErrors := make(chan error, 2)

	go a.serveHTTP(serverErrors)
	go a.serveGRPC(serverErrors)

	var serveErr error
	select {
	case <-ctx.Done():
		a.logger.Info("shutdown signal received")
	case serveErr = <-serverErrors:
	}

	if err := a.shutdown(); err != nil {
		return err
	}
	if serveErr != nil {
		return fmt.Errorf("serve application: %w", serveErr)
	}

	return nil
}

func (a *application) serveHTTP(serverErrors chan<- error) {
	a.logger.Info("HTTP server started", "address", a.httpAddress)
	if err := a.httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
		serverErrors <- fmt.Errorf("serve HTTP: %w", err)
	}
}

func (a *application) serveGRPC(serverErrors chan<- error) {
	a.logger.Info("gRPC server started", "address", a.grpcAddress)
	if err := a.grpcServer.Serve(a.grpcListener); err != nil {
		serverErrors <- fmt.Errorf("serve gRPC: %w", err)
	}
}

func (a *application) shutdown() error {
	ctx, cancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer cancel()

	if err := a.httpServer.Shutdown(ctx); err != nil {
		return fmt.Errorf("shut down HTTP server: %w", err)
	}
	gracefulStopGRPC(a.grpcServer, ctx)

	return nil
}

func gracefulStopGRPC(server *grpc.Server, ctx context.Context) {
	done := make(chan struct{})
	go func() {
		server.GracefulStop()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		server.Stop()
		<-done
	}
}

// Command server is the single binary for dynoconf. It serves the HTTP UI/REST
// API (and embedded frontend) on HTTP_ADDR and the gRPC ConfigStream API on
// GRPC_ADDR. Run with the "migrate" argument to apply migrations and exit.
package main

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5"
	"google.golang.org/grpc"

	"github.com/dynoconf/dynoconf/internal/audit"
	"github.com/dynoconf/dynoconf/internal/auth"
	"github.com/dynoconf/dynoconf/internal/config"
	"github.com/dynoconf/dynoconf/internal/events"
	"github.com/dynoconf/dynoconf/internal/grpcserver"
	pb "github.com/dynoconf/dynoconf/internal/grpcserver/configpb"
	"github.com/dynoconf/dynoconf/internal/httpserver"
	"github.com/dynoconf/dynoconf/internal/migrate"
	"github.com/dynoconf/dynoconf/internal/store"
	"github.com/dynoconf/dynoconf/web"
)

func main() {
	log := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(log)

	if err := run(log); err != nil {
		log.Error("fatal", "err", err)
		os.Exit(1)
	}
}

func run(log *slog.Logger) error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}

	// `server migrate` applies migrations and exits.
	if len(os.Args) > 1 && os.Args[1] == "migrate" {
		log.Info("applying migrations")
		if err := migrate.Up(cfg.DatabaseURL); err != nil {
			return err
		}
		log.Info("migrations applied")
		return nil
	}

	rootCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	// Apply migrations on startup (idempotent).
	if err := migrate.Up(cfg.DatabaseURL); err != nil {
		return fmt.Errorf("migrate: %w", err)
	}

	st, err := store.New(rootCtx, cfg.DatabaseURL)
	if err != nil {
		return err
	}
	defer st.Close()

	auditor := audit.New(st, log)

	// Events broker: a dedicated connection for LISTEN.
	broker := events.NewBroker(func(ctx context.Context) (*pgx.Conn, error) {
		return pgx.Connect(ctx, cfg.DatabaseURL)
	}, log)
	go broker.Run(rootCtx)

	replicaID := makeReplicaID()
	log.Info("starting", "contour", cfg.ContourName, "replica", replicaID)

	tracker := grpcserver.NewConnTracker(replicaID, st, broker, log)
	go tracker.Run(rootCtx)

	authn, err := auth.New(rootCtx, cfg, st, log)
	if err != nil {
		return err
	}

	// --- gRPC server ---
	grpcSrv := grpc.NewServer()
	pb.RegisterConfigStreamServer(grpcSrv, grpcserver.New(st, broker, tracker, log))
	grpcLis, err := net.Listen("tcp", cfg.GRPCAddr)
	if err != nil {
		return fmt.Errorf("grpc listen: %w", err)
	}
	grpcErr := make(chan error, 1)
	go func() {
		log.Info("gRPC listening", "addr", cfg.GRPCAddr)
		grpcErr <- grpcSrv.Serve(grpcLis)
	}()

	// --- HTTP server ---
	staticFS, err := web.DistFS()
	if err != nil {
		return fmt.Errorf("embed frontend: %w", err)
	}
	httpSrv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           httpserver.New(cfg, st, authn, broker, auditor, staticFS, log).Handler(),
		ReadHeaderTimeout: 10 * time.Second,
	}
	httpErr := make(chan error, 1)
	go func() {
		log.Info("HTTP listening", "addr", cfg.HTTPAddr)
		httpErr <- httpSrv.ListenAndServe()
	}()

	// --- wait for shutdown or fatal server error ---
	select {
	case <-rootCtx.Done():
		log.Info("shutdown signal received, draining")
	case err := <-grpcErr:
		if err != nil {
			return fmt.Errorf("grpc serve: %w", err)
		}
	case err := <-httpErr:
		if err != nil && !errors.Is(err, http.ErrServerClosed) {
			return fmt.Errorf("http serve: %w", err)
		}
	}

	// Graceful shutdown.
	stop() // stop receiving more signals; cancels rootCtx-derived goroutines

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	done := make(chan struct{})
	go func() {
		grpcSrv.GracefulStop() // drains active streams
		close(done)
	}()
	select {
	case <-done:
	case <-shutdownCtx.Done():
		log.Warn("grpc graceful stop timed out, forcing")
		grpcSrv.Stop()
	}

	if err := httpSrv.Shutdown(shutdownCtx); err != nil {
		log.Warn("http shutdown error", "err", err)
	}

	log.Info("stopped")
	return nil
}

func makeReplicaID() string {
	host, _ := os.Hostname()
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return host + "-" + hex.EncodeToString(b)
}

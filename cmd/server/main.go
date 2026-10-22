package main

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/loducode/wompi-gateway/internal/config"
	"github.com/loducode/wompi-gateway/internal/dispatcher"
	"github.com/loducode/wompi-gateway/internal/domain"
	gatewayhttp "github.com/loducode/wompi-gateway/internal/http"
	"github.com/loducode/wompi-gateway/internal/repository/memory"
	"github.com/loducode/wompi-gateway/internal/repository/postgres"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("failed to load configuration", "error", err)
		os.Exit(1)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	var repo domain.Repository
	if cfg.DatabaseURL != "" {
		pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
		if err != nil {
			logger.Error("failed to create PostgreSQL connection pool", "error", err)
			os.Exit(1)
		}
		defer pool.Close()

		if err := pool.Ping(ctx); err != nil {
			logger.Error("failed to ping PostgreSQL database", "error", err)
			os.Exit(1)
		}

		pgRepo := postgres.New(pool)
		if cfg.AutoMigrate {
			if err := pgRepo.Migrate(ctx); err != nil {
				logger.Error("failed to run database migrations", "error", err)
				os.Exit(1)
			}
			logger.Info("PostgreSQL schema migrations applied")
		}
		repo = pgRepo
	} else {
		logger.Warn("DATABASE_URL not set; running with in-memory repository (data will not persist across restarts)")
		repo = memory.New()
	}

	disp := dispatcher.New(repo, dispatcher.Config{
		Concurrency:    cfg.WorkerConcurrency,
		PollInterval:   cfg.WorkerPollInterval,
		DefaultTimeout: cfg.ForwardTimeout,
		Logger:         logger,
	})

	go disp.Start(ctx)

	router := gatewayhttp.NewRouter(repo, disp, cfg.AdminAPIKey, cfg.GinMode)
	srv := &http.Server{
		Addr:              ":" + cfg.Port,
		Handler:           router,
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       30 * time.Second,
		WriteTimeout:      30 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	go func() {
		logger.Info("Wompi Webhook Gateway listening", "addr", srv.Addr, "admin_path", "/admin")
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Error("HTTP server failed", "error", err)
			os.Exit(1)
		}
	}()

	<-ctx.Done()
	logger.Info("shutdown signal received, draining connections...")

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful shutdown failed", "error", err)
	}
	logger.Info("server stopped cleanly")
}

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
	_ "github.com/joho/godotenv/autoload"

	"dynamic-reminder/internal/config"
	"dynamic-reminder/internal/handler"
	"dynamic-reminder/internal/repository"
	"dynamic-reminder/internal/service"
)

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo}))
	slog.SetDefault(logger)

	cfg, err := config.Load()
	if err != nil {
		logger.Error("config load failed", "err", err)
		os.Exit(1)
	}

	// signal.NotifyContext gives us the canonical "cancel on Ctrl-C / SIGTERM"
	// context. Every downstream goroutine derives from it so shutdown is a
	// single point of truth.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	pool, err := pgxpool.New(ctx, cfg.DatabaseURL)
	if err != nil {
		logger.Error("db connect failed", "err", err)
		os.Exit(1)
	}
	defer pool.Close()

	pingCtx, cancelPing := context.WithTimeout(ctx, 5*time.Second)
	if err := pool.Ping(pingCtx); err != nil {
		cancelPing()
		logger.Error("db ping failed", "err", err)
		os.Exit(1)
	}
	cancelPing()
	logger.Info("connected to database")

	rules := service.NewRuleService(pool)
	audit := service.NewAuditService(pool)
	taskRepo := repository.NewTaskRepository(pool)

	scheduler := service.NewScheduler(pool, service.SchedulerOptions{
		TickInterval: cfg.SchedulerTick,
		Workers:      cfg.SchedulerWorkers,
		QueueSize:    cfg.SchedulerQueueSize,
		Logger:       logger,
	})
	//"When scheduler completely stopped"
	schedDone := make(chan struct{})
	go func() {
		//This acts like a signal:
		defer close(schedDone)
		scheduler.Run(ctx)
	}()

	srv := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           handler.NewRouter(rules, audit, taskRepo, logger),
		ReadHeaderTimeout: 5 * time.Second,
	}

	srvErr := make(chan error, 1)
	go func() {
		logger.Info("http server listening", "addr", cfg.HTTPAddr)
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			srvErr <- err
		}
	}()

	select {
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	case err := <-srvErr:
		logger.Error("http server crashed", "err", err)
	}

	// Cancel the root context so the scheduler begins draining. Without this,
	// a server crash (e.g. port already in use) never cancels ctx, and the
	// `<-schedDone` wait below would block the process forever.
	stop()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("graceful http shutdown failed", "err", err)
	}

	<-schedDone
	logger.Info("shutdown complete")
}

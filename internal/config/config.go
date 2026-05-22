package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

// Config is loaded once at process start from environment variables.
type Config struct {
	DatabaseURL        string
	HTTPAddr           string
	SchedulerTick      time.Duration
	SchedulerWorkers   int
	SchedulerQueueSize int
	ShutdownTimeout    time.Duration
}

func Load() (Config, error) {
	cfg := Config{
		DatabaseURL: env("DATABASE_URL", ""),
		HTTPAddr:    env("HTTP_ADDR", ":8080"),
	}
	if cfg.DatabaseURL == "" {
		return cfg, fmt.Errorf("DATABASE_URL is required")
	}

	tick, err := time.ParseDuration(env("SCHEDULER_TICK", "30s"))
	if err != nil || tick <= 0 {
		return cfg, fmt.Errorf("invalid SCHEDULER_TICK")
	}
	cfg.SchedulerTick = tick

	workers, err := strconv.Atoi(env("SCHEDULER_WORKERS", "5"))
	if err != nil || workers < 1 {
		return cfg, fmt.Errorf("invalid SCHEDULER_WORKERS")
	}
	cfg.SchedulerWorkers = workers

	queue, err := strconv.Atoi(env("SCHEDULER_QUEUE_SIZE", "100"))
	if err != nil || queue < 1 {
		return cfg, fmt.Errorf("invalid SCHEDULER_QUEUE_SIZE")
	}
	cfg.SchedulerQueueSize = queue

	shutdown, err := time.ParseDuration(env("SHUTDOWN_TIMEOUT", "15s"))
	if err != nil || shutdown <= 0 {
		return cfg, fmt.Errorf("invalid SHUTDOWN_TIMEOUT")
	}
	cfg.ShutdownTimeout = shutdown

	return cfg, nil
}

func env(key, def string) string {
	if v, ok := os.LookupEnv(key); ok && v != "" {
		return v
	}
	return def
}

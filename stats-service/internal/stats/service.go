package stats

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/redis/go-redis/v9"
	"github.com/segmentio/kafka-go"
)

func env(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func run() error {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTelemetry, err := setupTelemetry(ctx)
	if err != nil {
		slog.Warn("Falha ao configurar telemetria", "error", err)
	}
	if shutdownTelemetry != nil {
		defer func() {
			flushCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
			defer cancel()
			_ = shutdownTelemetry(flushCtx)
		}()
	}
	config, err := pgxpool.ParseConfig(env("DATABASE_URL", "postgresql://stats_user:stats_pass@localhost:5432/stats"))
	if err != nil {
		return err
	}
	config.MaxConns, config.MinConns = 16, 2
	config.ConnConfig.ConnectTimeout = 3 * time.Second
	pool, err := pgxpool.NewWithConfig(ctx, config)
	if err != nil {
		return err
	}
	defer pool.Close()
	repo := &PostgresRepository{pool}
	migrateCtx, cancel := context.WithTimeout(ctx, 5*time.Minute)
	err = repo.Migrate(migrateCtx)
	cancel()
	if err != nil {
		return fmt.Errorf("migração: %w", err)
	}
	options, err := redis.ParseURL(env("REDIS_URL", "redis://localhost:6379"))
	if err != nil {
		return err
	}
	options.MaxRetries = -1
	options.DialTimeout = cacheTimeout
	options.ReadTimeout, options.WriteTimeout, options.PoolTimeout = cacheTimeout, cacheTimeout, cacheTimeout
	options.ContextTimeoutEnabled = true
	options.PoolSize, options.MinIdleConns = 16, 1
	client := redis.NewClient(options)
	defer func() { _ = client.Close() }()
	cache := &RedisCache{client}
	reader := kafka.NewReader(kafka.ReaderConfig{
		Brokers: strings.Split(env("KAFKA_BROKERS", "localhost:29092"), ","),
		Topic:   env("KAFKA_TOPIC_LINK_CLICKED", "link.clicked"), GroupID: env("KAFKA_GROUP_ID", "stats-service"),
		StartOffset: kafka.FirstOffset, MinBytes: 1, MaxBytes: 1 << 20,
		MaxWait: 100 * time.Millisecond, QueueCapacity: 512, CommitInterval: 0,
	})
	consumerDone := make(chan struct{})
	go func() { defer close(consumerDone); consume(ctx, reader, repo, cache) }()
	defer func() { stop(); _ = reader.Close(); <-consumerDone }()
	server := &http.Server{Addr: ":" + env("PORT", "8000"), Handler: newHandler(repo, cache),
		ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 5 * time.Second,
		IdleTimeout: 60 * time.Second, MaxHeaderBytes: 64 << 10}
	serverErr := make(chan error, 1)
	go func() { serverErr <- server.ListenAndServe() }()
	slog.InfoContext(ctx, "stats-service Go ouvindo", "port", env("PORT", "8000"))
	select {
	case <-ctx.Done():
	case err := <-serverErr:
		if !errors.Is(err, http.ErrServerClosed) {
			return err
		}
	}
	shutdownCtx, cancelShutdown := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancelShutdown()
	return server.Shutdown(shutdownCtx)
}

// Main inicializa o serviço ou executa o healthcheck do binário.
func Main() {
	if len(os.Args) > 1 && os.Args[1] == "healthcheck" {
		client := http.Client{Timeout: 2 * time.Second}
		response, err := client.Get("http://127.0.0.1:" + env("PORT", "8000") + "/health")
		if err != nil {
			os.Exit(1)
		}
		_ = response.Body.Close()
		if response.StatusCode != 200 {
			os.Exit(1)
		}
		return
	}
	if err := run(); err != nil {
		slog.Error("stats-service encerrou com erro", "error", err)
		os.Exit(1)
	}
}

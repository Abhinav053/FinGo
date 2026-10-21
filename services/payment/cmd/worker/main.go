package main

import (
	"context"
	"os"
	"os/signal"
	"syscall"
	"time"

	_ "github.com/jackc/pgx/v5/stdlib"
	"github.com/joho/godotenv"
	"github.com/rs/zerolog"
	"github.com/rs/zerolog/log"

	"github.com/Abhinav053/FinGo/services/payment/internal/config"
	"github.com/Abhinav053/FinGo/services/payment/internal/queue"
	"github.com/Abhinav053/FinGo/services/payment/internal/store"
)

func main() {
	_ = godotenv.Load()
	cfg, err := config.Load()
	if err != nil {
		log.Fatal().Err(err).Msg("failed to load configuration")
	}

	logger := buildLogger(cfg)
	if cfg.RabbitMQURL == "" {
		log.Fatal().Msg("RABBITMQ_URL is required by the payment worker")
	}
	db, err := store.Open(cfg.DatabaseURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to database")
	}
	defer db.Close()
	broker, err := queue.Connect(cfg.RabbitMQURL)
	if err != nil {
		log.Fatal().Err(err).Msg("failed to connect to RabbitMQ")
	}
	defer broker.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGINT, syscall.SIGTERM)
	done := make(chan error, 2)

	go func() {
		ticker := time.NewTicker(250 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ctx.Done():
				done <- nil
				return
			case <-ticker.C:
				for {
					published, err := store.PublishNext(ctx, db, broker.Publish)
					if err != nil {
						logger.Error().Err(err).Msg("failed to process queued payment")
						break
					}
					if !published {
						break
					}
				}
			}
		}
	}()
	go func() {
		err := broker.Consume(ctx, func(ctx context.Context, event queue.Event) error {
			return store.ProcessEvent(ctx, db, event, cfg.ProviderDefaultOutcome, cfg.ProviderReconcileOutcome, time.Duration(cfg.ProviderTimeoutMs)*time.Millisecond)
		})
		if err != nil && ctx.Err() == nil {
			logger.Error().Err(err).Msg("RabbitMQ consumer stopped")
		}
		done <- err
	}()

	logger.Info().Msg("payment-worker running")
	<-quit
	cancel()
	shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer shutdownCancel()
	for i := 0; i < 2; i++ {
		select {
		case <-done:
		case <-shutdownCtx.Done():
			logger.Error().Msg("worker shutdown timed out")
			i = 2
		}
	}
}

func buildLogger(cfg *config.Config) zerolog.Logger {
	level, err := zerolog.ParseLevel(cfg.LogLevel)
	if err != nil {
		level = zerolog.InfoLevel
	}
	if cfg.Env == "development" {
		return zerolog.New(zerolog.ConsoleWriter{Out: os.Stderr, TimeFormat: time.RFC3339}).
			Level(level).With().Timestamp().Str("service", "payment-worker").Logger()
	}
	return zerolog.New(os.Stderr).Level(level).With().Timestamp().Str("service", "payment-worker").Logger()
}

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

	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/cache"
	"github.com/theRiddler9/OrderFlow/internal/config"
	"github.com/theRiddler9/OrderFlow/internal/database"
	"github.com/theRiddler9/OrderFlow/internal/httpapi"
	"github.com/theRiddler9/OrderFlow/internal/metrics"
	"github.com/theRiddler9/OrderFlow/internal/reservation"
	"github.com/theRiddler9/OrderFlow/internal/tracing"
)

func main() {
	logger := slog.New(slog.NewJSONHandler(os.Stdout, nil))

	if err := run(logger); err != nil {
		logger.Error("API stopped", "error", err)
		os.Exit(1)
	}
}

func run(logger *slog.Logger) error {
	configuration, err := config.LoadAPI()
	if err != nil {
		return err
	}

	tokenManager, err := auth.NewTokenManager(
		configuration.JWTSecret,
		configuration.JWTIssuer,
		configuration.JWTAudience,
		time.Duration(configuration.JWTAccessTokenMinutes)*time.Minute,
	)
	if err != nil {
		return err
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	if configuration.TracingEnabled && configuration.OTLPEndpoint != "" {
		shutdownTracer, err := tracing.Init(ctx, "orderflow-api", configuration.OTLPEndpoint)
		if err != nil {
			logger.Warn("distributed tracing initialization failed, falling back to no-op", "error", err)
		} else {
			defer func() {
				shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				defer cancel()
				_ = shutdownTracer(shutdownCtx)
			}()
			logger.Info("distributed tracing initialized", "endpoint", configuration.OTLPEndpoint)
		}
	} else {
		_, _ = tracing.Init(ctx, "orderflow-api", "")
	}

	databasePool, err := database.Connect(ctx, configuration.DatabaseURL, configuration.DatabaseTimeout)
	if err != nil {
		return err
	}
	defer databasePool.Close()

	reservationService := reservation.NewService(databasePool, configuration.ReservationHold).
		WithFeatureFlags(configuration.CacheEnabled, configuration.SoldOutFastPath)
	authService := auth.NewService(auth.NewPostgresStore(databasePool), tokenManager)

	var redisClient *cache.Client
	if configuration.RedisAddress != "" {
		rc, err := cache.New(configuration.RedisAddress, configuration.RedisPassword, configuration.RedisTimeout, logger)
		if err != nil {
			logger.Warn("redis connection unavailable, rate limiting and sold-out cache will degrade gracefully", "error", err)
		} else {
			redisClient = rc
			defer redisClient.Close()
			reservationService.WithCache(redisClient, configuration.CacheSoldOutTTL)
		}
	}

	metricsCollector := metrics.New(nil)
	metricsCollector.RegisterPoolStats(databasePool, "api")

	api := httpapi.NewServer(
		databasePool,
		reservationService,
		authService,
		redisClient,
		configuration.RateLimitLoginPerMinute,
		configuration.RateLimitReservationPerMinute,
		logger,
	).WithRateLimitEnabled(configuration.RateLimitEnabled).
		WithMetrics(metricsCollector)
	httpServer := &http.Server{
		Addr:              configuration.HTTPAddress,
		Handler:           api.Handler(),
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       15 * time.Second,
		WriteTimeout:      15 * time.Second,
		IdleTimeout:       60 * time.Second,
	}

	serverErrors := make(chan error, 1)
	go func() {
		logger.Info("API started", "address", configuration.HTTPAddress, "environment", configuration.Environment)
		serverErrors <- httpServer.ListenAndServe()
	}()

	select {
	case err := <-serverErrors:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		logger.Info("shutdown signal received")
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return err
	}

	if err := <-serverErrors; !errors.Is(err, http.ErrServerClosed) {
		return err
	}

	return nil
}

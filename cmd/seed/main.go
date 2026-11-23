package main

import (
	"context"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/database"
	"github.com/theRiddler9/OrderFlow/internal/seed"
)

type pgxUserStore struct {
	pool *pgxpool.Pool
}

func (s *pgxUserStore) UpsertUser(ctx context.Context, id, email, passwordHash string) (string, error) {
	const query = `
		INSERT INTO users (id, email, password_hash)
		VALUES ($1, $2, $3)
		ON CONFLICT (email) DO UPDATE SET email = EXCLUDED.email
		RETURNING id`
	var actualID string
	err := s.pool.QueryRow(ctx, query, id, email, passwordHash).Scan(&actualID)
	return actualID, err
}

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))

	count := flag.Int("n", 1000, "number of test users to seed")
	outputPath := flag.String("out", "loadtest/tokens.json", "output path for generated tokens JSON")
	duration := flag.Duration("duration", 24*time.Hour, "validity duration for generated tokens")
	dbURL := flag.String("database-url", "", "PostgreSQL database connection URL (defaults to DATABASE_URL)")
	jwtSecret := flag.String("jwt-secret", "", "JWT signing secret (defaults to JWT_SECRET)")
	jwtIssuer := flag.String("jwt-issuer", "orderflow", "JWT issuer claim")
	jwtAudience := flag.String("jwt-audience", "orderflow-api", "JWT audience claim")
	flag.Parse()

	databaseURL := *dbURL
	if databaseURL == "" {
		databaseURL = os.Getenv("DATABASE_URL")
	}
	if databaseURL == "" {
		logger.Error("database URL must be provided via -database-url or DATABASE_URL")
		os.Exit(1)
	}

	secret := *jwtSecret
	if secret == "" {
		secret = os.Getenv("JWT_SECRET")
	}
	if len(secret) < 32 {
		logger.Error("JWT secret must be at least 32 bytes (via -jwt-secret or JWT_SECRET)")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()

	pool, err := database.Connect(ctx, databaseURL, 10*time.Second)
	if err != nil {
		logger.Error("connect to database failed", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	tokenManager, err := auth.NewTokenManager(secret, *jwtIssuer, *jwtAudience, *duration)
	if err != nil {
		logger.Error("create token manager failed", "error", err)
		os.Exit(1)
	}

	seeder := seed.NewSeeder(&pgxUserStore{pool: pool}, tokenManager)

	logger.Info("seeding test users", "count", *count, "output", *outputPath)
	start := time.Now()
	tokens, err := seeder.SeedUsers(ctx, *count)
	if err != nil {
		logger.Error("seed users failed", "error", err)
		os.Exit(1)
	}

	if err := seed.SaveTokens(*outputPath, tokens); err != nil {
		logger.Error("save tokens failed", "error", err)
		os.Exit(1)
	}

	logger.Info("seeding complete", "count", len(tokens), "elapsed", time.Since(start), "destination", *outputPath)
	fmt.Printf("Successfully seeded %d users to %s\n", len(tokens), *outputPath)
}

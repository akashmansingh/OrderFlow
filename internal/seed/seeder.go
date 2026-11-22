package seed

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/auth"
)

// UserToken holds the generated identity and pre-signed bearer token for load testing.
type UserToken struct {
	UserID    string    `json:"user_id"`
	Email     string    `json:"email"`
	Token     string    `json:"token"`
	ExpiresAt time.Time `json:"expires_at"`
}

// UserStore defines the subset of DB operations needed to upsert load test users.
type UserStore interface {
	UpsertUser(ctx context.Context, id, email, passwordHash string) (string, error)
}

// Seeder manages generating test users and pre-signing JWTs.
type Seeder struct {
	store  UserStore
	tokens *auth.TokenManager
}

// NewSeeder creates a new test user seeder.
func NewSeeder(store UserStore, tokens *auth.TokenManager) *Seeder {
	return &Seeder{
		store:  store,
		tokens: tokens,
	}
}

// SeedUsers creates or updates n users and generates valid JWT tokens for them.
func (s *Seeder) SeedUsers(ctx context.Context, n int) ([]UserToken, error) {
	if n <= 0 {
		return nil, fmt.Errorf("number of users must be greater than zero")
	}

	results := make([]UserToken, 0, n)
	// Precomputed bcrypt hash for password "loadtest-password"
	dummyPasswordHash := "$2a$10$7vN347K/6bYdM0oJ2QkWtONaT4p9c9iTqWnI36pAomrIkgk497m4m"

	for i := 1; i <= n; i++ {
		email := fmt.Sprintf("loadtest-user-%04d@orderflow.test", i)
		requestedID := uuid.New().String()

		actualID, err := s.store.UpsertUser(ctx, requestedID, email, dummyPasswordHash)
		if err != nil {
			return nil, fmt.Errorf("upsert user %s: %w", email, err)
		}

		tokenResp, err := s.tokens.Issue(actualID)
		if err != nil {
			return nil, fmt.Errorf("issue token for %s (%s): %w", email, actualID, err)
		}

		results = append(results, UserToken{
			UserID:    actualID,
			Email:     email,
			Token:     tokenResp.AccessToken,
			ExpiresAt: tokenResp.ExpiresAt,
		})
	}

	return results, nil
}

// SaveTokens writes the generated tokens as formatted JSON to destination.
func SaveTokens(destination string, tokens []UserToken) error {
	dir := filepath.Dir(destination)
	if dir != "" && dir != "." {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("create directory %s: %w", dir, err)
		}
	}

	data, err := json.MarshalIndent(tokens, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal tokens: %w", err)
	}

	if err := os.WriteFile(destination, append(data, '\n'), 0o644); err != nil {
		return fmt.Errorf("write tokens to %s: %w", destination, err)
	}

	return nil
}

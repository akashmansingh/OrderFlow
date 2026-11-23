package seed_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/theRiddler9/OrderFlow/internal/auth"
	"github.com/theRiddler9/OrderFlow/internal/seed"
)

type memoryUserStore struct {
	users map[string]string // email -> id
}

func newMemoryUserStore() *memoryUserStore {
	return &memoryUserStore{users: make(map[string]string)}
}

func (m *memoryUserStore) UpsertUser(ctx context.Context, id, email, passwordHash string) (string, error) {
	if existingID, ok := m.users[email]; ok {
		return existingID, nil
	}
	m.users[email] = id
	return id, nil
}

func TestSeederGeneratesValidPreSignedTokens(t *testing.T) {
	tokenManager, err := auth.NewTokenManager(
		"super-secure-long-enough-test-secret-key-32b",
		"orderflow",
		"orderflow-api",
		24*time.Hour,
	)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	store := newMemoryUserStore()
	seeder := seed.NewSeeder(store, tokenManager)

	tokens, err := seeder.SeedUsers(context.Background(), 10)
	if err != nil {
		t.Fatalf("SeedUsers() error = %v", err)
	}

	if len(tokens) != 10 {
		t.Fatalf("len(tokens) = %d, want 10", len(tokens))
	}

	for i, ut := range tokens {
		if ut.Token == "" {
			t.Fatalf("user %d token is empty", i)
		}
		subject, err := tokenManager.Verify(ut.Token)
		if err != nil {
			t.Fatalf("token for %s failed verification: %v", ut.Email, err)
		}
		if subject != ut.UserID {
			t.Fatalf("subject = %q, want %q", subject, ut.UserID)
		}
	}

	// Test writing to file
	tempDir := t.TempDir()
	outPath := filepath.Join(tempDir, "tokens.json")

	if err := seed.SaveTokens(outPath, tokens); err != nil {
		t.Fatalf("SaveTokens() error = %v", err)
	}

	data, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("ReadFile() error = %v", err)
	}
	if len(data) == 0 {
		t.Fatalf("tokens file is empty")
	}
}

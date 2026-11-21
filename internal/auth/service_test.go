package auth_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/auth"
)

type fakeStore struct {
	user         auth.User
	passwordHash string
	createEmail  string
	createHash   string
	createErr    error
	lookupEmail  string
	lookupErr    error
	createCalls  int
	lookupCalls  int
}

func (store *fakeStore) CreateUser(_ context.Context, email, passwordHash string) (auth.User, error) {
	store.createCalls++
	store.createEmail = email
	store.createHash = passwordHash
	if store.createErr != nil {
		return auth.User{}, store.createErr
	}
	return store.user, nil
}

func (store *fakeStore) FindUserByEmail(_ context.Context, email string) (auth.User, string, error) {
	store.lookupCalls++
	store.lookupEmail = email
	if store.lookupErr != nil {
		return auth.User{}, "", store.lookupErr
	}
	return store.user, store.passwordHash, nil
}

func newTestService(t *testing.T, store *fakeStore) (*auth.Service, *auth.TokenManager) {
	t.Helper()
	manager, err := auth.NewTokenManager(testJWTSecret, "orderflow", "orderflow-api", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}
	return auth.NewService(store, manager), manager
}

func TestRegisterNormalizesEmailAndStoresHash(t *testing.T) {
	user := auth.User{ID: uuid.NewString(), Email: "ada@example.com", CreatedAt: time.Now().UTC()}
	store := &fakeStore{user: user}
	service, _ := newTestService(t, store)

	got, err := service.Register(t.Context(), "  Ada@Example.COM ", "correct horse battery")
	if err != nil {
		t.Fatalf("Register() error = %v", err)
	}
	if got != user {
		t.Fatalf("Register() = %+v, want %+v", got, user)
	}
	if store.createEmail != "ada@example.com" {
		t.Fatalf("stored email = %q, want normalized email", store.createEmail)
	}
	if store.createHash == "correct horse battery" || !auth.VerifyPassword(store.createHash, "correct horse battery") {
		t.Fatal("Register() did not store a usable password hash")
	}
}

func TestRegisterRejectsInvalidInputBeforeWriting(t *testing.T) {
	store := &fakeStore{}
	service, _ := newTestService(t, store)

	tests := []struct {
		name     string
		email    string
		password string
		wantErr  error
	}{
		{"invalid email", "not-an-email", "correct horse battery", auth.ErrInvalidEmail},
		{"short password", "user@example.com", "too-short", auth.ErrInvalidPassword},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := service.Register(t.Context(), test.email, test.password)
			if !errors.Is(err, test.wantErr) {
				t.Fatalf("Register() error = %v, want %v", err, test.wantErr)
			}
		})
	}
	if store.createCalls != 0 {
		t.Fatalf("CreateUser calls = %d, want 0", store.createCalls)
	}
}

func TestRegisterReturnsDuplicateEmail(t *testing.T) {
	store := &fakeStore{createErr: auth.ErrDuplicateEmail}
	service, _ := newTestService(t, store)

	_, err := service.Register(t.Context(), "user@example.com", "correct horse battery")
	if !errors.Is(err, auth.ErrDuplicateEmail) {
		t.Fatalf("Register() error = %v, want ErrDuplicateEmail", err)
	}
}

func TestLoginIssuesTokenForValidCredentials(t *testing.T) {
	userID := uuid.NewString()
	password := "correct horse battery"
	hash, err := auth.HashPassword(password)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	store := &fakeStore{
		user:         auth.User{ID: userID, Email: "user@example.com"},
		passwordHash: hash,
	}
	service, manager := newTestService(t, store)

	token, err := service.Login(t.Context(), " USER@Example.com ", password)
	if err != nil {
		t.Fatalf("Login() error = %v", err)
	}
	if store.lookupEmail != "user@example.com" {
		t.Fatalf("lookup email = %q, want normalized email", store.lookupEmail)
	}
	if subject, err := manager.Verify(token.AccessToken); err != nil || subject != userID {
		t.Fatalf("token subject = %q, error = %v, want %q", subject, err, userID)
	}
}

func TestLoginRejectsInvalidCredentials(t *testing.T) {
	hash, err := auth.HashPassword("correct horse battery")
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}

	tests := map[string]*fakeStore{
		"wrong password": {user: auth.User{ID: uuid.NewString()}, passwordHash: hash},
		"unknown email":  {lookupErr: auth.ErrUserNotFound},
	}

	for name, store := range tests {
		t.Run(name, func(t *testing.T) {
			service, _ := newTestService(t, store)
			_, err := service.Login(t.Context(), "user@example.com", "wrong password value")
			if !errors.Is(err, auth.ErrInvalidCredentials) {
				t.Fatalf("Login() error = %v, want ErrInvalidCredentials", err)
			}
		})
	}
}

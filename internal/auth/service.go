package auth

import (
	"context"
	"errors"
	"fmt"
)

var ErrInvalidCredentials = errors.New("invalid email or password")

// Service coordinates password verification, user persistence, and access tokens.
type Service struct {
	store  Store
	tokens *TokenManager
}

func NewService(store Store, tokens *TokenManager) *Service {
	return &Service{store: store, tokens: tokens}
}

func (service *Service) Register(ctx context.Context, email, password string) (User, error) {
	normalizedEmail := NormalizeEmail(email)
	if err := ValidateEmail(normalizedEmail); err != nil {
		return User{}, err
	}
	passwordHash, err := HashPassword(password)
	if err != nil {
		return User{}, err
	}

	user, err := service.store.CreateUser(ctx, normalizedEmail, passwordHash)
	if errors.Is(err, ErrDuplicateEmail) {
		return User{}, ErrDuplicateEmail
	}
	if err != nil {
		return User{}, fmt.Errorf("register user: %w", err)
	}
	return user, nil
}

func (service *Service) Login(ctx context.Context, email, password string) (TokenResponse, error) {
	normalizedEmail := NormalizeEmail(email)
	if ValidateEmail(normalizedEmail) != nil || password == "" {
		return TokenResponse{}, ErrInvalidCredentials
	}

	user, passwordHash, err := service.store.FindUserByEmail(ctx, normalizedEmail)
	if errors.Is(err, ErrUserNotFound) {
		return TokenResponse{}, ErrInvalidCredentials
	}
	if err != nil {
		return TokenResponse{}, fmt.Errorf("look up user: %w", err)
	}
	if !VerifyPassword(passwordHash, password) {
		return TokenResponse{}, ErrInvalidCredentials
	}

	token, err := service.tokens.Issue(user.ID)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("issue access token: %w", err)
	}
	return token, nil
}

// VerifyAccessToken lets HTTP middleware authenticate requests without exposing token internals.
func (service *Service) VerifyAccessToken(rawToken string) (string, error) {
	return service.tokens.Verify(rawToken)
}

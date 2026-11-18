package auth

import (
	"errors"
	"fmt"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

const minJWTSecretBytes = 32

var ErrInvalidToken = errors.New("invalid access token")

// TokenManager issues and verifies short-lived HS256 access tokens.
type TokenManager struct {
	secret   []byte
	issuer   string
	audience string
	lifetime time.Duration
}

// TokenResponse is the login response contract.
type TokenResponse struct {
	AccessToken string    `json:"access_token"`
	TokenType   string    `json:"token_type"`
	ExpiresAt   time.Time `json:"expires_at"`
}

func NewTokenManager(secret, issuer, audience string, lifetime time.Duration) (*TokenManager, error) {
	if len(secret) < minJWTSecretBytes {
		return nil, fmt.Errorf("JWT_SECRET must be at least %d bytes", minJWTSecretBytes)
	}
	if issuer == "" || audience == "" {
		return nil, errors.New("JWT issuer and audience are required")
	}
	if lifetime <= 0 {
		return nil, errors.New("JWT access token lifetime must be positive")
	}

	return &TokenManager{
		secret:   []byte(secret),
		issuer:   issuer,
		audience: audience,
		lifetime: lifetime,
	}, nil
}

// Issue creates a bearer token whose subject is the authenticated user ID.
func (manager *TokenManager) Issue(userID string) (TokenResponse, error) {
	if _, err := uuid.Parse(userID); err != nil {
		return TokenResponse{}, fmt.Errorf("issue access token: subject must be a user UUID")
	}

	now := time.Now().UTC()
	expiresAt := now.Add(manager.lifetime)
	claims := jwt.RegisteredClaims{
		Subject:   userID,
		Issuer:    manager.issuer,
		Audience:  jwt.ClaimStrings{manager.audience},
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(expiresAt),
	}

	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(manager.secret)
	if err != nil {
		return TokenResponse{}, fmt.Errorf("sign access token: %w", err)
	}

	return TokenResponse{
		AccessToken: signed,
		TokenType:   "Bearer",
		ExpiresAt:   expiresAt,
	}, nil
}

// Verify validates the signature and every required claim, returning the user UUID subject.
func (manager *TokenManager) Verify(rawToken string) (string, error) {
	parser := jwt.NewParser(
		jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Alg()}),
		jwt.WithIssuer(manager.issuer),
		jwt.WithAudience(manager.audience),
		jwt.WithExpirationRequired(),
		jwt.WithIssuedAt(),
	)

	var claims jwt.RegisteredClaims
	token, err := parser.ParseWithClaims(rawToken, &claims, func(token *jwt.Token) (any, error) {
		if token.Method.Alg() != jwt.SigningMethodHS256.Alg() {
			return nil, errors.New("unexpected JWT signing method")
		}
		return manager.secret, nil
	})
	if err != nil || !token.Valid || claims.IssuedAt == nil {
		return "", ErrInvalidToken
	}
	if _, err := uuid.Parse(claims.Subject); err != nil {
		return "", ErrInvalidToken
	}

	return claims.Subject, nil
}

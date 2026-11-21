package auth

import (
	"errors"
	"fmt"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/bcrypt"
)

const (
	// MinPasswordLength is the minimum number of characters accepted at registration.
	MinPasswordLength = 12
	// bcrypt silently truncates inputs beyond 72 bytes, so reject them explicitly.
	maxPasswordBytes = 72
)

var (
	ErrInvalidEmail    = errors.New("email must be a valid address")
	ErrInvalidPassword = fmt.Errorf("password must be at least %d characters and at most %d bytes", MinPasswordLength, maxPasswordBytes)
)

// NormalizeEmail produces the canonical email representation stored and looked up by OrderFlow.
func NormalizeEmail(email string) string {
	return strings.ToLower(strings.TrimSpace(email))
}

// ValidateEmail rejects obviously malformed addresses before they reach the database.
func ValidateEmail(email string) error {
	local, domain, hasAt := strings.Cut(email, "@")
	if !hasAt || local == "" || domain == "" || strings.ContainsAny(email, " \t\r\n") || strings.Contains(domain, "@") {
		return ErrInvalidEmail
	}
	return nil
}

// ValidatePassword enforces the registration password policy and bcrypt's input limit.
func ValidatePassword(password string) error {
	if utf8.RuneCountInString(password) < MinPasswordLength || len(password) > maxPasswordBytes {
		return ErrInvalidPassword
	}
	return nil
}

// HashPassword returns a bcrypt hash for a valid password.
func HashPassword(password string) (string, error) {
	if err := ValidatePassword(password); err != nil {
		return "", err
	}
	hash, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.DefaultCost)
	if err != nil {
		return "", fmt.Errorf("hash password: %w", err)
	}
	return string(hash), nil
}

// VerifyPassword compares a candidate password with a stored bcrypt hash.
func VerifyPassword(hash, password string) bool {
	if hash == "" || len(password) > maxPasswordBytes {
		return false
	}
	return bcrypt.CompareHashAndPassword([]byte(hash), []byte(password)) == nil
}

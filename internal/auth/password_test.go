package auth_test

import (
	"errors"
	"strings"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/auth"
)

func TestNormalizeEmail(t *testing.T) {
	got := auth.NormalizeEmail("  Ada.Lovelace@Example.COM ")
	if want := "ada.lovelace@example.com"; got != want {
		t.Fatalf("NormalizeEmail() = %q, want %q", got, want)
	}
}

func TestValidateEmail(t *testing.T) {
	tests := map[string]bool{
		"user@example.com":       true,
		"user+tag@example.co.uk": true,
		"":                       false,
		"not-an-email":           false,
		"user@":                  false,
		"@example.com":           false,
		"user name@example.com":  false,
		"user@one@two.com":       false,
	}

	for email, wantValid := range tests {
		err := auth.ValidateEmail(email)
		if wantValid && err != nil {
			t.Errorf("ValidateEmail(%q) error = %v, want valid", email, err)
		}
		if !wantValid && !errors.Is(err, auth.ErrInvalidEmail) {
			t.Errorf("ValidateEmail(%q) error = %v, want ErrInvalidEmail", email, err)
		}
	}
}

func TestPasswordPolicyAndHashing(t *testing.T) {
	validPassword := "correct horse battery"
	if err := auth.ValidatePassword(validPassword); err != nil {
		t.Fatalf("ValidatePassword(valid) error = %v", err)
	}
	if err := auth.ValidatePassword(strings.Repeat("a", auth.MinPasswordLength-1)); !errors.Is(err, auth.ErrInvalidPassword) {
		t.Fatalf("ValidatePassword(short) error = %v, want ErrInvalidPassword", err)
	}
	if err := auth.ValidatePassword(strings.Repeat("a", 73)); !errors.Is(err, auth.ErrInvalidPassword) {
		t.Fatalf("ValidatePassword(long) error = %v, want ErrInvalidPassword", err)
	}

	hash, err := auth.HashPassword(validPassword)
	if err != nil {
		t.Fatalf("HashPassword() error = %v", err)
	}
	if hash == validPassword {
		t.Fatal("password hash must not equal plaintext password")
	}
	if !auth.VerifyPassword(hash, validPassword) {
		t.Fatal("VerifyPassword() rejected the original password")
	}
	if auth.VerifyPassword(hash, "wrong password value") {
		t.Fatal("VerifyPassword() accepted the wrong password")
	}
	if auth.VerifyPassword(hash, strings.Repeat(validPassword, 5)) {
		t.Fatal("VerifyPassword() accepted an overlong password")
	}
}

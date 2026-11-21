package auth_test

import (
	"errors"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/theRiddler9/OrderFlow/internal/auth"
)

const testJWTSecret = "test-secret-with-at-least-thirty-two-bytes"

func TestIssueAndVerifyToken(t *testing.T) {
	manager, err := auth.NewTokenManager(testJWTSecret, "orderflow", "orderflow-api", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	userID := uuid.NewString()
	token, err := manager.Issue(userID)
	if err != nil {
		t.Fatalf("Issue() error = %v", err)
	}
	if token.TokenType != "Bearer" || token.AccessToken == "" {
		t.Fatalf("Issue() returned %+v", token)
	}
	if !token.ExpiresAt.After(time.Now()) {
		t.Fatalf("ExpiresAt = %s, want a future time", token.ExpiresAt)
	}

	subject, err := manager.Verify(token.AccessToken)
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if subject != userID {
		t.Fatalf("Verify() subject = %q, want %q", subject, userID)
	}
}

func TestNewTokenManagerValidatesConfiguration(t *testing.T) {
	tests := map[string]func() error{
		"short secret": func() error {
			_, err := auth.NewTokenManager("short", "orderflow", "orderflow-api", time.Minute)
			return err
		},
		"empty issuer": func() error {
			_, err := auth.NewTokenManager(testJWTSecret, "", "orderflow-api", time.Minute)
			return err
		},
		"empty audience": func() error {
			_, err := auth.NewTokenManager(testJWTSecret, "orderflow", "", time.Minute)
			return err
		},
		"zero lifetime": func() error {
			_, err := auth.NewTokenManager(testJWTSecret, "orderflow", "orderflow-api", 0)
			return err
		},
	}

	for name, run := range tests {
		if err := run(); err == nil {
			t.Errorf("%s: expected a validation error", name)
		}
	}
}

func TestVerifyRejectsInvalidTokens(t *testing.T) {
	manager, err := auth.NewTokenManager(testJWTSecret, "orderflow", "orderflow-api", 15*time.Minute)
	if err != nil {
		t.Fatalf("NewTokenManager() error = %v", err)
	}

	now := time.Now().UTC()
	validClaims := func() jwt.RegisteredClaims {
		return jwt.RegisteredClaims{
			Subject:   uuid.NewString(),
			Issuer:    "orderflow",
			Audience:  jwt.ClaimStrings{"orderflow-api"},
			IssuedAt:  jwt.NewNumericDate(now),
			ExpiresAt: jwt.NewNumericDate(now.Add(15 * time.Minute)),
		}
	}

	wrongIssuer := validClaims()
	wrongIssuer.Issuer = "other-issuer"
	wrongAudience := validClaims()
	wrongAudience.Audience = jwt.ClaimStrings{"other-api"}
	expired := validClaims()
	expired.ExpiresAt = jwt.NewNumericDate(now.Add(-time.Minute))
	missingExpiry := validClaims()
	missingExpiry.ExpiresAt = nil
	missingIssuedAt := validClaims()
	missingIssuedAt.IssuedAt = nil
	missingSubject := validClaims()
	missingSubject.Subject = ""
	malformedSubject := validClaims()
	malformedSubject.Subject = "not-a-uuid"

	tests := map[string]string{
		"wrong secret":           signClaims(t, validClaims(), jwt.SigningMethodHS256, []byte("a-different-secret-that-is-also-long-enough")),
		"wrong issuer":           signClaims(t, wrongIssuer, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"wrong audience":         signClaims(t, wrongAudience, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"expired":                signClaims(t, expired, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"missing expiry":         signClaims(t, missingExpiry, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"missing issued-at":      signClaims(t, missingIssuedAt, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"missing subject":        signClaims(t, missingSubject, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"malformed subject":      signClaims(t, malformedSubject, jwt.SigningMethodHS256, []byte(testJWTSecret)),
		"unexpected algorithm":   signClaims(t, validClaims(), jwt.SigningMethodHS384, []byte(testJWTSecret)),
		"unsecured token":        signClaims(t, validClaims(), jwt.SigningMethodNone, jwt.UnsafeAllowNoneSignatureType),
		"malformed token string": "not.a.jwt",
	}

	for name, rawToken := range tests {
		t.Run(name, func(t *testing.T) {
			subject, err := manager.Verify(rawToken)
			if !errors.Is(err, auth.ErrInvalidToken) {
				t.Fatalf("Verify() error = %v, want ErrInvalidToken", err)
			}
			if subject != "" {
				t.Fatalf("Verify() subject = %q, want empty", subject)
			}
		})
	}
}

func signClaims(t *testing.T, claims jwt.Claims, method jwt.SigningMethod, key any) string {
	t.Helper()
	signed, err := jwt.NewWithClaims(method, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign test token: %v", err)
	}
	return signed
}

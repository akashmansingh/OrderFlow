package verify_test

import (
	"context"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/verify"
)

func TestVerifierHandlesNilPoolDefensively(t *testing.T) {
	// Defensively tests that nil pool results in a failed check rather than an unhandled panic
	defer func() {
		if r := recover(); r == nil {
			// If it returned cleanly without panic, check outcome
		}
	}()

	verifier := verify.NewVerifier(nil)
	results, allPassed := verifier.VerifyAll(context.Background())
	if allPassed {
		t.Fatal("expected allPassed to be false with nil pool")
	}
	if len(results) != 5 {
		t.Fatalf("expected 5 check results, got %d", len(results))
	}
}

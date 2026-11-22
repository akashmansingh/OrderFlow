package workflow_test

import (
	"encoding/json"
	"errors"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/workflow"
)

func TestFormatDLQEnvelope(t *testing.T) {
	original := []byte(`{"invalid":"json_body"}`)
	reason := "syntax error"
	retryCount := 3

	encoded, err := workflow.FormatDLQEnvelope(original, reason, retryCount)
	if err != nil {
		t.Fatalf("FormatDLQEnvelope() error = %v", err)
	}

	var env workflow.DLQEnvelope
	if err := json.Unmarshal(encoded, &env); err != nil {
		t.Fatalf("unmarshal DLQEnvelope: %v", err)
	}

	if env.ID == "" {
		t.Fatal("expected non-empty envelope ID")
	}
	if string(env.OriginalPayload) != string(original) {
		t.Fatalf("OriginalPayload = %s, want %s", env.OriginalPayload, original)
	}
	if env.ErrorReason != reason {
		t.Fatalf("ErrorReason = %q, want %q", env.ErrorReason, reason)
	}
	if env.RetryCount != retryCount {
		t.Fatalf("RetryCount = %d, want %d", env.RetryCount, retryCount)
	}
	if env.FailedAt.IsZero() {
		t.Fatal("expected non-zero FailedAt timestamp")
	}
}

func TestIsPoisonError(t *testing.T) {
	if workflow.IsPoisonError(nil) {
		t.Fatal("expected nil error not to be poison")
	}

	if !workflow.IsPoisonError(workflow.ErrPoisonMessage) {
		t.Fatal("expected ErrPoisonMessage to be recognized as poison")
	}

	var dummy map[string]int
	unmarshalErr := json.Unmarshal([]byte(`{"not_an_int":"abc"}`), &dummy)
	if !workflow.IsPoisonError(unmarshalErr) {
		t.Fatalf("expected UnmarshalTypeError to be recognized as poison, got err = %v", unmarshalErr)
	}

	transientErr := errors.New("connection reset by peer")
	if workflow.IsPoisonError(transientErr) {
		t.Fatal("expected transient network error not to be poison")
	}
}

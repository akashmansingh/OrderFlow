package workflow_test

import (
	"context"
	"strings"
	"testing"

	"github.com/theRiddler9/OrderFlow/internal/workflow"
)

func TestExpiryRunOnceRequiresDatabase(t *testing.T) {
	err := workflow.NewExpiry(nil).RunOnce(context.Background())
	if err == nil || !strings.Contains(err.Error(), "database") {
		t.Fatalf("RunOnce() error = %v, want database requirement", err)
	}
}

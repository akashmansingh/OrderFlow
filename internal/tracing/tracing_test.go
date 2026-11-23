package tracing

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
)

func TestInitWithNoEndpointInstallsNoopProvider(t *testing.T) {
	ctx := context.Background()
	shutdown, err := Init(ctx, "test-service", "")
	if err != nil {
		t.Fatalf("Init with empty endpoint returned unexpected error: %v", err)
	}
	defer func() {
		if err := shutdown(ctx); err != nil {
			t.Errorf("shutdown returned error: %v", err)
		}
	}()

	tracer := Tracer("test-tracer")
	if tracer == nil {
		t.Fatal("expected non-nil tracer")
	}

	_, span := tracer.Start(ctx, "test-span")
	if span == nil {
		t.Fatal("expected non-nil span from tracer")
	}
	span.End()

	// Verify W3C propagator is set
	propagator := otel.GetTextMapPropagator()
	if propagator == nil {
		t.Fatal("expected non-nil text map propagator")
	}

	header := http.Header{}
	propagator.Inject(ctx, propagation.HeaderCarrier(header))
}

func TestInitWithMockOTLPEndpoint(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	shutdown, err := Init(ctx, "mock-service", server.URL)
	if err != nil {
		t.Fatalf("Init returned unexpected error: %v", err)
	}

	tracer := Tracer("mock-tracer")
	ctx, span := tracer.Start(ctx, "mock-operation")
	span.End()

	if err := shutdown(ctx); err != nil {
		t.Fatalf("shutdown failed: %v", err)
	}
}

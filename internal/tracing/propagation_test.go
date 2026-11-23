package tracing

import (
	"context"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func TestPropagationInjectAndExtract(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	defer func() { _ = tp.Shutdown(context.Background()) }()
	tracer := tp.Tracer("test")

	ctx, span := tracer.Start(context.Background(), "parent-op")
	defer span.End()

	traceParent, _ := InjectTraceParent(ctx)
	if traceParent == "" {
		t.Fatal("expected non-empty traceParent string")
	}

	traceID := TraceIDFromContext(ctx)
	if traceID == "" {
		t.Fatal("expected valid traceID")
	}

	// Extract in a new context
	extractedCtx := ExtractTraceContext(context.Background(), traceParent, "")
	extractedTraceID := TraceIDFromContext(extractedCtx)
	if extractedTraceID != traceID {
		t.Fatalf("extracted TraceID = %s, want %s", extractedTraceID, traceID)
	}
}

func TestPropagationEmptyContext(t *testing.T) {
	traceParent, traceState := InjectTraceParent(context.Background())
	if traceParent != "" || traceState != "" {
		t.Fatalf("expected empty strings from empty context, got tp=%q, ts=%q", traceParent, traceState)
	}

	ctx := ExtractTraceContext(context.Background(), "", "")
	if TraceIDFromContext(ctx) != "" {
		t.Fatal("expected empty trace id from empty extraction")
	}
}

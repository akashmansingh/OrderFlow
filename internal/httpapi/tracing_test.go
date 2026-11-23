package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestTracingMiddlewareInjectsResponseHeader(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	handler := TracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	}))

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	traceParent := rec.Header().Get("traceparent")
	if traceParent == "" {
		t.Fatal("expected traceparent header in HTTP response")
	}
	if !strings.HasPrefix(traceParent, "00-") {
		t.Fatalf("unexpected traceparent format: %q", traceParent)
	}
}

func TestTracingMiddlewarePropagatesIncomingTraceParent(t *testing.T) {
	otel.SetTextMapPropagator(propagation.TraceContext{})
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
	defer func() { _ = tp.Shutdown(context.Background()) }()

	incomingTP := "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01"

	var observedTraceID string
	handler := TracingMiddleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		span := trace.SpanFromContext(r.Context())
		if span.SpanContext().IsValid() {
			observedTraceID = span.SpanContext().TraceID().String()
		}
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest(http.MethodGet, "/v1/events", nil)
	req.Header.Set("traceparent", incomingTP)
	rec := httptest.NewRecorder()

	handler.ServeHTTP(rec, req)

	if observedTraceID != "4bf92f3577b34da6a3ce929d0e0e4736" {
		t.Fatalf("observed TraceID = %q, want %q", observedTraceID, "4bf92f3577b34da6a3ce929d0e0e4736")
	}

	resTP := rec.Header().Get("traceparent")
	if !strings.Contains(resTP, "4bf92f3577b34da6a3ce929d0e0e4736") {
		t.Fatalf("response traceparent %q does not contain parent trace id", resTP)
	}
}

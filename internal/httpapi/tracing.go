package httpapi

import (
	"net/http"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"

	"github.com/theRiddler9/OrderFlow/internal/tracing"
)

// TracingMiddleware extracts W3C trace context, starts an HTTP server span,
// records request metadata and status code, and injects the traceparent into the response.
func TracingMiddleware(next http.Handler) http.Handler {
	tracer := tracing.Tracer("orderflow.httpapi")
	propagator := otel.GetTextMapPropagator()

	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ctx := propagator.Extract(r.Context(), propagation.HeaderCarrier(r.Header))

		spanName := "HTTP " + r.Method + " " + r.URL.Path
		ctx, span := tracer.Start(ctx, spanName,
			trace.WithSpanKind(trace.SpanKindServer),
			trace.WithAttributes(
				semconv.HTTPRequestMethodKey.String(r.Method),
				semconv.URLPathKey.String(r.URL.Path),
				semconv.ClientAddressKey.String(r.RemoteAddr),
				semconv.UserAgentOriginalKey.String(r.UserAgent()),
			),
		)
		defer span.End()

		// Inject traceparent to response header so clients can correlate
		if tp, _ := tracing.InjectTraceParent(ctx); tp != "" {
			w.Header().Set("traceparent", tp)
		}

		recorder := &statusRecorder{
			ResponseWriter: w,
			statusCode:     http.StatusOK,
		}

		next.ServeHTTP(recorder, r.WithContext(ctx))

		span.SetAttributes(semconv.HTTPResponseStatusCodeKey.Int(recorder.statusCode))
		if recorder.statusCode >= 500 {
			span.SetStatus(codes.Error, http.StatusText(recorder.statusCode))
		} else {
			span.SetStatus(codes.Ok, "")
		}
	})
}

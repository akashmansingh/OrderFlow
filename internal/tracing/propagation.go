package tracing

import (
	"context"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// InjectTraceParent returns the W3C traceparent and tracestate strings for the active span in ctx.
// If ctx has no active span or invalid trace context, it returns empty strings.
func InjectTraceParent(ctx context.Context) (traceParent string, traceState string) {
	carrier := propagation.MapCarrier{}
	otel.GetTextMapPropagator().Inject(ctx, carrier)
	return carrier.Get("traceparent"), carrier.Get("tracestate")
}

// ExtractTraceContext creates a new context containing the trace context parsed from traceParent and traceState.
func ExtractTraceContext(ctx context.Context, traceParent, traceState string) context.Context {
	if traceParent == "" {
		return ctx
	}
	carrier := propagation.MapCarrier{
		"traceparent": traceParent,
	}
	if traceState != "" {
		carrier["tracestate"] = traceState
	}
	return otel.GetTextMapPropagator().Extract(ctx, carrier)
}

// TraceIDFromContext returns the hex trace ID string if ctx has a valid span.
func TraceIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		return spanCtx.TraceID().String()
	}
	return ""
}

// SpanIDFromContext returns the hex span ID string if ctx has a valid span.
func SpanIDFromContext(ctx context.Context) string {
	spanCtx := trace.SpanContextFromContext(ctx)
	if spanCtx.IsValid() {
		return spanCtx.SpanID().String()
	}
	return ""
}

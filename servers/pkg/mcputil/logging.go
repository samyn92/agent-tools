package mcputil

import (
	"context"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel/trace"
)

// Logger returns a slog.Logger that automatically injects trace_id and span_id
// from the context into every log record. This enables log-trace correlation
// in Tempo/Loki.
//
// Usage:
//
//	log := mcputil.Logger()
//	log.InfoContext(ctx, "fetching repo", "owner", owner)
//	// Output: level=INFO msg="fetching repo" owner=acme trace_id=abc123 span_id=def456
func Logger() *slog.Logger {
	return slog.New(&traceHandler{inner: slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})})
}

// LoggerJSON returns a JSON-formatted logger with trace correlation.
// Preferred for structured log aggregation (e.g. Loki).
func LoggerJSON() *slog.Logger {
	return slog.New(&traceHandler{inner: slog.NewJSONHandler(os.Stderr, &slog.HandlerOptions{
		Level: slog.LevelInfo,
	})})
}

// traceHandler wraps a slog.Handler to inject trace context.
type traceHandler struct {
	inner slog.Handler
}

func (h *traceHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return h.inner.Enabled(ctx, level)
}

func (h *traceHandler) Handle(ctx context.Context, r slog.Record) error {
	sc := trace.SpanContextFromContext(ctx)
	if sc.IsValid() {
		r.AddAttrs(
			slog.String("trace_id", sc.TraceID().String()),
			slog.String("span_id", sc.SpanID().String()),
		)
	}
	return h.inner.Handle(ctx, r)
}

func (h *traceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return &traceHandler{inner: h.inner.WithAttrs(attrs)}
}

func (h *traceHandler) WithGroup(name string) slog.Handler {
	return &traceHandler{inner: h.inner.WithGroup(name)}
}

// Package otelutil provides OpenTelemetry tracing initialization for MCP tool servers.
//
// Usage:
//
//	shutdown, _ := otelutil.Init(ctx, "mcp-tool-tempo")
//	defer shutdown(context.Background())
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is a no-op.
// When set, traces are exported via gRPC to the specified endpoint.
package otelutil

import (
	"context"
	"os"
	"time"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.26.0"
	"go.opentelemetry.io/otel/trace"
)

// Tracer is the package-level tracer. Safe to use after Init() returns.
// When tracing is disabled (no endpoint), this is a no-op tracer.
var Tracer trace.Tracer

// Init initializes OpenTelemetry tracing for an MCP tool server.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is a no-op —
// the returned shutdown function is safe to call but does nothing.
//
// The returned function must be called on shutdown to flush pending spans.
func Init(ctx context.Context, serviceName string) (func(context.Context) error, error) {
	endpoint := os.Getenv("OTEL_EXPORTER_OTLP_ENDPOINT")
	if endpoint == "" {
		Tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }, nil
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName(serviceName),
		),
	)
	if err != nil {
		res = resource.Default()
	}

	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpoint(endpoint),
		otlptracegrpc.WithInsecure(),
	)
	if err != nil {
		// Graceful degradation: tracing won't work but the tool still runs.
		Tracer = otel.Tracer(serviceName)
		return func(context.Context) error { return nil }, err
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter, sdktrace.WithBatchTimeout(2*time.Second)),
		sdktrace.WithResource(res),
		sdktrace.WithSampler(sdktrace.AlwaysSample()),
	)

	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.TraceContext{})
	Tracer = tp.Tracer(serviceName)

	return tp.Shutdown, nil
}

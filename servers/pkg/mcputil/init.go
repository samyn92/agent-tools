// Package mcputil is the AgentOps SDK for building traced MCP tool servers.
//
// It provides:
//   - Automatic OpenTelemetry tracing for every tool invocation (AddTool / AddToolTo)
//   - Session-level root spans with server metadata (NewServer)
//   - Panic recovery with stack traces recorded as span events
//   - Traced HTTP client for external API calls (TracedHTTP)
//   - Traced subprocess execution for CLI wrappers (TracedExec)
//   - Traced Kubernetes API operations for client-go users (K8sOp)
//   - Structured logging with trace_id/span_id correlation (Logger)
//   - Optional tool input/output recording as span events (WithInputOutput)
//   - Health/readiness signaling for operator probes (Ready)
//   - Shared result helpers (TextResult / ErrResult)
//
// Quick start:
//
//	func main() {
//	    ctx := context.Background()
//	    shutdown, _ := mcputil.Init(ctx, "mcp-tool-myservice")
//	    defer shutdown(ctx)
//
//	    log := mcputil.Logger()
//	    server := mcputil.NewServer("myservice", "0.1.0")
//
//	    mcputil.AddToolTo(server, "my_tool", "Does something.", handler)
//
//	    mcputil.Ready("mcp-tool-myservice")
//
//	    ctx, stop := signal.NotifyContext(ctx, syscall.SIGTERM, syscall.SIGINT)
//	    defer stop()
//	    server.Run(ctx, &mcp.StdioTransport{})
//	}
//
// Every tool registered via AddTool/AddToolTo is automatically wrapped in an
// OpenTelemetry span with tool name, duration, error status, and result metadata.
// Handler panics are recovered and returned as error results with stack traces
// recorded in the span.
//
// When OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is a no-op —
// zero overhead, the tools still work normally.
package mcputil

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

// Tracer is the package-level tracer used by all instrumentation in this package.
// Safe to use after Init() returns. No-op when tracing is disabled.
var Tracer trace.Tracer

// Init initializes OpenTelemetry tracing for an MCP tool server.
//
// If OTEL_EXPORTER_OTLP_ENDPOINT is not set, tracing is a no-op —
// the returned shutdown function is safe to call but does nothing.
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

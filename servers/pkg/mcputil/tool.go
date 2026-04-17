package mcputil

import (
	"context"
	"encoding/json"
	"fmt"
	"runtime/debug"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// ToolOption configures per-tool behavior for AddTool.
type ToolOption func(*toolOpts)

type toolOpts struct {
	recordIO bool
}

// WithInputOutput enables recording of tool input and output as span events.
// Input is recorded as "gen_ai.tool.input" and output as "gen_ai.tool.output",
// matching the agentops-runtime convention. Output is truncated to 2000 chars.
//
// Use with care — inputs may contain sensitive data (API keys, tokens, etc.).
func WithInputOutput() ToolOption {
	return func(o *toolOpts) { o.recordIO = true }
}

// AddTool registers an MCP tool with automatic OpenTelemetry tracing.
//
// Every invocation creates a span named "tool.<name>" with:
//   - tool.name          — the tool name
//   - tool.duration_ms   — execution duration in milliseconds
//   - tool.result.error  — true if the tool returned an error result
//
// Panics in handlers are recovered, recorded as span events with stack traces,
// and returned as error results to the agent instead of crashing the server.
//
// Works with both *mcp.Server and *mcputil.Server.
func AddTool[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any], opts ...ToolOption) {
	o := &toolOpts{}
	for _, opt := range opts {
		opt(o)
	}

	wrapped := func(ctx context.Context, req *mcp.CallToolRequest, in In) (result *mcp.CallToolResult, meta any, err error) {
		ctx, span := Tracer.Start(ctx, "tool."+name, trace.WithAttributes(
			attribute.String("tool.name", name),
			attribute.String("gen_ai.operation.name", "execute_tool"),
		))
		defer span.End()

		// Record input as span event if opt-in.
		if o.recordIO {
			if inputJSON, jsonErr := json.Marshal(in); jsonErr == nil {
				span.AddEvent("gen_ai.tool.input", trace.WithAttributes(
					attribute.String("tool.name", name),
					attribute.String("tool.input", truncate(string(inputJSON), 2000)),
				))
			}
		}

		start := time.Now()

		// Panic recovery — catch handler panics, record stack trace, return error result.
		defer func() {
			if r := recover(); r != nil {
				stack := string(debug.Stack())
				panicMsg := fmt.Sprintf("panic in tool %s: %v", name, r)

				span.AddEvent("tool.panic", trace.WithAttributes(
					attribute.String("panic.message", fmt.Sprintf("%v", r)),
					attribute.String("panic.stack", truncate(stack, 4000)),
				))
				span.SetStatus(codes.Error, panicMsg)
				span.SetAttributes(
					attribute.Bool("tool.result.error", true),
					attribute.Float64("tool.duration_ms", float64(time.Since(start).Milliseconds())),
				)

				result = ErrResult("internal error: tool panicked — %v", r)
				meta = nil
				err = nil // Don't propagate panic as error — return a usable result to the agent.
			}
		}()

		result, meta, err = h(ctx, req, in)
		elapsed := time.Since(start)

		span.SetAttributes(attribute.Float64("tool.duration_ms", float64(elapsed.Milliseconds())))

		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, err.Error())
			return result, meta, err
		}

		if result != nil && result.IsError {
			span.SetAttributes(attribute.Bool("tool.result.error", true))
			if msg := resultText(result); msg != "" {
				span.SetStatus(codes.Error, truncate(msg, 256))
			}
		}

		// Record output as span event if opt-in.
		if o.recordIO && result != nil {
			if msg := resultText(result); msg != "" {
				attrs := []attribute.KeyValue{
					attribute.String("tool.name", name),
					attribute.String("tool.output", truncate(msg, 2000)),
				}
				if result.IsError {
					attrs = append(attrs, attribute.Bool("tool.error", true))
				}
				span.AddEvent("gen_ai.tool.output", trace.WithAttributes(attrs...))
			}
		}

		return result, meta, nil
	}

	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, wrapped)
}

// AddToolTo registers an MCP tool on a mcputil.Server, incrementing the tool count
// for session span attributes. Same tracing behavior as AddTool.
func AddToolTo[In any](s *Server, name, desc string, h mcp.ToolHandlerFor[In, any], opts ...ToolOption) {
	s.incrementToolCount()
	AddTool(s.Server, name, desc, h, opts...)
}

// resultText extracts the first text content from a CallToolResult.
func resultText(r *mcp.CallToolResult) string {
	if r == nil {
		return ""
	}
	for _, c := range r.Content {
		if tc, ok := c.(*mcp.TextContent); ok {
			return tc.Text
		}
	}
	return ""
}

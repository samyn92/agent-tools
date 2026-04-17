package mcputil

import (
	"context"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// Server wraps an mcp.Server with a session-level root span.
// All tool spans created via AddTool become children of the session span.
type Server struct {
	*mcp.Server
	name    string
	version string
	mode    string

	// sessionCtx carries the root mcp.session span.
	// Set by Run(), used by AddTool wrappers via SessionContext().
	sessionCtx context.Context
	sessionMu  sync.RWMutex
	toolCount  int
}

// ServerOption configures a Server.
type ServerOption func(*Server)

// WithMode sets the server mode (e.g. "readonly", "readwrite") as a span attribute.
func WithMode(mode string) ServerOption {
	return func(s *Server) { s.mode = mode }
}

// NewServer creates an MCP server wrapped with session-level tracing.
//
// When Run() is called, a root span "mcp.session" is started that lives for
// the entire server lifecycle. All tool invocations become children of this span.
// The session span records:
//   - mcp.server.name     — server implementation name
//   - mcp.server.version  — server version
//   - mcp.server.mode     — optional mode (readonly/readwrite)
//   - mcp.tool.count      — number of registered tools
func NewServer(name, version string, opts ...ServerOption) *Server {
	s := &Server{
		Server:  mcp.NewServer(&mcp.Implementation{Name: name, Version: version}, nil),
		name:    name,
		version: version,
	}
	for _, opt := range opts {
		opt(s)
	}
	return s
}

// Run starts the MCP server with a session-level root span.
// The span lives until the server exits (context cancellation or transport close).
func (s *Server) Run(ctx context.Context, transport mcp.Transport) error {
	attrs := []attribute.KeyValue{
		attribute.String("mcp.server.name", s.name),
		attribute.String("mcp.server.version", s.version),
		attribute.Int("mcp.tool.count", s.toolCount),
	}
	if s.mode != "" {
		attrs = append(attrs, attribute.String("mcp.server.mode", s.mode))
	}

	ctx, span := Tracer.Start(ctx, "mcp.session", trace.WithAttributes(attrs...))
	defer span.End()

	s.sessionMu.Lock()
	s.sessionCtx = ctx
	s.sessionMu.Unlock()

	span.AddEvent("mcp.server.started")

	err := s.Server.Run(ctx, transport)

	if err != nil && ctx.Err() == nil {
		span.AddEvent("mcp.server.error", trace.WithAttributes(
			attribute.String("error.message", err.Error()),
		))
	} else {
		span.AddEvent("mcp.server.stopped")
	}

	return err
}

// SessionContext returns the context carrying the session root span.
// Returns the given ctx if the session has not started yet.
func (s *Server) SessionContext(ctx context.Context) context.Context {
	s.sessionMu.RLock()
	defer s.sessionMu.RUnlock()
	if s.sessionCtx != nil {
		return s.sessionCtx
	}
	return ctx
}

// incrementToolCount tracks the number of registered tools for the session span.
func (s *Server) incrementToolCount() {
	s.toolCount++
}

package mcputil

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

// defaultHTTPClient is used by TracedHTTP when no custom client is provided.
var defaultHTTPClient = &http.Client{Timeout: 30 * time.Second}

// HTTPOption configures a TracedHTTP call.
type HTTPOption func(*httpOpts)

type httpOpts struct {
	client  *http.Client
	headers map[string]string
	body    io.Reader
	maxBody int64 // max response body to read (0 = 10 MiB default)
}

// WithHTTPClient sets a custom HTTP client for the request.
func WithHTTPClient(c *http.Client) HTTPOption {
	return func(o *httpOpts) { o.client = c }
}

// WithHeader adds a header to the outgoing request.
func WithHeader(key, value string) HTTPOption {
	return func(o *httpOpts) {
		if o.headers == nil {
			o.headers = make(map[string]string)
		}
		o.headers[key] = value
	}
}

// WithBody sets the request body.
func WithBody(r io.Reader) HTTPOption {
	return func(o *httpOpts) { o.body = r }
}

// WithMaxResponseBody sets the maximum response body size to read.
// Default is 10 MiB.
func WithMaxResponseBody(n int64) HTTPOption {
	return func(o *httpOpts) { o.maxBody = n }
}

// TracedHTTP performs an HTTP request wrapped in an OpenTelemetry span.
//
// The span is named "http.<METHOD>" (e.g. "http.GET") and records:
//   - http.method           — GET, POST, etc.
//   - http.url              — full request URL (query params stripped for safety)
//   - http.status_code      — response status code
//   - http.duration_ms      — request duration in milliseconds
//   - http.response_size    — response body size in bytes
//
// Returns the response body bytes and status code, or an error.
func TracedHTTP(ctx context.Context, method, url string, opts ...HTTPOption) ([]byte, int, error) {
	o := &httpOpts{
		client:  defaultHTTPClient,
		maxBody: 10 << 20, // 10 MiB
	}
	for _, opt := range opts {
		opt(o)
	}

	ctx, span := Tracer.Start(ctx, "http."+strings.ToUpper(method), trace.WithAttributes(
		attribute.String("http.method", strings.ToUpper(method)),
		attribute.String("http.url", sanitizeURL(url)),
	))
	defer span.End()

	start := time.Now()

	req, err := http.NewRequestWithContext(ctx, method, url, o.body)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, 0, fmt.Errorf("create request: %w", err)
	}

	for k, v := range o.headers {
		req.Header.Set(k, v)
	}

	resp, err := o.client.Do(req)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, err.Error())
		return nil, 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(io.LimitReader(resp.Body, o.maxBody))
	elapsed := time.Since(start)

	span.SetAttributes(
		attribute.Int("http.status_code", resp.StatusCode),
		attribute.Float64("http.duration_ms", float64(elapsed.Milliseconds())),
		attribute.Int("http.response_size", len(body)),
	)

	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "read body: "+err.Error())
		return body, resp.StatusCode, fmt.Errorf("read body: %w", err)
	}

	if resp.StatusCode >= 400 {
		span.SetStatus(codes.Error, fmt.Sprintf("HTTP %d", resp.StatusCode))
	}

	return body, resp.StatusCode, nil
}

// sanitizeURL removes query parameters to avoid leaking tokens/secrets in traces.
func sanitizeURL(raw string) string {
	if i := strings.IndexByte(raw, '?'); i >= 0 {
		return raw[:i] + "?..."
	}
	return raw
}

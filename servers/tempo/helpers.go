package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

// ── MCP helpers ─────────────────────────────────────────────────────

func jsonResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcputil.ErrResult("json marshal: %v", err)
	}
	return mcputil.TextResult(string(data))
}

func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

// ── Tempo HTTP client (traced) ──────────────────────────────────────

var tempoHTTPClient = &http.Client{Timeout: 30 * time.Second}

// tempoGet performs a traced GET request to Tempo's HTTP API.
func tempoGet(ctx context.Context, path string) ([]byte, error) {
	body, status, err := mcputil.TracedHTTP(ctx, "GET", tempoURL+path,
		mcputil.WithHTTPClient(tempoHTTPClient),
		mcputil.WithMaxResponseBody(50<<20), // 50 MiB cap
	)
	if err != nil {
		return nil, fmt.Errorf("tempo request failed: %w", err)
	}
	if status >= 400 {
		return nil, fmt.Errorf("HTTP %d: %s", status, string(body))
	}
	return body, nil
}

// tempoSearch runs a TraceQL search query.
func tempoSearch(ctx context.Context, query string, limit int, start, end time.Time) (*searchResponse, error) {
	params := url.Values{}
	params.Set("q", query)
	if limit > 0 {
		params.Set("limit", strconv.Itoa(limit))
	} else {
		params.Set("limit", "200")
	}
	if !start.IsZero() {
		params.Set("start", strconv.FormatInt(start.Unix(), 10))
	}
	if !end.IsZero() {
		params.Set("end", strconv.FormatInt(end.Unix(), 10))
	}

	data, err := tempoGet(ctx, "/api/search?"+params.Encode())
	if err != nil {
		return nil, err
	}

	var resp searchResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing search response: %w", err)
	}
	return &resp, nil
}

// tempoGetTrace fetches a full trace by ID from Tempo.
func tempoGetTrace(ctx context.Context, traceID string) (*otlpTraceResponse, error) {
	data, err := tempoGet(ctx, "/api/traces/"+traceID)
	if err != nil {
		return nil, err
	}

	var resp otlpTraceResponse
	if err := json.Unmarshal(data, &resp); err != nil {
		return nil, fmt.Errorf("parsing trace response: %w", err)
	}
	return &resp, nil
}

// ── Time parsing ────────────────────────────────────────────────────

// parseTimeRange parses human-friendly time range into start/end times.
// Supports: "1h", "6h", "24h", "72h", "7d", or RFC3339 timestamps.
func parseTimeRange(since string) (start, end time.Time) {
	end = time.Now()
	if since == "" {
		since = "24h"
	}

	// Try duration format: "1h", "24h", "7d"
	if strings.HasSuffix(since, "d") {
		if days, err := strconv.Atoi(strings.TrimSuffix(since, "d")); err == nil {
			start = end.Add(-time.Duration(days) * 24 * time.Hour)
			return
		}
	}
	if d, err := time.ParseDuration(since); err == nil {
		start = end.Add(-d)
		return
	}

	// Try RFC3339
	if t, err := time.Parse(time.RFC3339, since); err == nil {
		start = t
		return
	}

	// Default: 24h
	start = end.Add(-24 * time.Hour)
	return
}

// ── Span extraction helpers ─────────────────────────────────────────

// extractSpans flattens the OTLP trace response into a flat slice of spans
// with resource attributes attached.
func extractSpans(trace *otlpTraceResponse) []flatSpan {
	var spans []flatSpan
	for _, rs := range trace.Batches {
		resAttrs := attrMap(rs.Resource.Attributes)
		for _, ss := range rs.ScopeSpans {
			for _, s := range ss.Spans {
				startNano, _ := strconv.ParseInt(s.StartTimeUnixNano, 10, 64)
				endNano, _ := strconv.ParseInt(s.EndTimeUnixNano, 10, 64)
				fs := flatSpan{
					TraceID:       s.TraceID,
					SpanID:        s.SpanID,
					ParentSpanID:  s.ParentSpanID,
					Name:          s.Name,
					Kind:          s.Kind,
					StartTimeUnix: startNano,
					EndTimeUnix:   endNano,
					Attributes:    attrMap(s.Attributes),
					ResAttributes: resAttrs,
					Events:        s.Events,
					Status:        s.Status,
					Links:         s.Links,
				}
				if fs.EndTimeUnix > fs.StartTimeUnix {
					fs.DurationMs = float64(fs.EndTimeUnix-fs.StartTimeUnix) / 1e6
				}
				spans = append(spans, fs)
			}
		}
	}
	return spans
}

// findRootSpan finds the root span (agent.prompt) in a flat span list.
func findRootSpan(spans []flatSpan) *flatSpan {
	for i := range spans {
		if spans[i].Name == "agent.prompt" {
			return &spans[i]
		}
	}
	// Fallback: find span with no parent
	for i := range spans {
		if spans[i].ParentSpanID == "" {
			return &spans[i]
		}
	}
	if len(spans) > 0 {
		return &spans[0]
	}
	return nil
}

// filterSpansByName returns spans matching a name prefix.
func filterSpansByName(spans []flatSpan, prefix string) []flatSpan {
	var out []flatSpan
	for _, s := range spans {
		if strings.HasPrefix(s.Name, prefix) {
			out = append(out, s)
		}
	}
	return out
}

// attrMap converts OTLP attribute arrays to a simple string map.
func attrMap(attrs []otlpAttribute) map[string]string {
	m := make(map[string]string, len(attrs))
	for _, a := range attrs {
		if a.Value.StringValue != "" {
			m[a.Key] = a.Value.StringValue
		} else if a.Value.IntValue != "" {
			m[a.Key] = a.Value.IntValue
		} else if a.Value.DoubleValue != 0 {
			m[a.Key] = fmt.Sprintf("%.2f", a.Value.DoubleValue)
		} else if a.Value.BoolValue {
			m[a.Key] = "true"
		}
	}
	return m
}

// getAttr retrieves an attribute from either span or resource attributes.
func getAttr(s *flatSpan, key string) string {
	if v, ok := s.Attributes[key]; ok {
		return v
	}
	if v, ok := s.ResAttributes[key]; ok {
		return v
	}
	return ""
}

// attrInt parses an attribute as int64, returns 0 on failure.
func attrInt(s *flatSpan, key string) int64 {
	v := getAttr(s, key)
	if v == "" {
		return 0
	}
	n, _ := strconv.ParseInt(v, 10, 64)
	return n
}

// ── Statistics helpers ──────────────────────────────────────────────

func percentile(sorted []float64, p float64) float64 {
	if len(sorted) == 0 {
		return 0
	}
	idx := p / 100.0 * float64(len(sorted)-1)
	lower := int(idx)
	upper := lower + 1
	if upper >= len(sorted) {
		return sorted[len(sorted)-1]
	}
	weight := idx - float64(lower)
	return sorted[lower]*(1-weight) + sorted[upper]*weight
}

func sortedFloat64s(vals []float64) []float64 {
	out := make([]float64, len(vals))
	copy(out, vals)
	sort.Float64s(out)
	return out
}

func avg(vals []float64) float64 {
	if len(vals) == 0 {
		return 0
	}
	var sum float64
	for _, v := range vals {
		sum += v
	}
	return sum / float64(len(vals))
}

func sumInt64(vals []int64) int64 {
	var s int64
	for _, v := range vals {
		s += v
	}
	return s
}

func fmtDuration(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	if ms < 60000 {
		return fmt.Sprintf("%.1fs", ms/1000)
	}
	return fmt.Sprintf("%.1fm", ms/60000)
}

// isErrorStatus checks if a Tempo status code string indicates an error.
// Tempo returns status codes as strings like "STATUS_CODE_ERROR".
func isErrorStatus(code string) bool {
	return code == "STATUS_CODE_ERROR" || code == "2"
}

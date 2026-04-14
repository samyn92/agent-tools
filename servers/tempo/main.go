/*
MCP Tool: Tempo

An MCP stdio server providing Grafana Tempo trace analysis capabilities.
Uses net/http to query Tempo's HTTP API directly — no external dependencies
beyond the MCP SDK. Self-contained binary for agent trace analysis.

Requires: TEMPO_URL env var (default: http://tempo.observability.svc.cluster.local:3200)

Tools: tempo_search, tempo_get, tempo_agent_stats, tempo_slow_tools,

	tempo_errors, tempo_compare
*/
package main

import (
	"context"
	"log"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/otelutil"
)

var tempoURL string

func main() {
	shutdown, _ := otelutil.Init(context.Background(), "mcp-tool-tempo")
	defer func() { shutdown(context.Background()) }()

	tempoURL = strings.TrimRight(or(os.Getenv("TEMPO_URL"), "http://tempo.observability.svc.cluster.local:3200"), "/")

	server := mcp.NewServer(
		&mcp.Implementation{Name: "tempo-tools", Version: "0.1.0"},
		nil,
	)

	add(server, "tempo_search",
		"Search traces by agent name, time range, and status. Returns summarized trace metadata with durations, step counts, and token usage. Use this as the starting point to find traces worth investigating.",
		handleSearch)

	add(server, "tempo_get",
		"Fetch a full trace by traceID. Returns the complete span tree with durations, tool calls, token usage, errors, and memory operations. Use after tempo_search to drill into a specific execution.",
		handleGet)

	add(server, "tempo_agent_stats",
		"Compute aggregate performance statistics for an agent over a time window. Returns avg/p50/p95/p99 prompt duration, avg steps per prompt, avg token usage, error rate, slowest tools, and most-called tools. This is your main optimization analysis tool.",
		handleAgentStats)

	add(server, "tempo_slow_tools",
		"Find the slowest tool calls across all agents (or a specific agent) in a time window. Identifies performance bottlenecks at the tool level.",
		handleSlowTools)

	add(server, "tempo_errors",
		"Find all error spans (tool failures, model fallbacks, timeouts) across agents in a time window. Groups errors by type, agent, and tool. Use this to identify reliability issues.",
		handleErrors)

	add(server, "tempo_compare",
		"Compare two traces side-by-side by traceID. Shows differences in step count, token usage, duration, and tool call patterns. Use after applying an optimization to measure impact.",
		handleCompare)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.Fatal(err)
	}
}

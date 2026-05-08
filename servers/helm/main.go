/*
MCP Tool: helm

An MCP stdio server providing Helm chart inspection and values diffing.
Shells out to the helm CLI for OCI registry access and release queries.

Tools:
  - helm_show_values:    Show default values.yaml for a chart version
  - helm_show_chart:     Show Chart.yaml metadata (version, appVersion, deps)
  - helm_values_diff:    Diff default values between two chart versions
  - helm_get_values:     Get values of a deployed Helm release
  - helm_drift:          Compare release values against chart defaults (shows overrides & drift)
*/
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

var log *slog.Logger

func main() {
	// Helm needs writable dirs for cache/config/data. Agent pods run as
	// nonroot with read-only rootfs, so point everything to /tmp.
	for _, env := range []string{"HELM_CACHE_HOME", "HELM_CONFIG_HOME", "HELM_DATA_HOME"} {
		if os.Getenv(env) == "" {
			os.Setenv(env, "/tmp/helm")
		}
	}

	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-helm")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	helmBin = resolveHelm()

	server := mcputil.NewServer("helm", "0.1.0")

	mcputil.AddToolTo(server, "helm_show_values", "Show default values.yaml for a chart at a given version from an OCI or repo source.", handleShowValues)
	mcputil.AddToolTo(server, "helm_show_chart", "Show Chart.yaml metadata (version, appVersion, description, dependencies).", handleShowChart)
	mcputil.AddToolTo(server, "helm_values_diff", "Diff default values between two versions of the same chart. Shows added, removed, and changed defaults.", handleValuesDiff)
	mcputil.AddToolTo(server, "helm_get_values", "Get the user-supplied values of a deployed Helm release.", handleGetValues)
	mcputil.AddToolTo(server, "helm_drift", "Compare a release's effective values against the chart's defaults. Shows overrides, missing new defaults, and removed keys.", handleDrift)

	mcputil.Ready("mcp-tool-helm")
	defer mcputil.NotReady("mcp-tool-helm")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
	}
}

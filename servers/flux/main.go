/*
MCP Tool: Flux

An MCP stdio server providing Flux CD GitOps operations.
Shells out to the flux CLI for maximum compatibility with
kubeconfig, RBAC, and Flux controller communication.

Supports two modes controlled by the MODE environment variable:
  - readonly  (default): get, check, stats, logs, events, trace, tree, diff, export, debug, version
  - readwrite:           all readonly tools + reconcile, suspend, resume, delete

Requires: flux in PATH, valid kubeconfig, Flux controllers installed in cluster.
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
	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-flux")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	fluxBin = resolveFlux()

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	server := mcputil.NewServer("flux-"+mode, "0.1.0", mcputil.WithMode(mode))

	// ── Readonly tools (always registered) ──
	mcputil.AddToolTo(server, "flux_get", "Get Flux resources: all, helmreleases, kustomizations, sources (git/helm/oci/bucket/chart), alerts, receivers, images.", handleGet)
	mcputil.AddToolTo(server, "flux_check", "Check Flux installation prerequisites and controller health.", handleCheck)
	mcputil.AddToolTo(server, "flux_stats", "Show Flux resource reconciliation statistics.", handleStats)
	mcputil.AddToolTo(server, "flux_logs", "Show Flux controller logs. Supports filtering by kind, name, namespace, and log level.", handleLogs)
	mcputil.AddToolTo(server, "flux_events", "Show Flux events for resources (Kustomization, HelmRelease, GitRepository, etc.).", handleEvents)
	mcputil.AddToolTo(server, "flux_trace", "Trace a Kubernetes object to its Flux source through the reconciliation chain.", handleTrace)
	mcputil.AddToolTo(server, "flux_tree", "Show the Flux resource tree for a kustomization (child resources and their status).", handleTree)
	mcputil.AddToolTo(server, "flux_diff", "Diff a kustomization against the live cluster state to preview changes.", handleDiff)
	mcputil.AddToolTo(server, "flux_export", "Export Flux resources as YAML manifests for backup or migration.", handleExport)
	mcputil.AddToolTo(server, "flux_debug", "Debug a helmrelease or kustomization by showing computed values and rendered manifests.", handleDebug)
	mcputil.AddToolTo(server, "flux_version", "Show Flux CLI and controller versions.", handleVersion)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		mcputil.AddToolTo(server, "flux_reconcile", "Trigger a reconciliation for a Flux resource (helmrelease, kustomization, source, etc.).", handleReconcile, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "flux_suspend", "Suspend reconciliation for a Flux resource.", handleSuspend, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "flux_resume", "Resume reconciliation for a suspended Flux resource.", handleResume, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "flux_delete", "Delete a Flux resource (helmrelease, kustomization, source, alert, receiver, etc.).", handleDelete, mcputil.WithInputOutput())
	}

	mcputil.Ready("mcp-tool-flux")
	defer mcputil.NotReady("mcp-tool-flux")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
	}
}

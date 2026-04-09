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
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	fluxBin = resolveFlux()

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	serverName := "flux-" + mode
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: "0.1.0"},
		nil,
	)

	// ── Readonly tools (always registered) ──
	add(server, "flux_get", "Get Flux resources: all, helmreleases, kustomizations, sources (git/helm/oci/bucket/chart), alerts, receivers, images.", handleGet)
	add(server, "flux_check", "Check Flux installation prerequisites and controller health.", handleCheck)
	add(server, "flux_stats", "Show Flux resource reconciliation statistics.", handleStats)
	add(server, "flux_logs", "Show Flux controller logs. Supports filtering by kind, name, namespace, and log level.", handleLogs)
	add(server, "flux_events", "Show Flux events for resources (Kustomization, HelmRelease, GitRepository, etc.).", handleEvents)
	add(server, "flux_trace", "Trace a Kubernetes object to its Flux source through the reconciliation chain.", handleTrace)
	add(server, "flux_tree", "Show the Flux resource tree for a kustomization (child resources and their status).", handleTree)
	add(server, "flux_diff", "Diff a kustomization against the live cluster state to preview changes.", handleDiff)
	add(server, "flux_export", "Export Flux resources as YAML manifests for backup or migration.", handleExport)
	add(server, "flux_debug", "Debug a helmrelease or kustomization by showing computed values and rendered manifests.", handleDebug)
	add(server, "flux_version", "Show Flux CLI and controller versions.", handleVersion)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		add(server, "flux_reconcile", "Trigger a reconciliation for a Flux resource (helmrelease, kustomization, source, etc.).", handleReconcile)
		add(server, "flux_suspend", "Suspend reconciliation for a Flux resource.", handleSuspend)
		add(server, "flux_resume", "Resume reconciliation for a suspended Flux resource.", handleResume)
		add(server, "flux_delete", "Delete a Flux resource (helmrelease, kustomization, source, alert, receiver, etc.).", handleDelete)
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

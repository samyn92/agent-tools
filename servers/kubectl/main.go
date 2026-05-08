/*
MCP Tool: kubectl

An MCP stdio server providing Kubernetes kubectl operations.
Shells out to the kubectl CLI for maximum compatibility with
kubeconfig, auth plugins, RBAC, etc.

Supports two modes controlled by the MODE environment variable:
  - readonly  (default): get, describe, logs, top, events, api-resources, explain
  - readwrite:           all readonly tools + exec, apply, delete, run, cp, rollout, scale, label, annotate

Requires: kubectl in PATH, valid kubeconfig.
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

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-kubectl")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	kubectlBin = resolveKubectl()

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	server := mcputil.NewServer("kubectl-"+mode, version, mcputil.WithMode(mode))

	// ── Readonly tools (always registered) ──
	mcputil.AddToolTo(server, "kubectl_get", "Get one or many resources. Supports all resource types, label selectors, field selectors, and output formats.", handleGet)
	mcputil.AddToolTo(server, "kubectl_describe", "Show detailed information about a resource including events, conditions, and status.", handleDescribe)
	mcputil.AddToolTo(server, "kubectl_logs", "Print container logs from a pod. Supports follow, tail, previous, since, and multi-container pods.", handleLogs)
	mcputil.AddToolTo(server, "kubectl_top", "Display resource usage (CPU/memory) for pods or nodes. Requires metrics-server.", handleTop)
	mcputil.AddToolTo(server, "kubectl_events", "List cluster events, optionally filtered by namespace or resource.", handleEvents)
	mcputil.AddToolTo(server, "kubectl_api_resources", "List available API resource types on the cluster.", handleAPIResources)
	mcputil.AddToolTo(server, "kubectl_explain", "Describe the fields of a resource type (e.g. pod.spec.containers).", handleExplain)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		mcputil.AddToolTo(server, "kubectl_exec", "Execute a command in a running container.", handleExec, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_apply", "Apply a Kubernetes manifest (YAML/JSON) to create or update resources.", handleApply, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_delete", "Delete resources by name, label selector, or from a manifest.", handleDelete, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_run", "Run a one-off pod with the given image and command.", handleRun, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_cp", "Copy files between containers and the local filesystem.", handleCp)
		mcputil.AddToolTo(server, "kubectl_rollout", "Manage rollouts: status, history, undo, restart.", handleRollout, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_scale", "Scale a deployment, replicaset, or statefulset.", handleScale, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_label", "Add or update labels on a resource.", handleLabel, mcputil.WithInputOutput())
		mcputil.AddToolTo(server, "kubectl_annotate", "Add or update annotations on a resource.", handleAnnotate, mcputil.WithInputOutput())
	}

	mcputil.Ready("mcp-tool-kubectl")
	defer mcputil.NotReady("mcp-tool-kubectl")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
	}
}

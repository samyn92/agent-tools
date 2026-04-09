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
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	kubectlBin = resolveKubectl()

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	serverName := "kubectl-" + mode
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: "0.1.0"},
		nil,
	)

	// ── Readonly tools (always registered) ──
	add(server, "kubectl_get", "Get one or many resources. Supports all resource types, label selectors, field selectors, and output formats.", handleGet)
	add(server, "kubectl_describe", "Show detailed information about a resource including events, conditions, and status.", handleDescribe)
	add(server, "kubectl_logs", "Print container logs from a pod. Supports follow, tail, previous, since, and multi-container pods.", handleLogs)
	add(server, "kubectl_top", "Display resource usage (CPU/memory) for pods or nodes. Requires metrics-server.", handleTop)
	add(server, "kubectl_events", "List cluster events, optionally filtered by namespace or resource.", handleEvents)
	add(server, "kubectl_api_resources", "List available API resource types on the cluster.", handleAPIResources)
	add(server, "kubectl_explain", "Describe the fields of a resource type (e.g. pod.spec.containers).", handleExplain)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		add(server, "kubectl_exec", "Execute a command in a running container.", handleExec)
		add(server, "kubectl_apply", "Apply a Kubernetes manifest (YAML/JSON) to create or update resources.", handleApply)
		add(server, "kubectl_delete", "Delete resources by name, label selector, or from a manifest.", handleDelete)
		add(server, "kubectl_run", "Run a one-off pod with the given image and command.", handleRun)
		add(server, "kubectl_cp", "Copy files between containers and the local filesystem.", handleCp)
		add(server, "kubectl_rollout", "Manage rollouts: status, history, undo, restart.", handleRollout)
		add(server, "kubectl_scale", "Scale a deployment, replicaset, or statefulset.", handleScale)
		add(server, "kubectl_label", "Add or update labels on a resource.", handleLabel)
		add(server, "kubectl_annotate", "Add or update annotations on a resource.", handleAnnotate)
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

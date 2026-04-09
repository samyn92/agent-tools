/*
MCP Tool: kube-explore (Intent-based Kubernetes Discovery & Operations)

An MCP stdio server providing intent-based Kubernetes tools that answer
the actual question in ONE call instead of forcing multi-step workflows.

Uses client-go directly — no kubectl dependency, self-contained binary.

Designed to be packaged as an OCI artifact and loaded by any
MCP-compatible agent runtime (Fantasy, Crush, Claude Code, etc.)

Intent Tools (new — the whole point):
  - kube_find      Fuzzy search across all namespaces and resource types
  - kube_health    Full cluster health snapshot in one call
  - kube_inspect   Deep single-resource inspection with logs, events, owner chain
  - kube_topology  Relationship graph for a workload
  - kube_diff      Compare desired vs live state
  - kube_logs      Enhanced logs with crash detection and fuzzy resolution
  - kube_exec      Exec with fuzzy pod resolution

Legacy Tools (kept for backward compat, agents sometimes need exact access):
  - kube_apply     Server-side apply YAML manifest
*/
package main

import (
	"context"
	"log"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	initClients()

	server := mcp.NewServer(
		&mcp.Implementation{Name: "kube-explore", Version: "0.1.0"},
		nil,
	)

	// ============================================================
	// Intent-based smart tools (the reason this binary exists)
	// ============================================================

	addTool(server, "kube_find",
		"Fuzzy search across ALL namespaces and ALL resource types. "+
			"Matches against name, labels, annotations, and status conditions. "+
			"Accepts partial names, label fragments, and status keywords "+
			"(failing, broken, unhealthy, pending, crash, oom). "+
			"Returns a ranked list with namespace, kind, name, status, age, "+
			"and relevance score. One call replaces 3-8 kubectl calls.",
		handleFind)

	addTool(server, "kube_health",
		"Full cluster health snapshot in ONE call. Returns: unhealthy pods, "+
			"pending PVCs, failed jobs (last 24h), recent error events (last 30m), "+
			"node conditions, and resource pressure warnings. "+
			"Optionally scoped to a single namespace. "+
			"One call replaces 5-10 kubectl calls across multiple resource types.",
		handleHealth)

	addTool(server, "kube_inspect",
		"Deep inspection of a single resource. Returns: full spec, status, "+
			"conditions, events, logs (if pod/job), owner chain "+
			"(Pod->ReplicaSet->Deployment), and related resources "+
			"(Services, Ingresses, PVCs, ConfigMaps, Secrets). "+
			"Accepts fuzzy names — no need to know the exact name or namespace. "+
			"One call replaces 3-4 kubectl calls (get + describe + logs + events).",
		handleInspect)

	addTool(server, "kube_topology",
		"Relationship graph for a workload. Shows: "+
			"Deployment -> ReplicaSet -> Pods, plus network (Services, Ingresses), "+
			"storage (PVCs), and config (ConfigMaps, Secrets) references. "+
			"Returns a tree structure. Accepts fuzzy names. "+
			"One call replaces 5+ kubectl calls traversing owner references manually.",
		handleTopology)

	addTool(server, "kube_diff",
		"Compare desired vs live state. Provide an inline YAML manifest as "+
			"the desired source and see field-level structural diff against "+
			"the live cluster state. Useful for drift detection.",
		handleDiff)

	addTool(server, "kube_logs",
		"Enhanced pod log fetching. Auto-detects crashlooping containers "+
			"and fetches both previous + current logs. Highlights error/panic/fatal "+
			"lines. Supports fuzzy pod name resolution — no need to know the "+
			"exact pod name. One call replaces 2-3 kubectl calls.",
		handleLogs)

	addTool(server, "kube_exec",
		"Execute a command in a pod. Enhanced: resolves fuzzy pod names first, "+
			"so you don't need the exact pod name or namespace.",
		handleExec)

	// ============================================================
	// Legacy tool (backward compat)
	// ============================================================

	addTool(server, "kube_apply",
		"Apply a YAML or JSON manifest using server-side apply. "+
			"Creates or updates resources. Supports multi-document YAML.",
		handleApply)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

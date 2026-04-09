/*
logs.go — kube_logs enhanced tool handler.

Enhanced log fetching: auto-detects crashlooping containers, fetches both
previous + current logs, highlights error/panic/fatal lines, supports
fuzzy pod name resolution.
*/
package main

import (
	"context"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type logsInput struct {
	Name      string `json:"name" jsonschema_description:"Pod name (fuzzy matching supported — partial names work)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit to search all namespaces)"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (for multi-container pods)"`
	Lines     int64  `json:"lines,omitempty" jsonschema_description:"Number of lines to fetch (default: 100)"`
	Since     string `json:"since,omitempty" jsonschema_description:"Only return logs newer than a duration (e.g. 5m, 1h, 24h)"`
	Previous  bool   `json:"previous,omitempty" jsonschema_description:"Show logs from previous container instance (auto-detected for crash-looping pods)"`
}

func handleLogs(ctx context.Context, _ *mcp.CallToolRequest, input logsInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return errResult("'name' is required"), nil, nil
	}

	lines := input.Lines
	if lines <= 0 {
		lines = 100
	}

	// Fuzzy-resolve the pod
	obj, kind, err := fuzzyFindOne(ctx, input.Name, "Pod", input.Namespace)
	if err != nil {
		return errResult("Pod not found: %v", err), nil, nil
	}
	if kind != "Pod" {
		return errResult("Found %s/%s but expected a Pod", kind, obj.GetName()), nil, nil
	}

	podName := obj.GetName()
	namespace := obj.GetNamespace()
	restarts := getRestartCount(obj)
	crashLooping := restarts > 0 && isPodUnhealthy(obj)

	response := LogsResponse{
		Pod:          podName,
		Namespace:    namespace,
		Container:    input.Container,
		CrashLooping: crashLooping,
		Restarts:     restarts,
	}

	// Fetch current logs
	current, err := getPodLogs(ctx, namespace, podName, input.Container, lines, false)
	if err != nil {
		return errResult("Error fetching logs for %s/%s: %v", namespace, podName, err), nil, nil
	}

	totalLines := strings.Count(current, "\n")
	response.TotalLines = totalLines
	response.Truncated = totalLines >= int(lines)
	response.CurrentLogs = current

	// Extract error lines
	errorLines := highlightErrorLines(current, 30)
	if errorLines != "" {
		response.ErrorLines = errorLines
	}

	// Auto-fetch previous logs if crash-looping or explicitly requested
	if crashLooping || input.Previous {
		previous, err := getPodLogs(ctx, namespace, podName, input.Container, 50, true)
		if err == nil && previous != "" {
			response.PreviousLogs = previous
		}
	}

	return jsonMarshalResult(response), nil, nil
}

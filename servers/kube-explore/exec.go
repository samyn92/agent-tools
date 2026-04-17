/*
exec.go — kube_exec tool handler.

Execute a command in a pod with fuzzy pod name resolution.
Enhanced: resolves fuzzy pod name first, then exec.
*/
package main

import (
	"context"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

type execInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name (fuzzy matching supported)"`
	Command   string `json:"command" jsonschema_description:"Command to execute (passed to sh -c)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit to search all namespaces)"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (for multi-container pods)"`
}

func handleExec(ctx context.Context, _ *mcp.CallToolRequest, input execInput) (*mcp.CallToolResult, any, error) {
	if input.Pod == "" {
		return mcputil.ErrResult("'pod' is required"), nil, nil
	}
	if input.Command == "" {
		return mcputil.ErrResult("'command' is required"), nil, nil
	}

	// Fuzzy-resolve the pod
	obj, kind, err := fuzzyFindOne(ctx, input.Pod, "Pod", input.Namespace)
	if err != nil {
		return mcputil.ErrResult("Pod not found: %v", err), nil, nil
	}
	if kind != "Pod" {
		return mcputil.ErrResult("Found %s/%s but expected a Pod", kind, obj.GetName()), nil, nil
	}

	podName := obj.GetName()
	namespace := obj.GetNamespace()

	// Apply a 300s timeout to prevent runaway exec commands
	execCtx, cancel := context.WithTimeout(ctx, 300*time.Second)
	defer cancel()

	output, err := execInPod(execCtx, namespace, podName, input.Container, input.Command)
	if err != nil {
		if execCtx.Err() == context.DeadlineExceeded {
			return mcputil.ErrResult("Exec in %s/%s timed out after 300s\n%s", namespace, podName, output), nil, nil
		}
		return mcputil.ErrResult("Exec in %s/%s failed: %v\n%s", namespace, podName, err, output), nil, nil
	}

	return mcputil.TextResult(output), nil, nil
}

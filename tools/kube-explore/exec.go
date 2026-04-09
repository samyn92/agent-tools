/*
exec.go — kube_exec tool handler.

Execute a command in a pod with fuzzy pod name resolution.
Enhanced: resolves fuzzy pod name first, then exec.
*/
package main

import (
	"context"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

type execInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name (fuzzy matching supported)"`
	Command   string `json:"command" jsonschema_description:"Command to execute (passed to sh -c)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit to search all namespaces)"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (for multi-container pods)"`
}

func handleExec(ctx context.Context, _ *mcp.CallToolRequest, input execInput) (*mcp.CallToolResult, any, error) {
	if input.Pod == "" {
		return errResult("'pod' is required"), nil, nil
	}
	if input.Command == "" {
		return errResult("'command' is required"), nil, nil
	}

	// Fuzzy-resolve the pod
	obj, kind, err := fuzzyFindOne(ctx, input.Pod, "Pod", input.Namespace)
	if err != nil {
		return errResult("Pod not found: %v", err), nil, nil
	}
	if kind != "Pod" {
		return errResult("Found %s/%s but expected a Pod", kind, obj.GetName()), nil, nil
	}

	podName := obj.GetName()
	namespace := obj.GetNamespace()

	output, err := execInPod(ctx, namespace, podName, input.Container, input.Command)
	if err != nil {
		return errResult("Exec in %s/%s failed: %v\n%s", namespace, podName, err, output), nil, nil
	}

	return textResult(output), nil, nil
}

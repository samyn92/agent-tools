package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

// kubectlBin is the resolved path to the kubectl binary.
// Determined once at startup by resolveKubectl.
var kubectlBin string

// resolveKubectl finds the kubectl binary. It checks the directory
// containing this binary first (for OCI artifact co-bundling), then
// falls back to PATH lookup.
func resolveKubectl() string {
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), "kubectl")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	if p, err := exec.LookPath("kubectl"); err == nil {
		return p
	}
	return "kubectl"
}

// kube runs kubectl with the given arguments and returns the result.
// The context carries the parent span from the tool handler for trace correlation.
func kube(ctx context.Context, args ...string) *mcp.CallToolResult {
	return kubeWithTimeout(ctx, 30*time.Second, args...)
}

// kubeWithTimeout runs kubectl with the given arguments and a timeout.
func kubeWithTimeout(ctx context.Context, timeout time.Duration, args ...string) *mcp.CallToolResult {
	r := mcputil.TracedExecWithTimeout(ctx, timeout, kubectlBin, args...)

	cmdLine := "$ kubectl " + fmt.Sprintf("%v", args)

	if r.TimedOut {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s\ntimed out after %s\n%s", cmdLine, timeout, r.Output)}},
			IsError: true,
		}
	}
	if r.Err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s\n%s\n%s", cmdLine, r.Err, r.Output)}},
			IsError: true,
		}
	}
	output := r.Output
	if output == "" {
		output = "(no output)"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: cmdLine + "\n" + output}},
	}
}

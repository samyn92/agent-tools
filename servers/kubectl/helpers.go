package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// kubectlBin is the resolved path to the kubectl binary.
// Determined once at startup by resolveKubectl.
var kubectlBin string

// resolveKubectl finds the kubectl binary. It checks the directory
// containing this binary first (for OCI artifact co-bundling), then
// falls back to PATH lookup.
func resolveKubectl() string {
	// Check sibling of our own binary: /tools/kubectl/bin/kubectl
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), "kubectl")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	// Fall back to PATH
	if p, err := exec.LookPath("kubectl"); err == nil {
		return p
	}
	return "kubectl" // let it fail with a clear error
}

// add registers a tool with the MCP server using typed input.
func add[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, h)
}

// kube runs kubectl with the given arguments and returns the result.
func kube(args ...string) *mcp.CallToolResult {
	return kubeWithTimeout(30*time.Second, args...)
}

// kubeWithTimeout runs kubectl with the given arguments and a timeout.
func kubeWithTimeout(timeout time.Duration, args ...string) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, kubectlBin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("kubectl %s timed out after %s\n%s", strings.Join(args, " "), timeout, string(out))}},
			IsError: true,
		}
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("kubectl %s failed: %s\n%s", strings.Join(args, " "), err, string(out))}},
			IsError: true,
		}
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "(no output)"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

// textResult creates a successful text result.
func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// errResult creates an error result with formatting.
func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

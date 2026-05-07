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

var helmBin string

func resolveHelm() string {
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), "helm")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	if p, err := exec.LookPath("helm"); err == nil {
		return p
	}
	return "helm"
}

func helm(ctx context.Context, args ...string) *mcp.CallToolResult {
	return helmWithTimeout(ctx, 60*time.Second, args...)
}

func helmWithTimeout(ctx context.Context, timeout time.Duration, args ...string) *mcp.CallToolResult {
	r := mcputil.TracedExecWithTimeout(ctx, timeout, helmBin, args...)

	cmdLine := "$ helm " + fmt.Sprintf("%v", args)

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

// helmOutput runs helm and returns just stdout (for internal use in diff logic).
func helmOutput(ctx context.Context, args ...string) (string, error) {
	r := mcputil.TracedExecWithTimeout(ctx, 60*time.Second, helmBin, args...)
	if r.Err != nil {
		return "", fmt.Errorf("%s: %s", r.Err, r.Output)
	}
	return r.Output, nil
}

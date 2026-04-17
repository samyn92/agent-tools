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
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

// fluxBin is the resolved path to the flux binary.
var fluxBin string

// resolveFlux finds the flux binary. Checks sibling directory first
// (for OCI artifact co-bundling), then falls back to PATH lookup.
func resolveFlux() string {
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), "flux")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	if p, err := exec.LookPath("flux"); err == nil {
		return p
	}
	return "flux"
}

// flux runs the flux CLI with the given arguments and returns the result.
// The context carries the parent span from the tool handler for trace correlation.
func flux(ctx context.Context, args ...string) *mcp.CallToolResult {
	return fluxWithTimeout(ctx, 30*time.Second, args...)
}

// fluxWithTimeout runs the flux CLI with the given arguments and a timeout.
func fluxWithTimeout(ctx context.Context, timeout time.Duration, args ...string) *mcp.CallToolResult {
	r := mcputil.TracedExecWithTimeout(ctx, timeout, fluxBin, args...)

	cmdLine := "$ flux " + strings.Join(args, " ")

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

// appendNamespace adds -n or --all-namespaces to the args.
func appendNamespace(args []string, ns string) []string {
	if ns == "" {
		return args
	}
	if ns == "-A" || strings.EqualFold(ns, "all") {
		return append(args, "--all-namespaces")
	}
	return append(args, "-n", ns)
}

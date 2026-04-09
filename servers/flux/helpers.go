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

// add registers a tool with the MCP server using typed input.
func add[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, h)
}

// flux runs the flux CLI with the given arguments and returns the result.
func flux(args ...string) *mcp.CallToolResult {
	return fluxWithTimeout(30*time.Second, args...)
}

// fluxWithTimeout runs the flux CLI with the given arguments and a timeout.
func fluxWithTimeout(timeout time.Duration, args ...string) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	cmdLine := "$ flux " + strings.Join(args, " ")

	cmd := exec.CommandContext(ctx, fluxBin, args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if ctx.Err() == context.DeadlineExceeded {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s\ntimed out after %s\n%s", cmdLine, timeout, string(out))}},
			IsError: true,
		}
	}
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("%s\n%s\n%s", cmdLine, err, strings.TrimSpace(string(out)))}},
			IsError: true,
		}
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "(no output)"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: cmdLine + "\n" + text}},
	}
}

// errResult creates an error result with formatting.
func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
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

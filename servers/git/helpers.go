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

// gitBin is the resolved path to the git binary.
var gitBin string

// workspace is the base directory for git operations.
var workspace string

// defaultTimeout for git commands (clone/push/pull get longer).
const (
	defaultTimeout = 30 * time.Second
	networkTimeout = 120 * time.Second // clone, push, pull, fetch
)

// resolveGit finds the git binary. Checks the sibling directory first
// (for OCI artifact co-bundling), then falls back to PATH.
func resolveGit() string {
	self, err := os.Executable()
	if err == nil {
		sibling := filepath.Join(filepath.Dir(self), "git")
		if _, err := os.Stat(sibling); err == nil {
			return sibling
		}
	}
	if p, err := exec.LookPath("git"); err == nil {
		return p
	}
	return "git"
}

// add registers a tool with the MCP server using typed input.
func add[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, h)
}

// resolveCwd resolves a working directory relative to WORKSPACE.
// Returns an error if the resolved path escapes the workspace sandbox.
func resolveCwd(cwd string) (string, error) {
	if cwd == "" {
		return workspace, nil
	}
	var resolved string
	if filepath.IsAbs(cwd) {
		resolved = filepath.Clean(cwd)
	} else {
		resolved = filepath.Clean(filepath.Join(workspace, cwd))
	}
	// Ensure the resolved path is within the workspace sandbox.
	wsClean := filepath.Clean(workspace)
	if resolved != wsClean && !strings.HasPrefix(resolved, wsClean+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workspace sandbox %q", cwd, workspace)
	}
	return resolved, nil
}

// git runs a git command with the default timeout and returns an MCP result.
func git(cwd string, args ...string) *mcp.CallToolResult {
	return gitWithTimeout(defaultTimeout, cwd, args...)
}

// gitNetwork runs a git command with the network timeout (for clone, push, pull).
func gitNetwork(cwd string, args ...string) *mcp.CallToolResult {
	return gitWithTimeout(networkTimeout, cwd, args...)
}

// gitWithTimeout runs a git command with the given timeout.
func gitWithTimeout(timeout time.Duration, cwd string, args ...string) *mcp.CallToolResult {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	dir, err := resolveCwd(cwd)
	if err != nil {
		return errResult("blocked: %s", err)
	}

	cmdLine := "$ git " + strings.Join(args, " ")

	cmd := exec.CommandContext(ctx, gitBin, args...)
	cmd.Dir = dir
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

// or returns val if non-empty, otherwise def.
func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

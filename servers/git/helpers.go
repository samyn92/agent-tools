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
	wsClean := filepath.Clean(workspace)
	if resolved != wsClean && !strings.HasPrefix(resolved, wsClean+string(filepath.Separator)) {
		return "", fmt.Errorf("path %q escapes workspace sandbox %q", cwd, workspace)
	}
	return resolved, nil
}

// git runs a git command with the default timeout and returns an MCP result.
func git(ctx context.Context, cwd string, args ...string) *mcp.CallToolResult {
	return gitWithTimeout(ctx, defaultTimeout, cwd, args...)
}

// gitNetwork runs a git command with the network timeout (for clone, push, pull).
func gitNetwork(ctx context.Context, cwd string, args ...string) *mcp.CallToolResult {
	return gitWithTimeout(ctx, networkTimeout, cwd, args...)
}

// gitWithTimeout runs a git command with the given timeout, traced via mcputil.
func gitWithTimeout(ctx context.Context, timeout time.Duration, cwd string, args ...string) *mcp.CallToolResult {
	dir, err := resolveCwd(cwd)
	if err != nil {
		return mcputil.ErrResult("blocked: %s", err)
	}

	// Prepend -C <dir> so git runs in the correct directory.
	fullArgs := args
	if dir != "" {
		fullArgs = append([]string{"-C", dir}, args...)
	}

	cmdLine := "$ git " + strings.Join(args, " ")

	r := mcputil.TracedExecWithTimeout(ctx, timeout, gitBin, fullArgs...)

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

// or returns val if non-empty, otherwise def.
func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

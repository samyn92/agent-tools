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
//
// Behavior:
//   - Empty cwd → workspace root (most common, recommended for agents).
//   - Relative cwd → joined with workspace.
//   - Absolute cwd inside workspace → used as-is.
//   - Absolute cwd outside workspace → returns an actionable error that
//     names the workspace and suggests how to recover. We deliberately do
//     NOT silently rewrite, because the agent may be confused about which
//     repo it's operating on; surfacing the mistake is safer than guessing.
//
// The error message is written for the agent (LLM) consumer, not the
// human operator, so it explicitly tells the agent what to do next.
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
		return "", fmt.Errorf(
			"cwd %q is outside the sandboxed git workspace %q. "+
				"This MCP git tool can only operate inside %q. "+
				"To fix: omit the cwd parameter (defaults to the workspace root), "+
				"or pass a path under %q. The pre-cloned repository is typically at %q.",
			cwd, wsClean, wsClean, wsClean, filepath.Join(wsClean, "repo"),
		)
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

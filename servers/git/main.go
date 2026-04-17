/*
MCP Tool: Git

An MCP stdio server providing Git operations.
Shells out to the git CLI for maximum compatibility with auth,
SSH keys, credential helpers, GPG signing, etc.

Supports two modes controlled by the MODE environment variable:
  - readonly  (default): status, diff, log, branch_list, show
  - readwrite:           all readonly tools + add, commit, push, pull, branch, clone, clone_or_pull

Requires: git in PATH.
*/
package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

var log *slog.Logger

func main() {
	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-git")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	gitBin = resolveGit()
	workspace = os.Getenv("WORKSPACE")
	if workspace == "" {
		workspace = "/workspace"
	}

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	server := mcputil.NewServer("git-"+mode, "0.2.0", mcputil.WithMode(mode))

	// ── Readonly tools (always registered) ──
	registerReadonlyTools(server)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		registerReadwriteTools(server)
	}

	mcputil.Ready("mcp-tool-git")
	defer mcputil.NotReady("mcp-tool-git")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
	}
}

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
	"log"
	"os"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

func main() {
	gitBin = resolveGit()
	workspace = os.Getenv("WORKSPACE")
	if workspace == "" {
		workspace = "/workspace"
	}

	mode := os.Getenv("MODE")
	if mode == "" {
		mode = "readonly"
	}

	serverName := "git-" + mode
	server := mcp.NewServer(
		&mcp.Implementation{Name: serverName, Version: "0.2.0"},
		nil,
	)

	// ── Readonly tools (always registered) ──
	registerReadonlyTools(server)

	// ── Readwrite tools (only in readwrite mode) ──
	if mode == "readwrite" {
		registerReadwriteTools(server)
	}

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

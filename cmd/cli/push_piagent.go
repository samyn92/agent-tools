package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/samyn92/agent-tools/pkg/piagent"
	"github.com/spf13/cobra"
)

var (
	pushPiagentTag       string
	pushPiagentPlainHTTP bool
)

var pushPiagentCmd = &cobra.Command{
	Use:   "piagent [directory]",
	Short: "Push a Pi agent as an OCI artifact",
	Long: `Package a Pi agent directory as an OCI artifact and push it to a registry.

The directory must contain an index.ts or index.js file that exports the agent's
tools and configuration. Optional files (utils.ts, package.json, etc.) are included
automatically. node_modules, .git, and dist directories are excluded.

Examples:
  agent-tools push piagent ./my-agent/ -t ghcr.io/myorg/pr-classifier:v1.0.0
  agent-tools push piagent . -t ghcr.io/myorg/pr-classifier:latest

The pushed artifact can be referenced in a PiAgent CRD:
  spec:
    source:
      oci:
        ref: ghcr.io/myorg/pr-classifier:v1.0.0

Media types:
  Artifact type: application/vnd.agents.io.piagent.v1
  Code layer:    application/vnd.agents.io.piagent.code.v1.tar+gzip`,
	Args: cobra.ExactArgs(1),
	RunE: runPushPiagent,
}

func init() {
	pushPiagentCmd.Flags().StringVarP(&pushPiagentTag, "tag", "t", "", "OCI reference to push to (required)")
	pushPiagentCmd.Flags().BoolVar(&pushPiagentPlainHTTP, "plain-http", false, "Use HTTP instead of HTTPS for the registry")

	pushPiagentCmd.MarkFlagRequired("tag")
}

func runPushPiagent(cmd *cobra.Command, args []string) error {
	sourceDir := args[0]

	// Resolve to absolute path
	absDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}

	// Validate the source directory exists
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", sourceDir)
	}

	// Create pusher
	pusher := piagent.NewPusher()

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nPush cancelled")
		cancel()
	}()

	// Run the push
	opts := piagent.PushOptions{
		Tag:       pushPiagentTag,
		SourceDir: absDir,
		PlainHTTP: pushPiagentPlainHTTP,
	}

	if err := pusher.Push(ctx, opts); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}

	return nil
}

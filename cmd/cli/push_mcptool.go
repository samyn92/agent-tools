package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/samyn92/agent-tools/pkg/mcppush"
	"github.com/spf13/cobra"
)

var (
	pushMCPToolTag       string
	pushMCPToolPlainHTTP bool
)

var pushMCPToolCmd = &cobra.Command{
	Use:   "mcp-tool [directory]",
	Short: "Push an MCP tool server as an OCI artifact",
	Long: `Package an MCP tool directory as an OCI artifact and push it to a registry.

The directory must contain:
  - manifest.json  (name, command, transport)
  - bin/           (compiled server binary)

The MCP server binary communicates via stdio and is loaded by any
MCP-compatible agent runtime (Fantasy, Crush, Claude Code, etc.)

Examples:
  # Build the Go binary first, then push:
  cd tools/kubernetes && CGO_ENABLED=0 go build -o dist/bin/kubernetes .
  cp manifest.json dist/
  agent-tools push mcp-tool ./dist/ -t ghcr.io/myorg/tools/kubernetes:0.1.0

The pushed artifact can be referenced in an Agent CRD:
  spec:
    toolRefs:
      - name: kubernetes
        ociRef:
          ref: ghcr.io/myorg/tools/kubernetes:0.1.0

Media types:
  Artifact type: application/vnd.agents.io.mcp-tool.v1
  Code layer:    application/vnd.agents.io.mcp-tool.code.v1.tar+gzip`,
	Args: cobra.ExactArgs(1),
	RunE: runPushMCPTool,
}

func init() {
	pushMCPToolCmd.Flags().StringVarP(&pushMCPToolTag, "tag", "t", "", "OCI reference to push to (required)")
	pushMCPToolCmd.Flags().BoolVar(&pushMCPToolPlainHTTP, "plain-http", false, "Use HTTP instead of HTTPS for the registry")
	pushMCPToolCmd.MarkFlagRequired("tag")
}

func runPushMCPTool(cmd *cobra.Command, args []string) error {
	sourceDir := args[0]
	absDir, err := filepath.Abs(sourceDir)
	if err != nil {
		return fmt.Errorf("resolving path: %w", err)
	}
	info, err := os.Stat(absDir)
	if err != nil {
		return fmt.Errorf("cannot access %s: %w", sourceDir, err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", sourceDir)
	}

	pusher := mcppush.NewPusher()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nPush cancelled")
		cancel()
	}()

	opts := mcppush.PushOptions{
		Tag:       pushMCPToolTag,
		SourceDir: absDir,
		PlainHTTP: pushMCPToolPlainHTTP,
	}

	if err := pusher.Push(ctx, opts); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	return nil
}

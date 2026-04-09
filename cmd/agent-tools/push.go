package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/samyn92/agent-tools/internal/oci"
	"github.com/spf13/cobra"
)

var (
	pushTag       string
	pushPlainHTTP bool
)

var pushCmd = &cobra.Command{
	Use:   "push [directory]",
	Short: "Push an MCP tool server as an OCI artifact",
	Long: `Package an MCP tool directory as an OCI artifact and push it to a registry.

The directory must contain:
  - manifest.json  (name, command, transport)
  - bin/           (compiled server binary)

The MCP server binary communicates via stdio and is loaded by any
MCP-compatible agent runtime (Fantasy, Crush, Claude Code, etc.)

Examples:
  # Build the Go binary first, then push:
  cd servers/kube-explore && CGO_ENABLED=0 go build -o dist/bin/kube-explore .
  cp manifest.json dist/
  agent-tools push ./dist/ -t ghcr.io/myorg/agent-tools/kube-explore:0.2.0

The pushed artifact can be referenced in an Agent CRD:
  spec:
    toolRefs:
      - name: kube-explore
        ociRef:
          ref: ghcr.io/myorg/agent-tools/kube-explore:0.2.0

Media types:
  Artifact type: application/vnd.agents.io.mcp-tool.v1
  Code layer:    application/vnd.agents.io.mcp-tool.code.v1.tar+gzip`,
	Args: cobra.ExactArgs(1),
	RunE: runPush,
}

func init() {
	pushCmd.Flags().StringVarP(&pushTag, "tag", "t", "", "OCI reference to push to (required)")
	pushCmd.Flags().BoolVar(&pushPlainHTTP, "plain-http", false, "Use HTTP instead of HTTPS for the registry")
	pushCmd.MarkFlagRequired("tag")
}

func runPush(cmd *cobra.Command, args []string) error {
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

	pusher := oci.NewPusher()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nPush cancelled")
		cancel()
	}()

	return pusher.Push(ctx, oci.PushOptions{
		Tag:       pushTag,
		SourceDir: absDir,
		PlainHTTP: pushPlainHTTP,
	})
}

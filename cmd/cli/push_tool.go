package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/samyn92/agent-tools/pkg/toolpush"
	"github.com/spf13/cobra"
)

var (
	pushToolTag       string
	pushToolPlainHTTP bool
)

var pushToolCmd = &cobra.Command{
	Use:   "tool [directory]",
	Short: "Push a tool package as an OCI artifact",
	Long: `Package a tool directory as an OCI artifact and push it to a registry.

The directory must contain an index.ts or index.js file that exports an
AgentTool[] array. The runner will dynamically import this at Job startup.

Examples:
  agent-tools push tool ./tools/git/ -t ghcr.io/myorg/agent-tools/git:0.1.0
  agent-tools push tool . -t ghcr.io/myorg/agent-tools/kubectl:latest

The pushed artifact can be referenced in a PiAgent CRD:
  spec:
    toolRefs:
      - ref: ghcr.io/myorg/agent-tools/git:0.1.0

Media types:
  Artifact type: application/vnd.agents.io.tool.v1
  Code layer:    application/vnd.agents.io.tool.code.v1.tar+gzip`,
	Args: cobra.ExactArgs(1),
	RunE: runPushTool,
}

func init() {
	pushToolCmd.Flags().StringVarP(&pushToolTag, "tag", "t", "", "OCI reference to push to (required)")
	pushToolCmd.Flags().BoolVar(&pushToolPlainHTTP, "plain-http", false, "Use HTTP instead of HTTPS for the registry")
	pushToolCmd.MarkFlagRequired("tag")
}

func runPushTool(cmd *cobra.Command, args []string) error {
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

	pusher := toolpush.NewPusher()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nPush cancelled")
		cancel()
	}()

	opts := toolpush.PushOptions{
		Tag:       pushToolTag,
		SourceDir: absDir,
		PlainHTTP: pushToolPlainHTTP,
	}

	if err := pusher.Push(ctx, opts); err != nil {
		return fmt.Errorf("push failed: %w", err)
	}
	return nil
}

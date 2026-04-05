// Package main provides the agent-tools CLI.
// This CLI is used to push OCI artifacts (tool packages and Pi agents) to registries.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "agent-tools",
	Short: "Agent Tools CLI for pushing OCI artifacts",
	Long: `Agent Tools CLI provides utilities for packaging and pushing tool packages
and Pi agents as OCI artifacts.

Push a tool package:
  agent-tools push tool ./tools/git/ -t ghcr.io/myorg/agent-tools/git:0.1.0

Push a Pi agent:
  agent-tools push piagent ./my-agent/ -t ghcr.io/myorg/agent:v1.0.0

For more information, see https://github.com/samyn92/agent-tools`,
	Version: version,
}

func main() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Add subcommands
	rootCmd.AddCommand(pushCmd)
}

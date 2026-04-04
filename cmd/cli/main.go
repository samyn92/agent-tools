// Package main provides the agent-tools CLI.
// This CLI is used to build tool images, manage the catalog, and other utilities.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "agent-tools",
	Short: "Agent Tools CLI for building and managing agent tool images",
	Long: `Agent Tools CLI provides utilities for building tool images and packaging
Pi agents for the Agent Operator ecosystem.

Build tool images:
  agent-tools tool build -f tool.yaml -t myimage:latest

Push Pi agent artifacts:
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
	rootCmd.AddCommand(toolCmd)
	rootCmd.AddCommand(pushCmd)
}

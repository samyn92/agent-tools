package main

import (
	"github.com/spf13/cobra"
)

var toolCmd = &cobra.Command{
	Use:   "tool",
	Short: "Manage tool images",
	Long: `Commands for building and managing tool images.

Build a tool image from a manifest:
  agent-tools tool build -f tool.yaml -t myimage:latest

Build with inline options:
  agent-tools tool build --apk github-cli --binary gh -t myimage:latest`,
}

func init() {
	// Add subcommands
	toolCmd.AddCommand(toolBuildCmd)
}

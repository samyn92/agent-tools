// Package main provides the agent-tools CLI for packaging and pushing
// MCP tool servers as OCI artifacts to container registries.
package main

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
)

var version = "dev"

var rootCmd = &cobra.Command{
	Use:   "agent-tools",
	Short: "Package and push MCP tool servers as OCI artifacts",
	Long: `Agent Tools CLI packages MCP tool servers as OCI artifacts and pushes them
to OCI-compliant container registries.

Example:
  agent-tools push ./servers/kube-explore/dist/ -t ghcr.io/myorg/agent-tools/kube-explore:0.2.0

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
	rootCmd.AddCommand(pushCmd)
}

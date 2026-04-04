package main

import (
	"github.com/spf13/cobra"
)

var pushCmd = &cobra.Command{
	Use:   "push",
	Short: "Push artifacts to an OCI registry",
	Long: `Commands for packaging and pushing artifacts to OCI-compliant container registries.

Push a Pi agent:
  agent-tools push piagent ./my-agent/ -t ghcr.io/myorg/pr-classifier:v1.0.0

Push a tool package:
  agent-tools push tool ./tools/git/ -t ghcr.io/myorg/agent-tools/git:0.1.0`,
}

func init() {
	pushCmd.AddCommand(pushPiagentCmd)
	pushCmd.AddCommand(pushToolCmd)
}

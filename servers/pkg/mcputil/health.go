package mcputil

import (
	"fmt"
	"os"
	"path/filepath"
)

const defaultReadyDir = "/tmp"

// Ready writes a readiness file signaling that the MCP tool server has
// completed initialization and is ready to handle requests.
//
// The file is written to /tmp/<serverName>.ready by default.
// The operator's AgentTool health probes can check for this file
// to determine tool readiness without protocol-level health checks.
//
// Usage:
//
//	mcputil.Init(ctx, "mcp-tool-github")
//	server := mcputil.NewServer("github-tools", "0.1.0")
//	mcputil.AddTool(server.Server, ...)
//	mcputil.Ready("mcp-tool-github")  // signal readiness before Run()
//	server.Run(ctx, &mcp.StdioTransport{})
func Ready(serverName string) error {
	path := filepath.Join(defaultReadyDir, serverName+".ready")
	return os.WriteFile(path, []byte(fmt.Sprintf("ready:%s\n", serverName)), 0644)
}

// ReadyAt writes a readiness file at a custom path.
func ReadyAt(path string) error {
	return os.WriteFile(path, []byte("ready\n"), 0644)
}

// NotReady removes the readiness file. Call on shutdown if needed.
func NotReady(serverName string) {
	path := filepath.Join(defaultReadyDir, serverName+".ready")
	os.Remove(path)
}

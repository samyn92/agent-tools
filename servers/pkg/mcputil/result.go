package mcputil

import (
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// TextResult creates a successful MCP tool result with text content.
func TextResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

// ErrResult creates an error MCP tool result with a formatted message.
func ErrResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

// truncate shortens s to at most n bytes, appending "…" if truncated.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

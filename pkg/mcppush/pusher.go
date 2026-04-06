// Package mcppush provides OCI artifact packaging for MCP tool servers.
// MCP tools are compiled binaries that speak the MCP stdio protocol,
// compatible with any MCP-aware agent runtime (Fantasy, Crush, etc.)
package mcppush

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/samyn92/agent-tools/internal/ocipush"
)

const (
	// ArtifactType is the OCI artifact type for MCP tool packages.
	ArtifactType = "application/vnd.agents.io.mcp-tool.v1"

	// LayerMediaType is the media type for the MCP tool layer (tar+gzip).
	LayerMediaType = "application/vnd.agents.io.mcp-tool.code.v1.tar+gzip"

	// ConfigMediaType is the media type for the artifact config blob.
	ConfigMediaType = "application/vnd.agents.io.mcp-tool.config.v1+json"
)

// Pusher packages an MCP tool directory as an OCI artifact and pushes it.
type Pusher struct {
	Output      io.Writer
	ErrorOutput io.Writer
}

// NewPusher creates a new Pusher with default outputs.
func NewPusher() *Pusher {
	return &Pusher{
		Output:      os.Stdout,
		ErrorOutput: os.Stderr,
	}
}

// PushOptions configures a push operation.
type PushOptions struct {
	Tag       string
	SourceDir string
	PlainHTTP bool
}

// Push packages the MCP tool directory and pushes it as an OCI artifact.
// The directory must contain manifest.json and a bin/ directory with the server binary.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) error {
	if err := validateMCPSource(opts.SourceDir); err != nil {
		return fmt.Errorf("invalid MCP tool source: %w", err)
	}

	inner := ocipush.NewPusher(ocipush.ArtifactConfig{
		ArtifactType:    ArtifactType,
		LayerMediaType:  LayerMediaType,
		ConfigMediaType: ConfigMediaType,
		Label:           "MCP tool package",
		SkipValidation:  true,
	})
	inner.Output = p.Output
	inner.ErrorOutput = p.ErrorOutput

	return inner.Push(ctx, ocipush.PushOptions{
		Tag:       opts.Tag,
		SourceDir: opts.SourceDir,
		PlainHTTP: opts.PlainHTTP,
	})
}

// validateMCPSource ensures the directory contains manifest.json and a binary.
func validateMCPSource(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	// Must have manifest.json
	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return fmt.Errorf("manifest.json required: %w", err)
	}

	// Must have bin/ directory with at least one file
	binDir := filepath.Join(dir, "bin")
	if _, err := os.Stat(binDir); err != nil {
		return fmt.Errorf("bin/ directory required: %w", err)
	}

	entries, err := os.ReadDir(binDir)
	if err != nil {
		return fmt.Errorf("reading bin/: %w", err)
	}
	if len(entries) == 0 {
		return fmt.Errorf("bin/ directory must contain at least one binary")
	}

	return nil
}

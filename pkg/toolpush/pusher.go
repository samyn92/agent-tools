// Package toolpush provides OCI artifact packaging for Pi Agent tool packages.
// Tool packages are TypeScript/JavaScript modules that export an AgentTool[] array.
package toolpush

import (
	"context"
	"io"
	"os"

	"github.com/samyn92/agent-tools/internal/ocipush"
)

const (
	// ArtifactType is the OCI artifact type for Pi agent tool packages.
	ArtifactType = "application/vnd.agents.io.tool.v1"

	// LayerMediaType is the media type for the tool code layer (tar+gzip).
	LayerMediaType = "application/vnd.agents.io.tool.code.v1.tar+gzip"

	// ConfigMediaType is the media type for the artifact config blob.
	ConfigMediaType = "application/vnd.agents.io.tool.config.v1+json"
)

// Pusher packages a tool directory as an OCI artifact and pushes it to a registry.
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
	// Tag is the full OCI reference (e.g., "ghcr.io/myorg/agent-tools/git:0.1.0")
	Tag string

	// SourceDir is the path to the tool directory to package
	SourceDir string

	// PlainHTTP uses HTTP instead of HTTPS for the registry
	PlainHTTP bool
}

// Push packages the tool source directory and pushes it as an OCI artifact.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) error {
	inner := ocipush.NewPusher(ocipush.ArtifactConfig{
		ArtifactType:    ArtifactType,
		LayerMediaType:  LayerMediaType,
		ConfigMediaType: ConfigMediaType,
		Label:           "tool package",
	})
	inner.Output = p.Output
	inner.ErrorOutput = p.ErrorOutput

	return inner.Push(ctx, ocipush.PushOptions{
		Tag:       opts.Tag,
		SourceDir: opts.SourceDir,
		PlainHTTP: opts.PlainHTTP,
	})
}

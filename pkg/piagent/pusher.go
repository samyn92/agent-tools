// Package piagent provides OCI artifact packaging for Pi Agents.
// Pi agents are packaged as OCI artifacts so that the agent-operator
// can pull and run them as Kubernetes Jobs.
package piagent

import (
	"context"
	"io"
	"os"

	"github.com/samyn92/agent-tools/internal/ocipush"
)

const (
	// ArtifactType is the OCI artifact type for Pi agent packages.
	ArtifactType = "application/vnd.agents.io.piagent.v1"

	// LayerMediaType is the media type for the agent code layer (tar+gzip).
	LayerMediaType = "application/vnd.agents.io.piagent.code.v1.tar+gzip"

	// ConfigMediaType is the media type for the artifact config blob.
	ConfigMediaType = "application/vnd.agents.io.piagent.config.v1+json"
)

// Pusher packages a Pi agent directory as an OCI artifact and pushes it to a registry.
type Pusher struct {
	Output      io.Writer
	ErrorOutput io.Writer
}

// NewPusher creates a new Pusher.
func NewPusher() *Pusher {
	return &Pusher{
		Output:      os.Stdout,
		ErrorOutput: os.Stderr,
	}
}

// PushOptions configures a push operation.
type PushOptions struct {
	// Tag is the full OCI reference (e.g., "ghcr.io/myorg/pr-classifier:v1.0.0")
	Tag string

	// SourceDir is the path to the agent directory to package
	SourceDir string

	// PlainHTTP uses HTTP instead of HTTPS for the registry
	PlainHTTP bool
}

// Push packages the agent source directory and pushes it as an OCI artifact.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) error {
	inner := ocipush.NewPusher(ocipush.ArtifactConfig{
		ArtifactType:    ArtifactType,
		LayerMediaType:  LayerMediaType,
		ConfigMediaType: ConfigMediaType,
		Label:           "Pi agent",
	})
	inner.Output = p.Output
	inner.ErrorOutput = p.ErrorOutput

	return inner.Push(ctx, ocipush.PushOptions{
		Tag:       opts.Tag,
		SourceDir: opts.SourceDir,
		PlainHTTP: opts.PlainHTTP,
	})
}

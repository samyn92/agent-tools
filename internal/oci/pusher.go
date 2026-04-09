// Package oci provides OCI artifact packaging and push logic for MCP tool servers.
//
// It packages a directory (containing manifest.json + bin/) as a tar+gzip OCI
// artifact with custom media types and pushes it to an OCI-compliant registry
// using ORAS.
package oci

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

const (
	// ArtifactType is the OCI artifact type for MCP tool packages.
	ArtifactType = "application/vnd.agents.io.mcp-tool.v1"

	// LayerMediaType is the media type for the MCP tool code layer (tar+gzip).
	LayerMediaType = "application/vnd.agents.io.mcp-tool.code.v1.tar+gzip"

	// ConfigMediaType is the media type for the artifact config blob.
	ConfigMediaType = "application/vnd.agents.io.mcp-tool.config.v1+json"
)

// PushOptions configures a push operation.
type PushOptions struct {
	// Tag is the full OCI reference (e.g., "ghcr.io/myorg/agent-tools/kubernetes:0.1.0").
	Tag string

	// SourceDir is the path to the directory to package.
	SourceDir string

	// PlainHTTP uses HTTP instead of HTTPS for the registry.
	PlainHTTP bool
}

// Pusher packages an MCP tool directory as an OCI artifact and pushes it to a registry.
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

// Push validates, packages, and pushes the source directory as an OCI artifact.
// The directory must contain manifest.json and a bin/ directory with at least one binary.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) error {
	if opts.Tag == "" {
		return fmt.Errorf("tag is required")
	}
	if opts.SourceDir == "" {
		return fmt.Errorf("source directory is required")
	}

	if err := validateSource(opts.SourceDir); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}

	fmt.Fprintf(p.Output, "Packaging MCP tool from %s\n", opts.SourceDir)

	// Create the tar+gzip layer from the source directory
	layerData, err := CreateTarGzip(opts.SourceDir)
	if err != nil {
		return fmt.Errorf("creating archive: %w", err)
	}

	fmt.Fprintf(p.Output, "Archive size: %d bytes\n", len(layerData))

	// Parse the reference
	ref, err := ParseReference(opts.Tag)
	if err != nil {
		return fmt.Errorf("parsing reference: %w", err)
	}

	// Build the OCI artifact in an in-memory store
	store := memory.New()

	// Push the code layer
	layerDesc, err := pushBlob(ctx, store, LayerMediaType, layerData)
	if err != nil {
		return fmt.Errorf("storing layer: %w", err)
	}

	// Push empty config (required by OCI spec)
	configDesc, err := pushBlob(ctx, store, ConfigMediaType, []byte("{}"))
	if err != nil {
		return fmt.Errorf("storing config: %w", err)
	}

	// Build the manifest
	manifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: ArtifactType,
		Config:       configDesc,
		Layers:       []ocispec.Descriptor{layerDesc},
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	manifestDesc, err := pushBlob(ctx, store, ocispec.MediaTypeImageManifest, manifestData)
	if err != nil {
		return fmt.Errorf("storing manifest: %w", err)
	}

	// Tag the manifest
	if err := store.Tag(ctx, manifestDesc, ref.Tag); err != nil {
		return fmt.Errorf("tagging manifest: %w", err)
	}

	// Create remote repository and push
	repo, err := remote.NewRepository(ref.Repository)
	if err != nil {
		return fmt.Errorf("creating remote repository: %w", err)
	}

	repo.PlainHTTP = opts.PlainHTTP

	// Set up auth client
	creds := LoadDockerCredentials(ref.Registry)
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(ref.Registry, creds),
	}

	fmt.Fprintf(p.Output, "Pushing to %s\n", opts.Tag)

	_, err = oras.Copy(ctx, store, ref.Tag, repo, ref.Tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("pushing artifact: %w", err)
	}

	fmt.Fprintf(p.Output, "Successfully pushed %s\n", opts.Tag)
	return nil
}

// validateSource ensures the directory contains manifest.json and bin/ with a binary.
func validateSource(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	if _, err := os.Stat(filepath.Join(dir, "manifest.json")); err != nil {
		return fmt.Errorf("manifest.json required: %w", err)
	}

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

// pushBlob pushes a blob to the in-memory store and returns its descriptor.
func pushBlob(ctx context.Context, store *memory.Store, mediaType string, data []byte) (ocispec.Descriptor, error) {
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digest.FromBytes(data),
		Size:      int64(len(data)),
	}

	if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, nil
}

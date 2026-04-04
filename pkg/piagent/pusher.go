// Package piagent provides OCI artifact packaging and push logic for Pi Agents.
// Pi agents are packaged as OCI artifacts with a custom media type so that the
// agent-operator can pull and run them as Kubernetes Jobs.
package piagent

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
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
	// Output is where to write progress output (default: os.Stdout)
	Output io.Writer

	// ErrorOutput is where to write error output (default: os.Stderr)
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
	if opts.Tag == "" {
		return fmt.Errorf("tag is required")
	}
	if opts.SourceDir == "" {
		return fmt.Errorf("source directory is required")
	}

	// Validate the source directory
	if err := ValidateSource(opts.SourceDir); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}

	fmt.Fprintf(p.Output, "Packaging Pi agent from %s\n", opts.SourceDir)

	// Create the tar+gzip layer from the source directory
	layerData, err := createTarGzip(opts.SourceDir)
	if err != nil {
		return fmt.Errorf("creating archive: %w", err)
	}

	fmt.Fprintf(p.Output, "Archive size: %d bytes\n", len(layerData))

	// Parse the reference to get registry, repo, and tag
	ref, err := parseReference(opts.Tag)
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

	// Push an empty config (required by OCI spec)
	configData := []byte("{}")
	configDesc, err := pushBlob(ctx, store, ConfigMediaType, configData)
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

	manifestData, err := encodeJSON(manifest)
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	manifestDesc, err := pushBlob(ctx, store, ocispec.MediaTypeImageManifest, manifestData)
	if err != nil {
		return fmt.Errorf("storing manifest: %w", err)
	}

	// Tag the manifest in the memory store
	if err := store.Tag(ctx, manifestDesc, ref.tag); err != nil {
		return fmt.Errorf("tagging manifest: %w", err)
	}

	// Create the remote repository and push
	repo, err := remote.NewRepository(ref.repository)
	if err != nil {
		return fmt.Errorf("creating remote repository: %w", err)
	}

	repo.PlainHTTP = opts.PlainHTTP

	// Use default Docker credential store
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(ref.registry, auth.EmptyCredential),
	}

	// Try to get credentials from Docker config
	dockerCreds := loadDockerCredentials(ref.registry)
	if dockerCreds != (auth.Credential{}) {
		repo.Client = &auth.Client{
			Client:     retry.DefaultClient,
			Cache:      auth.NewCache(),
			Credential: auth.StaticCredential(ref.registry, dockerCreds),
		}
	}

	fmt.Fprintf(p.Output, "Pushing to %s\n", opts.Tag)

	_, err = oras.Copy(ctx, store, ref.tag, repo, ref.tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("pushing artifact: %w", err)
	}

	fmt.Fprintf(p.Output, "Successfully pushed %s\n", opts.Tag)
	return nil
}

// ValidateSource validates that the source directory is a valid Pi agent.
func ValidateSource(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	// Must contain index.ts or index.js
	hasIndex := false
	for _, name := range []string{"index.ts", "index.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		return fmt.Errorf("source directory must contain index.ts or index.js")
	}

	return nil
}

// createTarGzip creates a tar.gz archive of the given directory.
// Files are stored relative to the directory root.
func createTarGzip(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		// Get relative path
		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		// Skip the root directory entry
		if relPath == "." {
			return nil
		}

		// Skip node_modules and hidden directories (except .env-like files)
		base := filepath.Base(relPath)
		if info.IsDir() && (base == "node_modules" || base == ".git" || base == "dist") {
			return filepath.SkipDir
		}

		// Skip common non-essential files
		if !info.IsDir() && shouldSkipFile(base) {
			return nil
		}

		// Create tar header
		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		// Use forward slashes and relative paths
		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

		// Write file content
		if !info.IsDir() {
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()

			if _, err := io.Copy(tw, f); err != nil {
				return err
			}
		}

		return nil
	})

	if err != nil {
		return nil, err
	}

	if err := tw.Close(); err != nil {
		return nil, err
	}
	if err := gw.Close(); err != nil {
		return nil, err
	}

	return buf.Bytes(), nil
}

// shouldSkipFile returns true for files that should not be included in the artifact.
func shouldSkipFile(name string) bool {
	skip := []string{
		".DS_Store",
		"Thumbs.db",
		".gitignore",
		".npmrc",
		"package-lock.json",
		"yarn.lock",
		"pnpm-lock.yaml",
		"tsconfig.tsbuildinfo",
	}
	lower := strings.ToLower(name)
	for _, s := range skip {
		if lower == strings.ToLower(s) {
			return true
		}
	}
	return false
}

// referenceInfo holds parsed OCI reference components.
type referenceInfo struct {
	registry   string // e.g., "ghcr.io"
	repository string // e.g., "ghcr.io/myorg/pr-classifier"
	tag        string // e.g., "v1.0.0"
}

// parseReference parses a full OCI reference like "ghcr.io/myorg/repo:tag".
func parseReference(ref string) (referenceInfo, error) {
	// Split tag
	tag := "latest"
	repo := ref
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		// Make sure this isn't a port separator (e.g., localhost:5000/repo)
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			tag = afterColon
			repo = ref[:idx]
		}
	}

	// Extract registry (first path component)
	parts := strings.SplitN(repo, "/", 2)
	if len(parts) < 2 {
		return referenceInfo{}, fmt.Errorf("invalid reference %q: must be in format registry/repo[:tag]", ref)
	}

	return referenceInfo{
		registry:   parts[0],
		repository: repo,
		tag:        tag,
	}, nil
}

// pushBlob pushes a blob to the in-memory store and returns its descriptor.
func pushBlob(ctx context.Context, store *memory.Store, mediaType string, data []byte) (ocispec.Descriptor, error) {
	desc := ocispec.Descriptor{
		MediaType: mediaType,
		Digest:    digestOf(data),
		Size:      int64(len(data)),
	}

	if err := store.Push(ctx, desc, bytes.NewReader(data)); err != nil {
		return ocispec.Descriptor{}, err
	}

	return desc, nil
}

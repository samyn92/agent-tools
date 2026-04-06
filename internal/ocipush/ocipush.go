// Package ocipush provides shared OCI artifact packaging and push logic.
// It is used by both the tool package pusher and the Pi agent pusher to
// avoid code duplication.
package ocipush

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	"github.com/opencontainers/image-spec/specs-go"
	ocispec "github.com/opencontainers/image-spec/specs-go/v1"
	"oras.land/oras-go/v2"
	"oras.land/oras-go/v2/content/memory"
	"oras.land/oras-go/v2/registry/remote"
	"oras.land/oras-go/v2/registry/remote/auth"
	"oras.land/oras-go/v2/registry/remote/retry"
)

// ArtifactConfig defines the media types for a specific artifact kind.
type ArtifactConfig struct {
	// ArtifactType is the OCI artifact type (e.g., "application/vnd.agents.io.tool.v1").
	ArtifactType string

	// LayerMediaType is the media type for the code layer (tar+gzip).
	LayerMediaType string

	// ConfigMediaType is the media type for the config blob.
	ConfigMediaType string

	// Label is a human-readable label for log messages (e.g., "tool package", "Pi agent").
	Label string
}

// PushOptions configures a push operation.
type PushOptions struct {
	// Tag is the full OCI reference (e.g., "ghcr.io/myorg/agent-tools/git:0.1.0").
	Tag string

	// SourceDir is the path to the directory to package.
	SourceDir string

	// PlainHTTP uses HTTP instead of HTTPS for the registry.
	PlainHTTP bool
}

// Pusher packages a directory as an OCI artifact and pushes it to a registry.
type Pusher struct {
	Config      ArtifactConfig
	Output      io.Writer
	ErrorOutput io.Writer
}

// NewPusher creates a new Pusher with the given artifact config.
func NewPusher(config ArtifactConfig) *Pusher {
	return &Pusher{
		Config:      config,
		Output:      os.Stdout,
		ErrorOutput: os.Stderr,
	}
}

// Push packages the source directory and pushes it as an OCI artifact.
func (p *Pusher) Push(ctx context.Context, opts PushOptions) error {
	if opts.Tag == "" {
		return fmt.Errorf("tag is required")
	}
	if opts.SourceDir == "" {
		return fmt.Errorf("source directory is required")
	}

	if err := ValidateSource(opts.SourceDir); err != nil {
		return fmt.Errorf("invalid source: %w", err)
	}

	fmt.Fprintf(p.Output, "Packaging %s from %s\n", p.Config.Label, opts.SourceDir)

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
	layerDesc, err := PushBlob(ctx, store, p.Config.LayerMediaType, layerData)
	if err != nil {
		return fmt.Errorf("storing layer: %w", err)
	}

	// Push empty config (required by OCI spec)
	configData := []byte("{}")
	configDesc, err := PushBlob(ctx, store, p.Config.ConfigMediaType, configData)
	if err != nil {
		return fmt.Errorf("storing config: %w", err)
	}

	// Build the manifest
	manifest := ocispec.Manifest{
		Versioned:    specs.Versioned{SchemaVersion: 2},
		MediaType:    ocispec.MediaTypeImageManifest,
		ArtifactType: p.Config.ArtifactType,
		Config:       configDesc,
		Layers:       []ocispec.Descriptor{layerDesc},
	}

	manifestData, err := json.Marshal(manifest)
	if err != nil {
		return fmt.Errorf("encoding manifest: %w", err)
	}

	manifestDesc, err := PushBlob(ctx, store, ocispec.MediaTypeImageManifest, manifestData)
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

	// Default client with no credentials
	repo.Client = &auth.Client{
		Client:     retry.DefaultClient,
		Cache:      auth.NewCache(),
		Credential: auth.StaticCredential(ref.Registry, auth.EmptyCredential),
	}

	// Try Docker config credentials
	dockerCreds := LoadDockerCredentials(ref.Registry)
	if dockerCreds != (auth.Credential{}) {
		repo.Client = &auth.Client{
			Client:     retry.DefaultClient,
			Cache:      auth.NewCache(),
			Credential: auth.StaticCredential(ref.Registry, dockerCreds),
		}
	}

	fmt.Fprintf(p.Output, "Pushing to %s\n", opts.Tag)

	_, err = oras.Copy(ctx, store, ref.Tag, repo, ref.Tag, oras.DefaultCopyOptions)
	if err != nil {
		return fmt.Errorf("pushing artifact: %w", err)
	}

	fmt.Fprintf(p.Output, "Successfully pushed %s\n", opts.Tag)
	return nil
}

// ReferenceInfo holds parsed OCI reference components.
type ReferenceInfo struct {
	Registry   string // e.g., "ghcr.io"
	Repository string // e.g., "ghcr.io/myorg/pr-classifier"
	Tag        string // e.g., "v1.0.0"
}

// ParseReference parses a full OCI reference like "ghcr.io/myorg/repo:tag".
func ParseReference(ref string) (ReferenceInfo, error) {
	tag := "latest"
	repo := ref
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			tag = afterColon
			repo = ref[:idx]
		}
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) < 2 {
		return ReferenceInfo{}, fmt.Errorf("invalid reference %q: must be in format registry/repo[:tag]", ref)
	}

	return ReferenceInfo{
		Registry:   parts[0],
		Repository: repo,
		Tag:        tag,
	}, nil
}

// ValidateSource ensures the directory contains a valid package (index.ts or index.js).
func ValidateSource(dir string) error {
	info, err := os.Stat(dir)
	if err != nil {
		return fmt.Errorf("cannot access directory: %w", err)
	}
	if !info.IsDir() {
		return fmt.Errorf("%s is not a directory", dir)
	}

	hasIndex := false
	for _, name := range []string{"index.ts", "index.js"} {
		if _, err := os.Stat(filepath.Join(dir, name)); err == nil {
			hasIndex = true
			break
		}
	}
	if !hasIndex {
		return fmt.Errorf("directory must contain index.ts or index.js")
	}

	return nil
}

// CreateTarGzip creates a tar.gz archive of the given directory.
// It skips node_modules, .git, dist directories and common non-essential files.
func CreateTarGzip(dir string) ([]byte, error) {
	var buf bytes.Buffer
	gw := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gw)

	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}

		relPath, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}

		if relPath == "." {
			return nil
		}

		base := filepath.Base(relPath)
		if info.IsDir() && (base == "node_modules" || base == ".git" || base == "dist") {
			return filepath.SkipDir
		}

		if !info.IsDir() && shouldSkipFile(base) {
			return nil
		}

		header, err := tar.FileInfoHeader(info, "")
		if err != nil {
			return err
		}

		header.Name = filepath.ToSlash(relPath)

		if err := tw.WriteHeader(header); err != nil {
			return err
		}

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

// PushBlob pushes a blob to the in-memory store and returns its descriptor.
func PushBlob(ctx context.Context, store *memory.Store, mediaType string, data []byte) (ocispec.Descriptor, error) {
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

// LoadDockerCredentials attempts to read credentials from ~/.docker/config.json.
func LoadDockerCredentials(registry string) auth.Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return auth.EmptyCredential
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return auth.EmptyCredential
	}

	var config struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return auth.EmptyCredential
	}

	for host, entry := range config.Auths {
		h := strings.TrimPrefix(host, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.TrimSuffix(h, "/")
		if h == registry {
			if entry.Auth == "" {
				return auth.EmptyCredential
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				return auth.EmptyCredential
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				return auth.EmptyCredential
			}
			return auth.Credential{
				Username: parts[0],
				Password: parts[1],
			}
		}
	}

	return auth.EmptyCredential
}

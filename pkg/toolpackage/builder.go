// Package toolpackage provides the build orchestrator for tool images.
package toolpackage

import (
	"context"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
)

// Builder orchestrates the building of tool images.
type Builder struct {
	// Output is where to write build output (default: os.Stdout)
	Output io.Writer

	// ErrorOutput is where to write error output (default: os.Stderr)
	ErrorOutput io.Writer
}

// NewBuilder creates a new Builder.
func NewBuilder() *Builder {
	return &Builder{
		Output:      os.Stdout,
		ErrorOutput: os.Stderr,
	}
}

// BuildOptions configures a build.
type BuildOptions struct {
	// Tag is the image tag (required)
	Tag string

	// Push pushes the image after building
	Push bool

	// NoCache disables Docker build cache
	NoCache bool

	// Platforms specifies the target platforms (e.g., "linux/amd64,linux/arm64")
	// If empty, defaults to the host platform.
	Platforms string

	// BuildArgs are additional Docker build arguments
	BuildArgs map[string]string
}

// Build builds a tool image from a ToolPackage.
func (b *Builder) Build(ctx context.Context, pkg *ToolPackage, opts BuildOptions) error {
	if opts.Tag == "" {
		return fmt.Errorf("tag is required")
	}

	if err := pkg.Validate(); err != nil {
		return fmt.Errorf("invalid package: %w", err)
	}

	// Create temporary build context directory
	buildDir, err := os.MkdirTemp("", "tool-build-*")
	if err != nil {
		return fmt.Errorf("creating temp directory: %w", err)
	}
	defer os.RemoveAll(buildDir)

	fmt.Fprintf(b.Output, "Building tool: %s v%s\n", pkg.Metadata.Name, pkg.Metadata.Version)
	fmt.Fprintf(b.Output, "Install method: %s\n", pkg.InstallMethod())
	fmt.Fprintf(b.Output, "Build context: %s\n", buildDir)

	// Generate and write Dockerfile
	dockerfile, err := GenerateDockerfile(pkg)
	if err != nil {
		return fmt.Errorf("generating Dockerfile: %w", err)
	}

	dockerfilePath := filepath.Join(buildDir, "Dockerfile")
	if err := os.WriteFile(dockerfilePath, []byte(dockerfile), 0644); err != nil {
		return fmt.Errorf("writing Dockerfile: %w", err)
	}

	// Write deny.txt if there are deny patterns
	if len(pkg.Deny) > 0 {
		denyContent := GenerateDenyFile(pkg.Deny)
		denyPath := filepath.Join(buildDir, "deny.txt")
		if err := os.WriteFile(denyPath, []byte(denyContent), 0644); err != nil {
			return fmt.Errorf("writing deny.txt: %w", err)
		}
		fmt.Fprintf(b.Output, "Embedded %d deny patterns\n", len(pkg.Deny))
	}

	// Run docker build
	if err := b.runDockerBuild(ctx, buildDir, opts); err != nil {
		return fmt.Errorf("docker build: %w", err)
	}

	fmt.Fprintf(b.Output, "Successfully built %s\n", opts.Tag)

	return nil
}

// runDockerBuild runs the docker buildx build command.
// When Platforms is set or Push is true, it uses buildx to build multi-platform
// images and push directly to the registry (images are not stored locally).
func (b *Builder) runDockerBuild(ctx context.Context, buildDir string, opts BuildOptions) error {
	args := []string{"buildx", "build", "-t", opts.Tag}

	if opts.NoCache {
		args = append(args, "--no-cache")
	}

	if opts.Platforms != "" {
		args = append(args, "--platform", opts.Platforms)
	}

	if opts.Push {
		args = append(args, "--push")
	}

	for k, v := range opts.BuildArgs {
		args = append(args, "--build-arg", fmt.Sprintf("%s=%s", k, v))
	}

	args = append(args, ".")

	fmt.Fprintf(b.Output, "Running: docker %v\n", args)

	cmd := exec.CommandContext(ctx, "docker", args...)
	cmd.Dir = buildDir
	cmd.Stdout = b.Output
	cmd.Stderr = b.ErrorOutput

	return cmd.Run()
}

// FindRepoRoot finds the agent-tools repository root by looking for go.mod.
// It starts from the current directory and walks up.
func FindRepoRoot() (string, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return "", fmt.Errorf("getting working directory: %w", err)
	}

	dir := cwd
	for {
		goMod := filepath.Join(dir, "go.mod")
		if _, err := os.Stat(goMod); err == nil {
			// Verify it's the right go.mod by checking module name
			data, err := os.ReadFile(goMod)
			if err == nil && contains(string(data), "github.com/samyn92/agent-tools") {
				return dir, nil
			}
		}

		parent := filepath.Dir(dir)
		if parent == dir {
			break
		}
		dir = parent
	}

	return "", fmt.Errorf("could not find agent-tools repository root (no go.mod found)")
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsAt(s, substr))
}

func containsAt(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}

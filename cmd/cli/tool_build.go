package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/samyn92/agent-tools/pkg/toolpackage"
	"github.com/spf13/cobra"
)

var (
	// Flags for tool build command
	buildFile     string
	buildTag      string
	buildPush     bool
	buildNoCache  bool
	buildPlatform string

	// Gateway version flag
	buildGatewayVersion string

	// Inline build options
	buildBinary          string
	buildAPK             string
	buildPip             string
	buildNPM             string
	buildGo              string
	buildDownloadURL     string
	buildDownloadExtract string
	buildDownloadPath    string
	buildDeny            []string
)

var toolBuildCmd = &cobra.Command{
	Use:   "build",
	Short: "Build a tool image",
	Long: `Build a tool image from a manifest file or inline options.

From manifest:
  agent-tools tool build -f tool.yaml -t ghcr.io/myorg/tool-gh:v1.0.0
  agent-tools tool build -f tool.yaml -t ghcr.io/myorg/tool-gh:v1.0.0 --push

Inline build:
  agent-tools tool build --apk github-cli --binary gh -t myimage:latest
  agent-tools tool build --pip awscli --binary aws -t myimage:latest

Specify gateway version:
  agent-tools tool build -f tool.yaml -t myimage:latest --gateway-version v0.0.1`,
	RunE: runToolBuild,
}

func init() {
	// Manifest-based build
	toolBuildCmd.Flags().StringVarP(&buildFile, "file", "f", "", "Path to tool.yaml manifest")
	toolBuildCmd.Flags().StringVarP(&buildTag, "tag", "t", "", "Image tag (required)")
	toolBuildCmd.Flags().BoolVar(&buildPush, "push", false, "Push image after building")
	toolBuildCmd.Flags().BoolVar(&buildNoCache, "no-cache", false, "Do not use cache when building")
	toolBuildCmd.Flags().StringVar(&buildPlatform, "platform", "", "Set target platform (e.g., linux/amd64)")
	toolBuildCmd.Flags().StringVar(&buildGatewayVersion, "gateway-version", "latest", "Agent Operator Core release tag for capability-gateway binary")

	// Inline build options
	toolBuildCmd.Flags().StringVar(&buildBinary, "binary", "", "CLI binary name (for inline builds)")
	toolBuildCmd.Flags().StringVar(&buildAPK, "apk", "", "Alpine package to install")
	toolBuildCmd.Flags().StringVar(&buildPip, "pip", "", "Python package to install")
	toolBuildCmd.Flags().StringVar(&buildNPM, "npm", "", "NPM package to install")
	toolBuildCmd.Flags().StringVar(&buildGo, "go", "", "Go package to install")
	toolBuildCmd.Flags().StringVar(&buildDownloadURL, "download-url", "", "URL to download binary from")
	toolBuildCmd.Flags().StringVar(&buildDownloadExtract, "download-extract", "", "Archive format: tar.gz, tar.xz, zip")
	toolBuildCmd.Flags().StringVar(&buildDownloadPath, "download-path", "", "Path to binary within archive")
	toolBuildCmd.Flags().StringSliceVar(&buildDeny, "deny", nil, "Deny patterns to embed (can be specified multiple times)")

	toolBuildCmd.MarkFlagRequired("tag")
}

func runToolBuild(cmd *cobra.Command, args []string) error {
	// Validate flags
	if buildTag == "" {
		return fmt.Errorf("--tag is required")
	}

	// Get the package - either from file or inline options
	var pkg *toolpackage.ToolPackage
	var err error

	if buildFile != "" {
		// Load from manifest file
		pkg, err = toolpackage.LoadFromFile(buildFile)
		if err != nil {
			return fmt.Errorf("loading manifest: %w", err)
		}
	} else if buildBinary != "" {
		// Build from inline options
		if !hasInlineInstallMethod() {
			return fmt.Errorf("when using --binary, you must also specify an install method: --apk, --pip, --npm, --go, or --download-url")
		}

		pkg = toolpackage.NewInlinePackage(buildBinary, toolpackage.InlineOptions{
			APK:             buildAPK,
			Pip:             buildPip,
			NPM:             buildNPM,
			Go:              buildGo,
			DownloadURL:     buildDownloadURL,
			DownloadExtract: buildDownloadExtract,
			DownloadPath:    buildDownloadPath,
			Deny:            buildDeny,
		})
	} else {
		return fmt.Errorf("either --file or --binary is required")
	}

	// Validate the package
	if err := pkg.Validate(); err != nil {
		return fmt.Errorf("invalid tool package: %w", err)
	}

	// Create builder
	builder := toolpackage.NewBuilder(buildGatewayVersion)

	// Set up context with signal handling
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		fmt.Println("\nBuild cancelled")
		cancel()
	}()

	// Run the build
	opts := toolpackage.BuildOptions{
		Tag:      buildTag,
		Push:     buildPush,
		NoCache:  buildNoCache,
		Platform: buildPlatform,
	}

	if err := builder.Build(ctx, pkg, opts); err != nil {
		return fmt.Errorf("build failed: %w", err)
	}

	return nil
}

func hasInlineInstallMethod() bool {
	return buildAPK != "" || buildPip != "" || buildNPM != "" || buildGo != "" || buildDownloadURL != ""
}

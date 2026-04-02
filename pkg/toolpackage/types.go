// Package toolpackage provides types and utilities for the Tool Packaging Standard.
// It defines the ToolPackage manifest format (tool.yaml) that declaratively specifies
// how to build CLI tools into container images.
package toolpackage

import (
	"fmt"
	"os"

	"gopkg.in/yaml.v3"
)

const (
	// APIVersion is the current API version for ToolPackage manifests
	APIVersion = "tools.agents.io/v1"
	// Kind is the resource kind for ToolPackage manifests
	Kind = "ToolPackage"
)

// ToolPackage represents a tool.yaml manifest that defines how to build a CLI tool image.
type ToolPackage struct {
	APIVersion string       `yaml:"apiVersion"`
	Kind       string       `yaml:"kind"`
	Metadata   ToolMetadata `yaml:"metadata"`
	CLI        CLISpec      `yaml:"cli"`
	Deny       []string     `yaml:"deny,omitempty"`
	Env        []EnvDoc     `yaml:"env,omitempty"`
}

// ToolMetadata contains metadata about the tool package.
type ToolMetadata struct {
	Name        string `yaml:"name"`
	Version     string `yaml:"version"`
	Description string `yaml:"description,omitempty"`
}

// CLISpec defines how to install the CLI binary.
// Exactly one install method must be specified.
type CLISpec struct {
	// Binary is the name of the CLI binary (required)
	Binary string `yaml:"binary"`

	// APK is an Alpine package name to install
	APK string `yaml:"apk,omitempty"`

	// Download specifies a binary to download from a URL
	Download *DownloadSpec `yaml:"download,omitempty"`

	// Pip is a Python package to install via pip
	Pip string `yaml:"pip,omitempty"`

	// NPM is a Node.js package to install via npm
	NPM string `yaml:"npm,omitempty"`

	// Go is a Go package to install via go install (e.g., "github.com/cli/cli/v2@v2.45.0")
	Go string `yaml:"go,omitempty"`
}

// DownloadSpec defines how to download a binary from a URL.
type DownloadSpec struct {
	// URL is the download URL for the binary or archive
	URL string `yaml:"url"`

	// Extract specifies the archive format: "tar.gz", "tar.xz", "zip", or empty for raw binary
	Extract string `yaml:"extract,omitempty"`

	// Path is the path to the binary within the archive (required if Extract is set)
	Path string `yaml:"path,omitempty"`

	// Chmod sets the file permissions (default: 0755)
	Chmod string `yaml:"chmod,omitempty"`
}

// EnvDoc documents an environment variable that the tool expects.
type EnvDoc struct {
	// Name is the environment variable name
	Name string `yaml:"name"`

	// Description explains what this variable is for
	Description string `yaml:"description,omitempty"`

	// Required indicates if this variable must be set
	Required bool `yaml:"required,omitempty"`
}

// LoadFromFile loads a ToolPackage from a YAML file.
func LoadFromFile(path string) (*ToolPackage, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("reading file: %w", err)
	}

	return Parse(data)
}

// Parse parses a ToolPackage from YAML bytes.
func Parse(data []byte) (*ToolPackage, error) {
	var pkg ToolPackage
	if err := yaml.Unmarshal(data, &pkg); err != nil {
		return nil, fmt.Errorf("parsing YAML: %w", err)
	}

	return &pkg, nil
}

// Validate checks that the ToolPackage is valid.
func (p *ToolPackage) Validate() error {
	if p.APIVersion != APIVersion {
		return fmt.Errorf("unsupported apiVersion %q, expected %q", p.APIVersion, APIVersion)
	}

	if p.Kind != Kind {
		return fmt.Errorf("unsupported kind %q, expected %q", p.Kind, Kind)
	}

	if p.Metadata.Name == "" {
		return fmt.Errorf("metadata.name is required")
	}

	if p.Metadata.Version == "" {
		return fmt.Errorf("metadata.version is required")
	}

	if p.CLI.Binary == "" {
		return fmt.Errorf("cli.binary is required")
	}

	// Count install methods
	methods := 0
	if p.CLI.APK != "" {
		methods++
	}
	if p.CLI.Download != nil {
		methods++
	}
	if p.CLI.Pip != "" {
		methods++
	}
	if p.CLI.NPM != "" {
		methods++
	}
	if p.CLI.Go != "" {
		methods++
	}

	if methods == 0 {
		return fmt.Errorf("exactly one CLI install method required (apk, download, pip, npm, or go)")
	}
	if methods > 1 {
		return fmt.Errorf("only one CLI install method allowed, found %d", methods)
	}

	// Validate download spec
	if p.CLI.Download != nil {
		if p.CLI.Download.URL == "" {
			return fmt.Errorf("cli.download.url is required")
		}
		if p.CLI.Download.Extract != "" && p.CLI.Download.Path == "" {
			return fmt.Errorf("cli.download.path is required when extract is set")
		}
		validExtract := map[string]bool{"": true, "tar.gz": true, "tar.xz": true, "zip": true}
		if !validExtract[p.CLI.Download.Extract] {
			return fmt.Errorf("cli.download.extract must be one of: tar.gz, tar.xz, zip")
		}
	}

	return nil
}

// InstallMethod returns the name of the install method being used.
func (p *ToolPackage) InstallMethod() string {
	switch {
	case p.CLI.APK != "":
		return "apk"
	case p.CLI.Download != nil:
		return "download"
	case p.CLI.Pip != "":
		return "pip"
	case p.CLI.NPM != "":
		return "npm"
	case p.CLI.Go != "":
		return "go"
	default:
		return "unknown"
	}
}

// NewInlinePackage creates a ToolPackage from inline CLI flags.
// This is used for the --apk, --pip, etc. flags on the CLI.
func NewInlinePackage(binary string, opts InlineOptions) *ToolPackage {
	pkg := &ToolPackage{
		APIVersion: APIVersion,
		Kind:       Kind,
		Metadata: ToolMetadata{
			Name:    binary,
			Version: "inline",
		},
		CLI: CLISpec{
			Binary: binary,
		},
		Deny: opts.Deny,
	}

	switch {
	case opts.APK != "":
		pkg.CLI.APK = opts.APK
	case opts.Pip != "":
		pkg.CLI.Pip = opts.Pip
	case opts.NPM != "":
		pkg.CLI.NPM = opts.NPM
	case opts.Go != "":
		pkg.CLI.Go = opts.Go
	case opts.DownloadURL != "":
		pkg.CLI.Download = &DownloadSpec{
			URL:     opts.DownloadURL,
			Extract: opts.DownloadExtract,
			Path:    opts.DownloadPath,
		}
	}

	return pkg
}

// InlineOptions are options for creating a ToolPackage from CLI flags.
type InlineOptions struct {
	APK             string
	Pip             string
	NPM             string
	Go              string
	DownloadURL     string
	DownloadExtract string
	DownloadPath    string
	Deny            []string
}

package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Input types ──

type getInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: all, helmreleases (hr), kustomizations (ks), sources (all/git/helm/oci/bucket/chart), alerts, alert-providers, receivers, images (all/policy/repository/update), artifacts"`
	Name      string `json:"name,omitempty" jsonschema_description:"Resource name (omit to list all)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for default, '-A' for all namespaces)"`
}

type checkInput struct {
	Pre bool `json:"pre,omitempty" jsonschema_description:"Only check prerequisites (kubectl, cluster connection) without checking controllers"`
}

type statsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for default, '-A' for all)"`
}

type logsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for flux-system)"`
	Kind      string `json:"kind,omitempty" jsonschema_description:"Filter by Flux resource kind (e.g. Kustomization, HelmRelease, GitRepository)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Filter by resource name"`
	Level     string `json:"level,omitempty" jsonschema_description:"Log level filter: info, error"`
	Tail      int    `json:"tail,omitempty" jsonschema_description:"Number of log lines (default: 50)"`
	Since     string `json:"since,omitempty" jsonschema_description:"Show logs since duration (e.g. 5m, 1h)"`
}

type eventsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for default, '-A' for all)"`
	For       string `json:"for,omitempty" jsonschema_description:"Filter events for a specific resource (e.g. Kustomization/my-app, HelmRelease/nginx)"`
	Types     string `json:"types,omitempty" jsonschema_description:"Event types: Normal, Warning"`
}

type traceInput struct {
	Kind       string `json:"kind" jsonschema_description:"Kubernetes object kind (e.g. Deployment, Service, ConfigMap)"`
	Name       string `json:"name" jsonschema_description:"Object name"`
	Namespace  string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	APIVersion string `json:"api_version,omitempty" jsonschema_description:"API version (e.g. apps/v1). Required for disambiguation."`
}

type treeInput struct {
	Name      string `json:"name" jsonschema_description:"Kustomization name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type diffInput struct {
	Name      string `json:"name" jsonschema_description:"Kustomization name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Path      string `json:"path,omitempty" jsonschema_description:"Path to local kustomization directory (for local diff)"`
}

type exportInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type to export: helmrelease, kustomization, source (git/helm/oci/bucket/chart), alert, alert-provider, receiver, image (policy/repository/update)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Resource name (omit to export all of that type)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	All       bool   `json:"all,omitempty" jsonschema_description:"Export from all namespaces"`
}

type debugInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: helmrelease or kustomization"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type versionInput struct{}

// ── Handlers ──

func handleGet(_ context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required (e.g. all, helmreleases, kustomizations, sources git)"), nil, nil
	}
	// flux get supports space-separated subcommands like "sources git"
	args := append([]string{"get"}, strings.Fields(in.Resource)...)
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleCheck(_ context.Context, _ *mcp.CallToolRequest, in checkInput) (*mcp.CallToolResult, any, error) {
	args := []string{"check"}
	if in.Pre {
		args = append(args, "--pre")
	}
	return flux(args...), nil, nil
}

func handleStats(_ context.Context, _ *mcp.CallToolRequest, in statsInput) (*mcp.CallToolResult, any, error) {
	args := []string{"stats"}
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleLogs(_ context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, any, error) {
	args := []string{"logs"}
	if in.Namespace != "" {
		args = appendNamespace(args, in.Namespace)
	}
	if in.Kind != "" {
		args = append(args, "--kind", in.Kind)
	}
	if in.Name != "" {
		args = append(args, "--name", in.Name)
	}
	if in.Level != "" {
		args = append(args, "--level", in.Level)
	}
	tail := in.Tail
	if tail <= 0 {
		tail = 50
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail))
	if in.Since != "" {
		args = append(args, "--since", in.Since)
	}
	return flux(args...), nil, nil
}

func handleEvents(_ context.Context, _ *mcp.CallToolRequest, in eventsInput) (*mcp.CallToolResult, any, error) {
	args := []string{"events"}
	args = appendNamespace(args, in.Namespace)
	if in.For != "" {
		args = append(args, "--for", in.For)
	}
	if in.Types != "" {
		args = append(args, "--types", in.Types)
	}
	return flux(args...), nil, nil
}

func handleTrace(_ context.Context, _ *mcp.CallToolRequest, in traceInput) (*mcp.CallToolResult, any, error) {
	if in.Kind == "" || in.Name == "" {
		return errResult("kind and name are required"), nil, nil
	}
	args := []string{"trace", in.Kind, in.Name}
	args = appendNamespace(args, in.Namespace)
	if in.APIVersion != "" {
		args = append(args, "--api-version", in.APIVersion)
	}
	return flux(args...), nil, nil
}

func handleTree(_ context.Context, _ *mcp.CallToolRequest, in treeInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return errResult("name is required"), nil, nil
	}
	args := []string{"tree", "kustomization", in.Name}
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleDiff(_ context.Context, _ *mcp.CallToolRequest, in diffInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return errResult("name is required"), nil, nil
	}
	args := []string{"diff", "kustomization", in.Name}
	args = appendNamespace(args, in.Namespace)
	if in.Path != "" {
		args = append(args, "--path", in.Path)
	}
	return fluxWithTimeout(60_000_000_000, args...), nil, nil // 60s for diff
}

func handleExport(_ context.Context, _ *mcp.CallToolRequest, in exportInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required (e.g. helmrelease, kustomization, source git)"), nil, nil
	}
	args := append([]string{"export"}, strings.Fields(in.Resource)...)
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	if in.All {
		args = append(args, "--all")
	}
	return flux(args...), nil, nil
}

func handleDebug(_ context.Context, _ *mcp.CallToolRequest, in debugInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return errResult("resource and name are required"), nil, nil
	}
	args := []string{"debug", in.Resource, in.Name}
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleVersion(_ context.Context, _ *mcp.CallToolRequest, _ versionInput) (*mcp.CallToolResult, any, error) {
	return flux("version"), nil, nil
}

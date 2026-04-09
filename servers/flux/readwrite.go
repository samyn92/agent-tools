package main

import (
	"context"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Input types ──

type reconcileInput struct {
	Resource   string `json:"resource" jsonschema_description:"Resource type: helmrelease (hr), kustomization (ks), source git, source helm, source oci, source bucket, source chart, image repository, image policy, image update, receiver"`
	Name       string `json:"name" jsonschema_description:"Resource name"`
	Namespace  string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	WithSource bool   `json:"with_source,omitempty" jsonschema_description:"Also reconcile the source (for helmrelease/kustomization)"`
}

type suspendInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: helmrelease (hr), kustomization (ks), source git, source helm, source oci, source bucket, source chart, alert, alert-provider, image (repository/policy/update), receiver"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type resumeInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (same as suspend)"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type deleteInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: helmrelease, kustomization, source (git/helm/oci/bucket/chart), alert, alert-provider, receiver, image (policy/repository/update)"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Silent    bool   `json:"silent,omitempty" jsonschema_description:"Skip confirmation prompt (always true for MCP)"`
}

// ── Handlers ──

func handleReconcile(_ context.Context, _ *mcp.CallToolRequest, in reconcileInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return errResult("resource and name are required"), nil, nil
	}
	args := append([]string{"reconcile"}, strings.Fields(in.Resource)...)
	args = append(args, in.Name)
	args = appendNamespace(args, in.Namespace)
	if in.WithSource {
		args = append(args, "--with-source")
	}
	return fluxWithTimeout(120*time.Second, args...), nil, nil
}

func handleSuspend(_ context.Context, _ *mcp.CallToolRequest, in suspendInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return errResult("resource and name are required"), nil, nil
	}
	args := append([]string{"suspend"}, strings.Fields(in.Resource)...)
	args = append(args, in.Name)
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleResume(_ context.Context, _ *mcp.CallToolRequest, in resumeInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return errResult("resource and name are required"), nil, nil
	}
	args := append([]string{"resume"}, strings.Fields(in.Resource)...)
	args = append(args, in.Name)
	args = appendNamespace(args, in.Namespace)
	return flux(args...), nil, nil
}

func handleDelete(_ context.Context, _ *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return errResult("resource and name are required"), nil, nil
	}
	args := append([]string{"delete"}, strings.Fields(in.Resource)...)
	args = append(args, in.Name)
	args = appendNamespace(args, in.Namespace)
	// Always silent — MCP has no interactive prompt
	args = append(args, "--silent")
	return flux(args...), nil, nil
}

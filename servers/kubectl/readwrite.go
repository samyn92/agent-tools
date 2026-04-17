package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

// ── Input types ──

type execInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (required for multi-container pods)"`
	Command   string `json:"command" jsonschema_description:"Command to execute (passed to sh -c)"`
	Timeout   int    `json:"timeout,omitempty" jsonschema_description:"Timeout in seconds (default: 30, max: 300)"`
}

type applyInput struct {
	Manifest  string `json:"manifest" jsonschema_description:"YAML or JSON manifest content to apply"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (overrides manifest metadata)"`
	DryRun    string `json:"dry_run,omitempty" jsonschema_description:"Dry-run mode: client, server, or empty for real apply"`
}

type deleteInput struct {
	Resource  string `json:"resource,omitempty" jsonschema_description:"Resource type (e.g. pod, deployment)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Selector  string `json:"selector,omitempty" jsonschema_description:"Label selector (alternative to name)"`
	Force     bool   `json:"force,omitempty" jsonschema_description:"Force deletion (--grace-period=0 --force)"`
}

type runInput struct {
	Name      string `json:"name" jsonschema_description:"Pod name"`
	Image     string `json:"image" jsonschema_description:"Container image"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Command   string `json:"command,omitempty" jsonschema_description:"Command to run (passed to sh -c)"`
	Rm        bool   `json:"rm,omitempty" jsonschema_description:"Remove pod after completion (default: true)"`
	Timeout   int    `json:"timeout,omitempty" jsonschema_description:"Timeout in seconds (default: 60, max: 600)"`
}

type cpInput struct {
	Src       string `json:"src" jsonschema_description:"Source path. Use pod:path for container paths (e.g. nginx-xyz:/var/log/app.log)"`
	Dst       string `json:"dst" jsonschema_description:"Destination path. Use pod:path for container paths (e.g. nginx-xyz:/tmp/config.yaml)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name"`
}

type rolloutInput struct {
	Action    string `json:"action" jsonschema_description:"Rollout action: status, history, undo, restart"`
	Resource  string `json:"resource" jsonschema_description:"Resource (e.g. deployment/nginx, statefulset/postgres)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Revision  int    `json:"revision,omitempty" jsonschema_description:"Revision number (for undo)"`
}

type scaleInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource to scale (e.g. deployment/nginx, statefulset/postgres)"`
	Replicas  int    `json:"replicas" jsonschema_description:"Desired replica count"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type labelInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pod, node, deployment)"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Labels    string `json:"labels" jsonschema_description:"Labels to set (e.g. 'app=nginx env=prod'). Use key- to remove a label."`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Overwrite bool   `json:"overwrite,omitempty" jsonschema_description:"Overwrite existing labels"`
}

type annotateInput struct {
	Resource    string `json:"resource" jsonschema_description:"Resource type"`
	Name        string `json:"name" jsonschema_description:"Resource name"`
	Annotations string `json:"annotations" jsonschema_description:"Annotations to set (e.g. 'desc=my-service'). Use key- to remove."`
	Namespace   string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Overwrite   bool   `json:"overwrite,omitempty" jsonschema_description:"Overwrite existing annotations"`
}

// ── Handlers ──

func handleExec(ctx context.Context, _ *mcp.CallToolRequest, in execInput) (*mcp.CallToolResult, any, error) {
	if in.Pod == "" {
		return mcputil.ErrResult("pod is required"), nil, nil
	}
	if in.Command == "" {
		return mcputil.ErrResult("command is required"), nil, nil
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 30
	}
	if timeout > 300 {
		timeout = 300
	}

	args := []string{"exec", in.Pod}
	args = appendNamespace(args, in.Namespace)
	if in.Container != "" {
		args = append(args, "-c", in.Container)
	}
	args = append(args, "--", "sh", "-c", in.Command)

	return kubeWithTimeout(ctx, time.Duration(timeout)*time.Second, args...), nil, nil
}

func handleApply(ctx context.Context, _ *mcp.CallToolRequest, in applyInput) (*mcp.CallToolResult, any, error) {
	if in.Manifest == "" {
		return mcputil.ErrResult("manifest is required"), nil, nil
	}

	tmpfile, err := os.CreateTemp("", "kubectl-apply-*.yaml")
	if err != nil {
		return mcputil.ErrResult("failed to create temp file: %s", err), nil, nil
	}
	defer os.Remove(tmpfile.Name())

	if _, err := tmpfile.WriteString(in.Manifest); err != nil {
		tmpfile.Close()
		return mcputil.ErrResult("failed to write manifest: %s", err), nil, nil
	}
	tmpfile.Close()

	args := []string{"apply", "-f", tmpfile.Name()}
	args = appendNamespace(args, in.Namespace)
	if in.DryRun != "" {
		args = append(args, "--dry-run", in.DryRun)
	}
	return kube(ctx, args...), nil, nil
}

func handleDelete(ctx context.Context, _ *mcp.CallToolRequest, in deleteInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return mcputil.ErrResult("resource is required"), nil, nil
	}
	if in.Name == "" && in.Selector == "" {
		return mcputil.ErrResult("name or selector is required"), nil, nil
	}

	args := []string{"delete", in.Resource}
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}
	if in.Force {
		args = append(args, "--grace-period=0", "--force")
	}
	return kube(ctx, args...), nil, nil
}

func handleRun(ctx context.Context, _ *mcp.CallToolRequest, in runInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return mcputil.ErrResult("name is required"), nil, nil
	}
	if in.Image == "" {
		return mcputil.ErrResult("image is required"), nil, nil
	}

	timeout := in.Timeout
	if timeout <= 0 {
		timeout = 60
	}
	if timeout > 600 {
		timeout = 600
	}

	args := []string{"run", in.Name, "--image", in.Image, "--restart=Never"}
	args = appendNamespace(args, in.Namespace)
	args = append(args, "--rm", "--attach")

	if in.Command != "" {
		args = append(args, "--", "sh", "-c", in.Command)
	}

	return kubeWithTimeout(ctx, time.Duration(timeout)*time.Second, args...), nil, nil
}

func handleCp(ctx context.Context, _ *mcp.CallToolRequest, in cpInput) (*mcp.CallToolResult, any, error) {
	if in.Src == "" {
		return mcputil.ErrResult("src is required"), nil, nil
	}
	if in.Dst == "" {
		return mcputil.ErrResult("dst is required"), nil, nil
	}

	args := []string{"cp", in.Src, in.Dst}
	args = appendNamespace(args, in.Namespace)
	if in.Container != "" {
		args = append(args, "-c", in.Container)
	}
	return kube(ctx, args...), nil, nil
}

func handleRollout(ctx context.Context, _ *mcp.CallToolRequest, in rolloutInput) (*mcp.CallToolResult, any, error) {
	validActions := map[string]bool{"status": true, "history": true, "undo": true, "restart": true}
	if !validActions[in.Action] {
		return mcputil.ErrResult("action must be one of: status, history, undo, restart"), nil, nil
	}
	if in.Resource == "" {
		return mcputil.ErrResult("resource is required (e.g. deployment/nginx)"), nil, nil
	}

	args := []string{"rollout", in.Action, in.Resource}
	args = appendNamespace(args, in.Namespace)
	if in.Action == "undo" && in.Revision > 0 {
		args = append(args, fmt.Sprintf("--to-revision=%d", in.Revision))
	}
	return kube(ctx, args...), nil, nil
}

func handleScale(ctx context.Context, _ *mcp.CallToolRequest, in scaleInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return mcputil.ErrResult("resource is required (e.g. deployment/nginx)"), nil, nil
	}

	args := []string{"scale", in.Resource, fmt.Sprintf("--replicas=%d", in.Replicas)}
	args = appendNamespace(args, in.Namespace)
	return kube(ctx, args...), nil, nil
}

func handleLabel(ctx context.Context, _ *mcp.CallToolRequest, in labelInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return mcputil.ErrResult("resource and name are required"), nil, nil
	}
	if in.Labels == "" {
		return mcputil.ErrResult("labels is required"), nil, nil
	}

	args := []string{"label", in.Resource, in.Name}
	args = append(args, strings.Fields(in.Labels)...)
	args = appendNamespace(args, in.Namespace)
	if in.Overwrite {
		args = append(args, "--overwrite")
	}
	return kube(ctx, args...), nil, nil
}

func handleAnnotate(ctx context.Context, _ *mcp.CallToolRequest, in annotateInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" || in.Name == "" {
		return mcputil.ErrResult("resource and name are required"), nil, nil
	}
	if in.Annotations == "" {
		return mcputil.ErrResult("annotations is required"), nil, nil
	}

	args := []string{"annotate", in.Resource, in.Name}
	args = append(args, strings.Fields(in.Annotations)...)
	args = appendNamespace(args, in.Namespace)
	if in.Overwrite {
		args = append(args, "--overwrite")
	}
	return kube(ctx, args...), nil, nil
}

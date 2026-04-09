package main

import (
	"context"
	"fmt"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Input types ──

type getInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pods, deployments, services, nodes, all)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Resource name (omit to list all)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for default, use '-A' for all namespaces)"`
	Selector  string `json:"selector,omitempty" jsonschema_description:"Label selector (e.g. app=nginx)"`
	Output    string `json:"output,omitempty" jsonschema_description:"Output format: wide, yaml, json, name, jsonpath=... (default: wide)"`
}

type describeInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pod, deployment, node, service)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Resource name (omit to describe all of that type)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Selector  string `json:"selector,omitempty" jsonschema_description:"Label selector"`
}

type logsInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (required for multi-container pods)"`
	Tail      int    `json:"tail,omitempty" jsonschema_description:"Number of lines from the end (default: 100)"`
	Since     string `json:"since,omitempty" jsonschema_description:"Return logs newer than a duration (e.g. 5m, 1h)"`
	Previous  bool   `json:"previous,omitempty" jsonschema_description:"Show logs from the previous container instance"`
}

type topInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: pods or nodes"`
	Name      string `json:"name,omitempty" jsonschema_description:"Specific pod or node name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (for pods)"`
	Selector  string `json:"selector,omitempty" jsonschema_description:"Label selector (for pods)"`
	Sort      string `json:"sort,omitempty" jsonschema_description:"Sort by: cpu or memory"`
}

type eventsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for default, '-A' for all)"`
	For       string `json:"for,omitempty" jsonschema_description:"Filter events for a specific resource (e.g. pod/nginx-xyz)"`
	Types     string `json:"types,omitempty" jsonschema_description:"Event types to show: Normal, Warning"`
}

type apiResourcesInput struct {
	Namespaced *bool  `json:"namespaced,omitempty" jsonschema_description:"Filter: true for namespaced, false for cluster-scoped"`
	Verbs      string `json:"verbs,omitempty" jsonschema_description:"Filter by supported verbs (e.g. list,get)"`
}

type explainInput struct {
	Resource string `json:"resource" jsonschema_description:"Resource type or field path (e.g. pod, pod.spec.containers, deployment.spec.strategy)"`
}

// ── Handlers ──

func handleGet(_ context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required"), nil, nil
	}
	args := []string{"get", in.Resource}
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}
	output := in.Output
	if output == "" {
		output = "wide"
	}
	args = append(args, "-o", output)
	return kube(args...), nil, nil
}

func handleDescribe(_ context.Context, _ *mcp.CallToolRequest, in describeInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required"), nil, nil
	}
	args := []string{"describe", in.Resource}
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}
	return kube(args...), nil, nil
}

func handleLogs(_ context.Context, _ *mcp.CallToolRequest, in logsInput) (*mcp.CallToolResult, any, error) {
	if in.Pod == "" {
		return errResult("pod is required"), nil, nil
	}
	args := []string{"logs", in.Pod}
	args = appendNamespace(args, in.Namespace)
	if in.Container != "" {
		args = append(args, "-c", in.Container)
	}
	tail := in.Tail
	if tail <= 0 {
		tail = 100
	}
	args = append(args, "--tail", fmt.Sprintf("%d", tail))
	if in.Since != "" {
		args = append(args, "--since", in.Since)
	}
	if in.Previous {
		args = append(args, "--previous")
	}
	return kube(args...), nil, nil
}

func handleTop(_ context.Context, _ *mcp.CallToolRequest, in topInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required (pods or nodes)"), nil, nil
	}
	args := []string{"top", in.Resource}
	if in.Name != "" {
		args = append(args, in.Name)
	}
	args = appendNamespace(args, in.Namespace)
	if in.Selector != "" {
		args = append(args, "-l", in.Selector)
	}
	if in.Sort != "" {
		args = append(args, "--sort-by", in.Sort)
	}
	return kube(args...), nil, nil
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
	return kube(args...), nil, nil
}

func handleAPIResources(_ context.Context, _ *mcp.CallToolRequest, in apiResourcesInput) (*mcp.CallToolResult, any, error) {
	args := []string{"api-resources"}
	if in.Namespaced != nil {
		args = append(args, fmt.Sprintf("--namespaced=%t", *in.Namespaced))
	}
	if in.Verbs != "" {
		args = append(args, "--verbs", in.Verbs)
	}
	return kube(args...), nil, nil
}

func handleExplain(_ context.Context, _ *mcp.CallToolRequest, in explainInput) (*mcp.CallToolResult, any, error) {
	if in.Resource == "" {
		return errResult("resource is required"), nil, nil
	}
	return kube("explain", in.Resource, "--recursive=false"), nil, nil
}

// ── Helpers ──

func appendNamespace(args []string, ns string) []string {
	if ns == "" {
		return args
	}
	if ns == "-A" || strings.EqualFold(ns, "all") {
		return append(args, "--all-namespaces")
	}
	return append(args, "-n", ns)
}

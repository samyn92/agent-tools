/*
MCP Tool: Kubernetes (client-go native)

An MCP stdio server providing Kubernetes operations as tools.
Uses client-go directly — no kubectl dependency, self-contained binary.

Designed to be packaged as an OCI artifact and loaded by any
MCP-compatible agent runtime (Fantasy, Crush, Claude Code, etc.)

Tools provided:
  - kubectl_get         Get/list resources (any type, including CRDs)
  - kubectl_describe    Describe a resource in detail
  - kubectl_logs        Get pod logs
  - kubectl_apply       Apply a YAML/JSON manifest
  - kubectl_delete      Delete a resource
  - kubectl_exec        Execute a command in a pod
  - kubectl_namespaces  List all namespaces
  - kubectl_events      Get events for a namespace or resource
*/
package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

var (
	clientset     *kubernetes.Clientset
	dynClient     dynamic.Interface
	restConfig    *rest.Config
	mapper        *restmapper.DeferredDiscoveryRESTMapper
)

func main() {
	// Initialize Kubernetes client (in-cluster or kubeconfig)
	var err error
	restConfig, err = rest.InClusterConfig()
	if err != nil {
		// Fall back to kubeconfig
		loadingRules := clientcmd.NewDefaultClientConfigLoadingRules()
		configOverrides := &clientcmd.ConfigOverrides{}
		kubeConfig := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(loadingRules, configOverrides)
		restConfig, err = kubeConfig.ClientConfig()
		if err != nil {
			log.Fatalf("Cannot create Kubernetes config: %v", err)
		}
	}

	clientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Cannot create Kubernetes clientset: %v", err)
	}

	dynClient, err = dynamic.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Cannot create dynamic client: %v", err)
	}

	// Deferred discovery mapper for resolving resource types (including CRDs)
	dc := clientset.Discovery()
	mapper = restmapper.NewDeferredDiscoveryRESTMapper(
		&cachedDiscovery{dc},
	)

	server := mcp.NewServer(
		&mcp.Implementation{Name: "kubernetes-tools", Version: "0.2.0"},
		nil,
	)

	addTool(server, "kubectl_get",
		"Get or list Kubernetes resources. Supports any resource type including CRDs. Returns JSON.",
		handleGet)

	addTool(server, "kubectl_describe",
		"Get detailed info about a Kubernetes resource including status, conditions, and events.",
		handleDescribe)

	addTool(server, "kubectl_logs",
		"Get logs from a pod. Supports container selection and tail lines.",
		handleLogs)

	addTool(server, "kubectl_apply",
		"Apply a YAML or JSON manifest. Creates or updates resources.",
		handleApply)

	addTool(server, "kubectl_delete",
		"Delete a Kubernetes resource by type and name.",
		handleDelete)

	addTool(server, "kubectl_exec",
		"Execute a command inside a running pod container.",
		handleExec)

	addTool(server, "kubectl_namespaces",
		"List all namespaces in the cluster.",
		handleNamespaces)

	addTool(server, "kubectl_events",
		"Get events for a namespace, optionally filtered by resource.",
		handleEvents)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// ====================================================================
// Input types
// ====================================================================

type getInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type: pods, services, deployments, nodes, configmaps, agents, agentruns, or any CRD"`
	Name      string `json:"name,omitempty" jsonschema_description:"Specific resource name (omit to list all)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit for current context or cluster-scoped; use 'all' for all namespaces)"`
	Selector  string `json:"selector,omitempty" jsonschema_description:"Label selector (e.g. app=nginx)"`
}

type describeInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pod, service, deployment, agent)"`
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type logsInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (for multi-container pods)"`
	Tail      int64  `json:"tail,omitempty" jsonschema_description:"Number of lines from the end (default: 100)"`
	Previous  bool   `json:"previous,omitempty" jsonschema_description:"Show logs from previous container instance"`
}

type applyInput struct {
	Manifest  string `json:"manifest" jsonschema_description:"YAML or JSON manifest content to apply"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace override (uses manifest namespace if not set)"`
}

type deleteInput struct {
	Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pod, deployment, service, agent)"`
	Name      string `json:"name" jsonschema_description:"Resource name to delete"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}

type execInput struct {
	Pod       string `json:"pod" jsonschema_description:"Pod name"`
	Command   string `json:"command" jsonschema_description:"Command to execute (passed to sh -c)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Container string `json:"container,omitempty" jsonschema_description:"Container name (for multi-container pods)"`
}

type namespacesInput struct{}

type eventsInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace to get events from"`
	Resource  string `json:"resource,omitempty" jsonschema_description:"Filter by involved resource type (e.g. pod)"`
	Name      string `json:"name,omitempty" jsonschema_description:"Filter by involved resource name"`
}

// ====================================================================
// Handlers
// ====================================================================

func handleGet(ctx context.Context, _ *mcp.CallToolRequest, input getInput) (*mcp.CallToolResult, any, error) {
	gvr, namespaced, err := resolveResource(input.Resource)
	if err != nil {
		return errResult("Cannot resolve resource type %q: %v", input.Resource, err), nil, nil
	}

	var res dynamic.ResourceInterface
	if namespaced {
		ns := nsOrDefault(input.Namespace)
		if input.Namespace == "all" {
			res = dynClient.Resource(gvr).Namespace("")
		} else {
			res = dynClient.Resource(gvr).Namespace(ns)
		}
	} else {
		res = dynClient.Resource(gvr)
	}

	opts := metav1.ListOptions{}
	if input.Selector != "" {
		opts.LabelSelector = input.Selector
	}

	if input.Name != "" {
		obj, err := res.Get(ctx, input.Name, metav1.GetOptions{})
		if err != nil {
			return errResult("Error getting %s/%s: %v", input.Resource, input.Name, err), nil, nil
		}
		return jsonResult(formatResource(obj)), nil, nil
	}

	list, err := res.List(ctx, opts)
	if err != nil {
		return errResult("Error listing %s: %v", input.Resource, err), nil, nil
	}

	return jsonResult(formatResourceList(list, input.Resource)), nil, nil
}

func handleDescribe(ctx context.Context, _ *mcp.CallToolRequest, input describeInput) (*mcp.CallToolResult, any, error) {
	gvr, namespaced, err := resolveResource(input.Resource)
	if err != nil {
		return errResult("Cannot resolve resource type %q: %v", input.Resource, err), nil, nil
	}

	var res dynamic.ResourceInterface
	if namespaced {
		res = dynClient.Resource(gvr).Namespace(nsOrDefault(input.Namespace))
	} else {
		res = dynClient.Resource(gvr)
	}

	obj, err := res.Get(ctx, input.Name, metav1.GetOptions{})
	if err != nil {
		return errResult("Error getting %s/%s: %v", input.Resource, input.Name, err), nil, nil
	}

	// Build a describe-like output
	var sb strings.Builder
	fmt.Fprintf(&sb, "Name: %s\n", obj.GetName())
	fmt.Fprintf(&sb, "Namespace: %s\n", obj.GetNamespace())
	fmt.Fprintf(&sb, "Kind: %s\n", obj.GetKind())
	fmt.Fprintf(&sb, "Created: %s\n", obj.GetCreationTimestamp().Format("2006-01-02T15:04:05Z"))

	if labels := obj.GetLabels(); len(labels) > 0 {
		fmt.Fprintf(&sb, "Labels:\n")
		for k, v := range labels {
			fmt.Fprintf(&sb, "  %s: %s\n", k, v)
		}
	}
	if annotations := obj.GetAnnotations(); len(annotations) > 0 {
		fmt.Fprintf(&sb, "Annotations:\n")
		for k, v := range annotations {
			fmt.Fprintf(&sb, "  %s: %s\n", k, truncate(v, 200))
		}
	}

	// Spec
	if spec, ok := obj.Object["spec"]; ok {
		specJSON, _ := json.MarshalIndent(spec, "", "  ")
		fmt.Fprintf(&sb, "\nSpec:\n%s\n", string(specJSON))
	}

	// Status
	if status, ok := obj.Object["status"]; ok {
		statusJSON, _ := json.MarshalIndent(status, "", "  ")
		fmt.Fprintf(&sb, "\nStatus:\n%s\n", string(statusJSON))
	}

	// Events
	events, err := clientset.CoreV1().Events(nsOrDefault(input.Namespace)).List(ctx, metav1.ListOptions{
		FieldSelector: fmt.Sprintf("involvedObject.name=%s", input.Name),
	})
	if err == nil && len(events.Items) > 0 {
		fmt.Fprintf(&sb, "\nEvents:\n")
		for _, e := range events.Items {
			fmt.Fprintf(&sb, "  %s  %s  %s: %s\n",
				e.LastTimestamp.Format("15:04:05"),
				e.Type, e.Reason, e.Message)
		}
	}

	return textResult(sb.String()), nil, nil
}

func handleLogs(ctx context.Context, _ *mcp.CallToolRequest, input logsInput) (*mcp.CallToolResult, any, error) {
	ns := nsOrDefault(input.Namespace)
	tail := input.Tail
	if tail <= 0 {
		tail = 100
	}

	opts := &corev1.PodLogOptions{
		TailLines: &tail,
		Previous:  input.Previous,
	}
	if input.Container != "" {
		opts.Container = input.Container
	}

	stream, err := clientset.CoreV1().Pods(ns).GetLogs(input.Pod, opts).Stream(ctx)
	if err != nil {
		return errResult("Error getting logs for %s: %v", input.Pod, err), nil, nil
	}
	defer stream.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, stream); err != nil {
		return errResult("Error reading logs: %v", err), nil, nil
	}

	return textResult(buf.String()), nil, nil
}

func handleApply(ctx context.Context, _ *mcp.CallToolRequest, input applyInput) (*mcp.CallToolResult, any, error) {
	// Decode YAML/JSON to unstructured
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(input.Manifest), 4096)
	var results []string

	for {
		var rawObj map[string]interface{}
		if err := decoder.Decode(&rawObj); err != nil {
			if err == io.EOF {
				break
			}
			return errResult("Error parsing manifest: %v", err), nil, nil
		}
		if rawObj == nil {
			continue
		}

		obj := &unstructured.Unstructured{Object: rawObj}

		gvk := obj.GroupVersionKind()
		mapping, err := mapper.RESTMapping(gvk.GroupKind(), gvk.Version)
		if err != nil {
			return errResult("Cannot resolve resource mapping for %s: %v", gvk.String(), err), nil, nil
		}

		ns := obj.GetNamespace()
		if input.Namespace != "" {
			ns = input.Namespace
			obj.SetNamespace(ns)
		}

		var res dynamic.ResourceInterface
		if mapping.Scope.Name() == "namespace" {
			if ns == "" {
				ns = "default"
			}
			res = dynClient.Resource(mapping.Resource).Namespace(ns)
		} else {
			res = dynClient.Resource(mapping.Resource)
		}

		// Server-side apply
		data, err := json.Marshal(obj)
		if err != nil {
			return errResult("Error marshaling: %v", err), nil, nil
		}

		result, err := res.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "agenticops-mcp-tool",
		})
		if err != nil {
			return errResult("Error applying %s/%s: %v", obj.GetKind(), obj.GetName(), err), nil, nil
		}

		results = append(results, fmt.Sprintf("%s/%s configured", result.GetKind(), result.GetName()))
	}

	if len(results) == 0 {
		return errResult("No resources found in manifest"), nil, nil
	}
	return textResult(strings.Join(results, "\n")), nil, nil
}

func handleDelete(ctx context.Context, _ *mcp.CallToolRequest, input deleteInput) (*mcp.CallToolResult, any, error) {
	gvr, namespaced, err := resolveResource(input.Resource)
	if err != nil {
		return errResult("Cannot resolve resource type %q: %v", input.Resource, err), nil, nil
	}

	var res dynamic.ResourceInterface
	if namespaced {
		res = dynClient.Resource(gvr).Namespace(nsOrDefault(input.Namespace))
	} else {
		res = dynClient.Resource(gvr)
	}

	if err := res.Delete(ctx, input.Name, metav1.DeleteOptions{}); err != nil {
		return errResult("Error deleting %s/%s: %v", input.Resource, input.Name, err), nil, nil
	}

	return textResult(fmt.Sprintf("%s/%s deleted", input.Resource, input.Name)), nil, nil
}

func handleExec(ctx context.Context, _ *mcp.CallToolRequest, input execInput) (*mcp.CallToolResult, any, error) {
	ns := nsOrDefault(input.Namespace)

	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(input.Pod).
		Namespace(ns).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: input.Container,
			Command:   []string{"sh", "-c", input.Command},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return errResult("Error creating executor: %v", err), nil, nil
	}

	var stdout, stderr bytes.Buffer
	err = exec.StreamWithContext(ctx, remotecommand.StreamOptions{
		Stdout: &stdout,
		Stderr: &stderr,
	})

	output := stdout.String()
	if stderr.Len() > 0 {
		output += "\nSTDERR:\n" + stderr.String()
	}
	if err != nil {
		return errResult("Exec error: %v\n%s", err, output), nil, nil
	}

	return textResult(output), nil, nil
}

func handleNamespaces(ctx context.Context, _ *mcp.CallToolRequest, _ namespacesInput) (*mcp.CallToolResult, any, error) {
	list, err := clientset.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return errResult("Error listing namespaces: %v", err), nil, nil
	}

	var sb strings.Builder
	for _, ns := range list.Items {
		fmt.Fprintf(&sb, "%-30s %s\n", ns.Name, ns.Status.Phase)
	}
	return textResult(sb.String()), nil, nil
}

func handleEvents(ctx context.Context, _ *mcp.CallToolRequest, input eventsInput) (*mcp.CallToolResult, any, error) {
	ns := nsOrDefault(input.Namespace)

	opts := metav1.ListOptions{}
	if input.Name != "" {
		opts.FieldSelector = fmt.Sprintf("involvedObject.name=%s", input.Name)
	}

	events, err := clientset.CoreV1().Events(ns).List(ctx, opts)
	if err != nil {
		return errResult("Error listing events: %v", err), nil, nil
	}

	if len(events.Items) == 0 {
		return textResult("No events found."), nil, nil
	}

	var sb strings.Builder
	for _, e := range events.Items {
		fmt.Fprintf(&sb, "%s  %-8s %-20s %-30s %s\n",
			e.LastTimestamp.Format("15:04:05"),
			e.Type, e.Reason,
			fmt.Sprintf("%s/%s", e.InvolvedObject.Kind, e.InvolvedObject.Name),
			e.Message)
	}
	return textResult(sb.String()), nil, nil
}

// ====================================================================
// Resource resolution (supports CRDs)
// ====================================================================

func resolveResource(resource string) (schema.GroupVersionResource, bool, error) {
	// Common shortcuts
	shortcuts := map[string]schema.GroupVersionResource{
		"po": {Version: "v1", Resource: "pods"},                "pods": {Version: "v1", Resource: "pods"},
		"svc": {Version: "v1", Resource: "services"},           "services": {Version: "v1", Resource: "services"},
		"deploy": {Group: "apps", Version: "v1", Resource: "deployments"}, "deployments": {Group: "apps", Version: "v1", Resource: "deployments"},
		"ds": {Group: "apps", Version: "v1", Resource: "daemonsets"},       "daemonsets": {Group: "apps", Version: "v1", Resource: "daemonsets"},
		"sts": {Group: "apps", Version: "v1", Resource: "statefulsets"},    "statefulsets": {Group: "apps", Version: "v1", Resource: "statefulsets"},
		"cm": {Version: "v1", Resource: "configmaps"},          "configmaps": {Version: "v1", Resource: "configmaps"},
		"secret": {Version: "v1", Resource: "secrets"},         "secrets": {Version: "v1", Resource: "secrets"},
		"ns": {Version: "v1", Resource: "namespaces"},          "namespaces": {Version: "v1", Resource: "namespaces"},
		"no": {Version: "v1", Resource: "nodes"},               "nodes": {Version: "v1", Resource: "nodes"},
		"pvc": {Version: "v1", Resource: "persistentvolumeclaims"}, "persistentvolumeclaims": {Version: "v1", Resource: "persistentvolumeclaims"},
		"pv": {Version: "v1", Resource: "persistentvolumes"},   "persistentvolumes": {Version: "v1", Resource: "persistentvolumes"},
		"ing": {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, "ingresses": {Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"},
		"job": {Group: "batch", Version: "v1", Resource: "jobs"}, "jobs": {Group: "batch", Version: "v1", Resource: "jobs"},
		"cj": {Group: "batch", Version: "v1", Resource: "cronjobs"}, "cronjobs": {Group: "batch", Version: "v1", Resource: "cronjobs"},
		"sa": {Version: "v1", Resource: "serviceaccounts"}, "serviceaccounts": {Version: "v1", Resource: "serviceaccounts"},
		"events": {Version: "v1", Resource: "events"}, "ev": {Version: "v1", Resource: "events"},
		"ep": {Version: "v1", Resource: "endpoints"}, "endpoints": {Version: "v1", Resource: "endpoints"},
		// AgenticOps CRDs
		"ag": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "agents"}, "agents": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "agents"},
		"ar": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "agentruns"}, "agentruns": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "agentruns"},
		"ch": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "channels"}, "channels": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "channels"},
		"mcp": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "mcpservers"}, "mcpservers": {Group: "agents.agenticops.io", Version: "v1alpha1", Resource: "mcpservers"},
	}

	lower := strings.ToLower(resource)
	if gvr, ok := shortcuts[lower]; ok {
		namespaced := lower != "nodes" && lower != "no" && lower != "namespaces" && lower != "ns" && lower != "pv" && lower != "persistentvolumes"
		return gvr, namespaced, nil
	}

	// Try discovery for unknown resources
	resources, err := clientset.Discovery().ServerPreferredResources()
	if err != nil {
		return schema.GroupVersionResource{}, false, fmt.Errorf("discovery failed: %v", err)
	}

	for _, resList := range resources {
		gv, _ := schema.ParseGroupVersion(resList.GroupVersion)
		for _, r := range resList.APIResources {
			if strings.EqualFold(r.Name, lower) || strings.EqualFold(r.Kind, resource) {
				return schema.GroupVersionResource{
					Group:    gv.Group,
					Version:  gv.Version,
					Resource: r.Name,
				}, r.Namespaced, nil
			}
			// Check short names
			for _, sn := range r.ShortNames {
				if strings.EqualFold(sn, lower) {
					return schema.GroupVersionResource{
						Group:    gv.Group,
						Version:  gv.Version,
						Resource: r.Name,
					}, r.Namespaced, nil
				}
			}
		}
	}

	return schema.GroupVersionResource{}, false, fmt.Errorf("unknown resource type: %s", resource)
}

// ====================================================================
// Formatting
// ====================================================================

func formatResource(obj *unstructured.Unstructured) string {
	// Clean output: just the key fields
	output := map[string]interface{}{
		"name":      obj.GetName(),
		"namespace": obj.GetNamespace(),
		"kind":      obj.GetKind(),
		"created":   obj.GetCreationTimestamp().Format("2006-01-02T15:04:05Z"),
		"labels":    obj.GetLabels(),
	}
	if spec, ok := obj.Object["spec"]; ok {
		output["spec"] = spec
	}
	if status, ok := obj.Object["status"]; ok {
		output["status"] = status
	}
	data, _ := json.MarshalIndent(output, "", "  ")
	return string(data)
}

func formatResourceList(list *unstructured.UnstructuredList, resourceType string) string {
	if len(list.Items) == 0 {
		return fmt.Sprintf("No %s found.", resourceType)
	}

	var sb strings.Builder
	fmt.Fprintf(&sb, "%s (%d items):\n\n", resourceType, len(list.Items))

	for _, item := range list.Items {
		ns := item.GetNamespace()
		name := item.GetName()

		// Try to extract phase/status for common resource types
		phase := ""
		if status, ok := item.Object["status"].(map[string]interface{}); ok {
			if p, ok := status["phase"].(string); ok {
				phase = p
			}
			if conditions, ok := status["conditions"].([]interface{}); ok && phase == "" {
				for _, c := range conditions {
					if cm, ok := c.(map[string]interface{}); ok {
						if cm["type"] == "Ready" || cm["type"] == "Available" {
							phase = fmt.Sprintf("%s=%s", cm["type"], cm["status"])
							break
						}
					}
				}
			}
		}

		if ns != "" {
			fmt.Fprintf(&sb, "  %s/%s", ns, name)
		} else {
			fmt.Fprintf(&sb, "  %s", name)
		}
		if phase != "" {
			fmt.Fprintf(&sb, "  (%s)", phase)
		}
		fmt.Fprintln(&sb)
	}

	return sb.String()
}

// ====================================================================
// Helpers
// ====================================================================

func addTool[In any](s *mcp.Server, name, description string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: description}, h)
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func jsonResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
}

func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

func nsOrDefault(ns string) string {
	if ns == "" || ns == "all" {
		return "default"
	}
	return ns
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// cachedDiscovery wraps DiscoveryInterface to satisfy DeferredDiscoveryRESTMapper.
type cachedDiscovery struct {
	discovery.DiscoveryInterface
}

func (c *cachedDiscovery) Fresh() bool { return true }
func (c *cachedDiscovery) Invalidate() {}

/*
k8s.go — Kubernetes client initialization and shared helpers.

Initializes client-go (in-cluster or kubeconfig fallback) and exposes
the shared clients used by all tool handlers.
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
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/client-go/discovery"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/scheme"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/restmapper"
	"k8s.io/client-go/tools/clientcmd"
	"k8s.io/client-go/tools/remotecommand"
)

// Global clients — initialized once in initClients().
var (
	clientset  *kubernetes.Clientset
	dynClient  dynamic.Interface
	restConfig *rest.Config
	mapper     *restmapper.DeferredDiscoveryRESTMapper
)

// initClients initializes the Kubernetes clients. Called once from main().
func initClients() {
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

	// Increase QPS/burst for cross-namespace parallel scanning
	restConfig.QPS = 50
	restConfig.Burst = 100

	clientset, err = kubernetes.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Cannot create Kubernetes clientset: %v", err)
	}

	dynClient, err = dynamic.NewForConfig(restConfig)
	if err != nil {
		log.Fatalf("Cannot create dynamic client: %v", err)
	}

	dc := clientset.Discovery()
	mapper = restmapper.NewDeferredDiscoveryRESTMapper(
		&cachedDiscovery{
			DiscoveryInterface: dc,
			lastRefresh:        time.Now(),
			ttl:                5 * time.Minute,
		},
	)
}

// ====================================================================
// MCP result helpers
// ====================================================================

func jsonMarshalResult(v any) *mcp.CallToolResult {
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return mcputil.ErrResult("json marshal error: %v", err)
	}
	return mcputil.TextResult(string(data))
}

// ====================================================================
// Kubernetes helpers
// ====================================================================

// nsOrDefault returns the namespace, or "default" if empty.
func nsOrDefault(ns string) string {
	if ns == "" {
		return "default"
	}
	return ns
}

// allNamespaces returns "" which means all namespaces in client-go list calls.
func allNamespaces() string {
	return ""
}

// truncate limits a string to n characters.
func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// humanAge formats a duration into a human-readable age string like "5m", "2h", "14d".
func humanAge(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return fmt.Sprintf("%ds", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	default:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	}
}

// getPodStatus extracts the effective pod status string, accounting for
// container waiting reasons (CrashLoopBackOff, ImagePullBackOff, etc.)
// which are more useful than the bare phase.
func getPodStatus(obj *unstructured.Unstructured) string {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return "Unknown"
	}

	phase, _ := status["phase"].(string)

	// Check container statuses for more specific reasons
	containerStatuses, _ := status["containerStatuses"].([]interface{})
	for _, cs := range containerStatuses {
		csMap, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}
		if waiting, ok := csMap["waiting"].(map[string]interface{}); ok {
			if reason, ok := waiting["reason"].(string); ok && reason != "" {
				return reason
			}
		}
		if terminated, ok := csMap["terminated"].(map[string]interface{}); ok {
			if reason, ok := terminated["reason"].(string); ok && reason != "" {
				return reason
			}
		}
	}

	// Check init container statuses
	initStatuses, _ := status["initContainerStatuses"].([]interface{})
	for _, cs := range initStatuses {
		csMap, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}
		if waiting, ok := csMap["waiting"].(map[string]interface{}); ok {
			if reason, ok := waiting["reason"].(string); ok && reason != "" {
				return "Init:" + reason
			}
		}
	}

	if phase != "" {
		return phase
	}
	return "Unknown"
}

// getRestartCount returns the total restart count across all containers.
func getRestartCount(obj *unstructured.Unstructured) int64 {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return 0
	}
	containerStatuses, _ := status["containerStatuses"].([]interface{})
	var total int64
	for _, cs := range containerStatuses {
		csMap, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}
		if rc, ok := csMap["restartCount"].(int64); ok {
			total += rc
		} else if rc, ok := csMap["restartCount"].(float64); ok {
			total += int64(rc)
		}
	}
	return total
}

// getReplicaCounts returns "ready/desired" for Deployments, StatefulSets, etc.
func getReplicaCounts(obj *unstructured.Unstructured) string {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return ""
	}
	spec, _ := obj.Object["spec"].(map[string]interface{})

	desired := int64(1)
	if spec != nil {
		if r, ok := spec["replicas"].(int64); ok {
			desired = r
		} else if r, ok := spec["replicas"].(float64); ok {
			desired = int64(r)
		}
	}

	ready := int64(0)
	if r, ok := status["readyReplicas"].(int64); ok {
		ready = r
	} else if r, ok := status["readyReplicas"].(float64); ok {
		ready = int64(r)
	}

	return fmt.Sprintf("%d/%d", ready, desired)
}

// getResourceStatus extracts a human-readable status for any resource type.
func getResourceStatus(obj *unstructured.Unstructured) string {
	kind := obj.GetKind()

	switch kind {
	case "Pod":
		return getPodStatus(obj)
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		return getConditionStatus(obj)
	case "Job":
		return getJobStatus(obj)
	case "PersistentVolumeClaim":
		if status, ok := obj.Object["status"].(map[string]interface{}); ok {
			if phase, ok := status["phase"].(string); ok {
				return phase
			}
		}
		return "Unknown"
	case "Node":
		return getNodeStatus(obj)
	default:
		return getConditionStatus(obj)
	}
}

// getConditionStatus extracts Ready or Available condition status.
func getConditionStatus(obj *unstructured.Unstructured) string {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return ""
	}
	conditions, ok := status["conditions"].([]interface{})
	if !ok {
		return ""
	}
	for _, c := range conditions {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cType, _ := cm["type"].(string)
		cStatus, _ := cm["status"].(string)
		if cType == "Available" || cType == "Ready" {
			if cStatus == "True" {
				return cType
			}
			reason, _ := cm["reason"].(string)
			if reason != "" {
				return reason
			}
			return cType + "=False"
		}
	}
	return ""
}

// getJobStatus extracts job completion status.
func getJobStatus(obj *unstructured.Unstructured) string {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return "Unknown"
	}
	conditions, _ := status["conditions"].([]interface{})
	for _, c := range conditions {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cType, _ := cm["type"].(string)
		cStatus, _ := cm["status"].(string)
		if cType == "Complete" && cStatus == "True" {
			return "Complete"
		}
		if cType == "Failed" && cStatus == "True" {
			return "Failed"
		}
	}

	active, _ := status["active"].(float64)
	if active > 0 {
		return "Running"
	}
	return "Pending"
}

// getNodeStatus returns the node's Ready condition status.
func getNodeStatus(obj *unstructured.Unstructured) string {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return "Unknown"
	}
	conditions, _ := status["conditions"].([]interface{})
	for _, c := range conditions {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		if cm["type"] == "Ready" {
			if cm["status"] == "True" {
				return "Ready"
			}
			return "NotReady"
		}
	}
	return "Unknown"
}

// isPodUnhealthy returns true if a pod is in a bad state.
func isPodUnhealthy(obj *unstructured.Unstructured) bool {
	s := getPodStatus(obj)
	switch s {
	case "Running", "Succeeded", "Completed":
		return false
	case "Pending":
		// Pending is only unhealthy if it's been pending for a while
		created := obj.GetCreationTimestamp()
		return time.Since(created.Time) > 2*time.Minute
	default:
		// CrashLoopBackOff, ImagePullBackOff, Error, OOMKilled, etc.
		return true
	}
}

// getTerminationReason extracts the last termination reason and exit code for a pod.
func getTerminationReason(obj *unstructured.Unstructured) (string, int64) {
	status, ok := obj.Object["status"].(map[string]interface{})
	if !ok {
		return "", 0
	}
	containerStatuses, _ := status["containerStatuses"].([]interface{})
	for _, cs := range containerStatuses {
		csMap, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}
		if lastState, ok := csMap["lastState"].(map[string]interface{}); ok {
			if terminated, ok := lastState["terminated"].(map[string]interface{}); ok {
				reason, _ := terminated["reason"].(string)
				exitCode := int64(0)
				if ec, ok := terminated["exitCode"].(float64); ok {
					exitCode = int64(ec)
				}
				return reason, exitCode
			}
		}
	}
	return "", 0
}

// getPodLogs fetches logs for a pod, returning the log text and whether it was truncated.
func getPodLogs(ctx context.Context, namespace, podName, container string, tailLines int64, previous bool) (string, error) {
	opts := &corev1.PodLogOptions{
		TailLines: &tailLines,
		Previous:  previous,
	}
	if container != "" {
		opts.Container = container
	}

	stream, err := clientset.CoreV1().Pods(namespace).GetLogs(podName, opts).Stream(ctx)
	if err != nil {
		return "", err
	}
	defer stream.Close()

	buf := new(bytes.Buffer)
	if _, err := io.Copy(buf, stream); err != nil {
		return "", err
	}
	return buf.String(), nil
}

// highlightErrorLines filters log lines to only those containing error indicators.
func highlightErrorLines(logs string, maxLines int) string {
	keywords := []string{"error", "Error", "ERROR", "panic", "PANIC", "fatal", "FATAL",
		"FAIL", "fail", "exception", "Exception", "EXCEPTION",
		"OOMKilled", "OutOfMemory", "killed", "segfault"}

	lines := strings.Split(logs, "\n")
	var errorLines []string
	for _, line := range lines {
		for _, kw := range keywords {
			if strings.Contains(line, kw) {
				errorLines = append(errorLines, line)
				break
			}
		}
	}

	if len(errorLines) > maxLines {
		errorLines = errorLines[len(errorLines)-maxLines:]
	}
	return strings.Join(errorLines, "\n")
}

// execInPod executes a command in a pod and returns stdout+stderr.
func execInPod(ctx context.Context, namespace, podName, container, command string) (string, error) {
	req := clientset.CoreV1().RESTClient().Post().
		Resource("pods").
		Name(podName).
		Namespace(namespace).
		SubResource("exec").
		VersionedParams(&corev1.PodExecOptions{
			Container: container,
			Command:   []string{"sh", "-c", command},
			Stdout:    true,
			Stderr:    true,
		}, scheme.ParameterCodec)

	exec, err := remotecommand.NewSPDYExecutor(restConfig, "POST", req.URL())
	if err != nil {
		return "", fmt.Errorf("creating executor: %w", err)
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
		return output, fmt.Errorf("exec error: %w", err)
	}
	return output, nil
}

// getEvents fetches events for a specific object or all events in a namespace.
func getEvents(ctx context.Context, namespace, name string) ([]corev1.Event, error) {
	opts := metav1.ListOptions{}
	if name != "" {
		opts.FieldSelector = fmt.Sprintf("involvedObject.name=%s", name)
	}

	events, err := clientset.CoreV1().Events(namespace).List(ctx, opts)
	if err != nil {
		return nil, err
	}
	return events.Items, nil
}

// cachedDiscovery wraps DiscoveryInterface for DeferredDiscoveryRESTMapper.
// Invalidates the cache after a TTL to pick up new CRDs and API changes.
type cachedDiscovery struct {
	discovery.DiscoveryInterface
	lastRefresh time.Time
	ttl         time.Duration
}

func (c *cachedDiscovery) Fresh() bool {
	return time.Since(c.lastRefresh) < c.ttl
}

func (c *cachedDiscovery) Invalidate() {
	c.lastRefresh = time.Time{} // zero time forces refresh on next Fresh() check
}

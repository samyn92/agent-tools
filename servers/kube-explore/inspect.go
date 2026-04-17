/*
inspect.go — kube_inspect tool handler.

Deep inspection of a single resource. Returns: full spec, status, conditions,
events, logs (if pod/job), owner chain (Pod->ReplicaSet->Deployment),
and related resources (Service, Ingress, PVC, ConfigMap, Secret).

Resolves the target resource via fuzzy matching first, so exact names
and namespaces are not required.
*/
package main

import (
	"context"
	"strings"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

type inspectInput struct {
	Name      string `json:"name" jsonschema_description:"Resource name (fuzzy matching supported)"`
	Kind      string `json:"kind,omitempty" jsonschema_description:"Resource kind (e.g. Pod, Deployment). If omitted, searches all types."`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit to search all namespaces)"`
}

func handleInspect(ctx context.Context, _ *mcp.CallToolRequest, input inspectInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return mcputil.ErrResult("'name' is required"), nil, nil
	}

	// First, find the resource using fuzzy matching
	obj, kind, err := fuzzyFindOne(ctx, input.Name, input.Kind, input.Namespace)
	if err != nil {
		return mcputil.ErrResult("Resource not found: %v", err), nil, nil
	}

	// Build the inspection response in parallel
	response := InspectResponse{
		Resource: InspectResource{
			Kind:        kind,
			Namespace:   obj.GetNamespace(),
			Name:        obj.GetName(),
			Created:     obj.GetCreationTimestamp().Format("2006-01-02T15:04:05Z"),
			Labels:      obj.GetLabels(),
			Annotations: filterAnnotations(obj.GetAnnotations()),
		},
	}

	// Extract status
	response.Status = extractInspectStatus(obj, kind)

	// Parallel: owner chain, events, logs, related resources
	var wg sync.WaitGroup
	var mu sync.Mutex

	// Owner chain
	wg.Add(1)
	go func() {
		defer wg.Done()
		chain, _ := getOwnerChain(ctx, obj)
		mu.Lock()
		response.OwnerChain = chain
		mu.Unlock()
	}()

	// Events
	wg.Add(1)
	go func() {
		defer wg.Done()
		events, err := getEvents(ctx, obj.GetNamespace(), obj.GetName())
		if err == nil && len(events) > 0 {
			var inspectEvents []InspectEvent
			for _, e := range events {
				age := humanAge(e.LastTimestamp.Time)
				if e.LastTimestamp.IsZero() {
					age = humanAge(e.CreationTimestamp.Time)
				}
				inspectEvents = append(inspectEvents, InspectEvent{
					Type:    e.Type,
					Reason:  e.Reason,
					Message: truncate(e.Message, 300),
					Age:     age,
					Count:   e.Count,
				})
			}
			mu.Lock()
			response.Events = inspectEvents
			mu.Unlock()
		}
	}()

	// Logs (only for Pods and Jobs)
	if kind == "Pod" || kind == "Job" {
		wg.Add(1)
		go func() {
			defer wg.Done()
			logs := fetchInspectLogs(ctx, obj, kind)
			mu.Lock()
			response.Logs = logs
			mu.Unlock()
		}()
	}

	// Related resources
	wg.Add(1)
	go func() {
		defer wg.Done()
		// For Pods, inspect the owner (Deployment/StatefulSet) for related resources
		inspectTarget := obj
		if kind == "Pod" {
			root, err := getRootOwner(ctx, obj)
			if err == nil && root.GetKind() != "Pod" {
				inspectTarget = root
			}
		}
		related, err := findRelatedResources(ctx, inspectTarget)
		if err == nil {
			mu.Lock()
			response.RelatedResources = related.Flatten()
			mu.Unlock()
		}
	}()

	wg.Wait()

	return jsonMarshalResult(response), nil, nil
}

// fuzzyFindOne uses the find logic to locate a single best-match resource.
func fuzzyFindOne(ctx context.Context, name, kind, namespace string) (*unstructured.Unstructured, string, error) {
	// Determine which resource types to search
	var searchGVRs []resourceInfo
	if kind != "" {
		if info, ok := resolveKind(kind); ok {
			searchGVRs = []resourceInfo{info}
		} else {
			// Try resolve as a resource type string
			info, err := resolveResource(kind)
			if err != nil {
				return nil, "", err
			}
			searchGVRs = []resourceInfo{info}
		}
	} else {
		searchGVRs = defaultSearchGVRs
	}

	var (
		mu        sync.Mutex
		wg        sync.WaitGroup
		bestMatch *FuzzyMatch
	)

	for _, gvrInfo := range searchGVRs {
		wg.Add(1)
		go func(info resourceInfo) {
			defer wg.Done()
			ns := namespace
			var iface = dynClient.Resource(info.GVR).Namespace(ns)

			// Try exact get first (fastest path)
			if name != "" && namespace != "" {
				obj, err := iface.Get(ctx, name, metav1.GetOptions{})
				if err == nil {
					mu.Lock()
					bestMatch = &FuzzyMatch{Object: obj, Kind: info.Kind, Score: 1.0, MatchReason: "exact match"}
					mu.Unlock()
					return
				}
			}

			list, err := iface.List(ctx, metav1.ListOptions{})
			if err != nil {
				return
			}

			for i := range list.Items {
				item := &list.Items[i]
				m := fuzzyMatchObject(name, item, info.Kind)
				if m != nil {
					mu.Lock()
					if bestMatch == nil || m.Score > bestMatch.Score {
						bestMatch = m
					}
					mu.Unlock()
				}
			}
		}(gvrInfo)
	}

	wg.Wait()

	if bestMatch == nil {
		return nil, "", errNotFound(name)
	}

	return bestMatch.Object, bestMatch.Kind, nil
}

// extractInspectStatus builds the InspectStatus for any resource type.
func extractInspectStatus(obj *unstructured.Unstructured, kind string) InspectStatus {
	result := InspectStatus{}

	status, _ := obj.Object["status"].(map[string]interface{})
	if status == nil {
		return result
	}

	// Phase
	if phase, ok := status["phase"].(string); ok {
		result.Phase = phase
	} else {
		result.Phase = getResourceStatus(obj)
	}

	// Conditions
	if conditions, ok := status["conditions"].([]interface{}); ok {
		for _, c := range conditions {
			cm, ok := c.(map[string]interface{})
			if !ok {
				continue
			}
			cType, _ := cm["type"].(string)
			cStatus, _ := cm["status"].(string)
			reason, _ := cm["reason"].(string)
			message, _ := cm["message"].(string)
			result.Conditions = append(result.Conditions, InspectCondition{
				Type:    cType,
				Status:  cStatus,
				Reason:  reason,
				Message: truncate(message, 200),
			})
		}
	}

	// Container statuses (for Pods)
	if kind == "Pod" {
		result.Containers = extractContainerStatuses(status)
	}

	// Replicas (for workload controllers)
	switch kind {
	case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
		result.Replicas = getReplicaCounts(obj)
	}

	return result
}

// extractContainerStatuses builds ContainerStatus entries from pod status.
func extractContainerStatuses(status map[string]interface{}) []ContainerStatus {
	containerStatuses, _ := status["containerStatuses"].([]interface{})
	var results []ContainerStatus

	for _, cs := range containerStatuses {
		csMap, ok := cs.(map[string]interface{})
		if !ok {
			continue
		}

		name, _ := csMap["name"].(string)
		image, _ := csMap["image"].(string)
		restarts := int64(0)
		if rc, ok := csMap["restartCount"].(float64); ok {
			restarts = int64(rc)
		}

		state := "unknown"
		var reason string
		var lastTerm *TerminationInfo

		if running, ok := csMap["state"].(map[string]interface{})["running"]; ok && running != nil {
			state = "running"
		}
		if waiting, ok := csMap["state"].(map[string]interface{})["waiting"].(map[string]interface{}); ok {
			state = "waiting"
			reason, _ = waiting["reason"].(string)
		}
		if terminated, ok := csMap["state"].(map[string]interface{})["terminated"].(map[string]interface{}); ok {
			state = "terminated"
			reason, _ = terminated["reason"].(string)
		}

		// Last termination state
		if lastState, ok := csMap["lastState"].(map[string]interface{}); ok {
			if terminated, ok := lastState["terminated"].(map[string]interface{}); ok {
				termReason, _ := terminated["reason"].(string)
				exitCode := int64(0)
				if ec, ok := terminated["exitCode"].(float64); ok {
					exitCode = int64(ec)
				}
				lastTerm = &TerminationInfo{
					Reason:   termReason,
					ExitCode: exitCode,
				}
			}
		}

		results = append(results, ContainerStatus{
			Name:            name,
			Image:           image,
			State:           state,
			Reason:          reason,
			Restarts:        restarts,
			LastTermination: lastTerm,
		})
	}

	return results
}

// fetchInspectLogs gets logs for a pod, handling crash-looping containers.
func fetchInspectLogs(ctx context.Context, obj *unstructured.Unstructured, kind string) *InspectLogs {
	result := &InspectLogs{}

	podName := obj.GetName()
	namespace := obj.GetNamespace()

	// For Jobs, we need to find the pod first
	if kind == "Job" {
		pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{
			LabelSelector: "job-name=" + podName,
		})
		if err != nil || len(pods.Items) == 0 {
			return nil
		}
		podName = pods.Items[0].Name
	}

	// Fetch current logs
	current, err := getPodLogs(ctx, namespace, podName, "", 100, false)
	if err == nil && current != "" {
		totalLines := strings.Count(current, "\n")
		result.TotalLines = totalLines
		result.Truncated = totalLines > 100

		// If logs are long, show error lines only
		if totalLines > 50 {
			errorLines := highlightErrorLines(current, 30)
			if errorLines != "" {
				result.Current = errorLines
				result.ErrorLinesShown = true
			} else {
				// No error lines found, show last 50 lines
				lines := strings.Split(current, "\n")
				if len(lines) > 50 {
					result.Current = strings.Join(lines[len(lines)-50:], "\n")
					result.Truncated = true
				} else {
					result.Current = current
				}
			}
		} else {
			result.Current = current
		}
	}

	// Check if crash-looping — fetch previous logs
	restarts := getRestartCount(obj)
	if restarts > 0 {
		previous, err := getPodLogs(ctx, namespace, podName, "", 50, true)
		if err == nil && previous != "" {
			result.Previous = previous
		}
	}

	return result
}

// filterAnnotations removes noisy system annotations.
func filterAnnotations(annotations map[string]string) map[string]string {
	if len(annotations) == 0 {
		return nil
	}

	noisy := []string{
		"kubectl.kubernetes.io/last-applied-configuration",
		"deployment.kubernetes.io/revision",
		"cni.projectcalico.org/",
		"kubernetes.io/psp",
	}

	result := make(map[string]string)
	for k, v := range annotations {
		skip := false
		for _, prefix := range noisy {
			if strings.HasPrefix(k, prefix) {
				skip = true
				break
			}
		}
		if !skip {
			result[k] = truncate(v, 200)
		}
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

type notFoundError struct {
	name string
}

func (e *notFoundError) Error() string {
	return "no resource found matching '" + e.name + "'"
}

func errNotFound(name string) error {
	return &notFoundError{name: name}
}

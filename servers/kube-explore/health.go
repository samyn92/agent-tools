/*
health.go — kube_health tool handler.

Full cluster health snapshot in a single call: unhealthy pods, pending PVCs,
failed jobs, recent error events, node conditions, resource pressure warnings.
All resource types are scanned in parallel using errgroup.
*/
package main

import (
	"context"
	"sort"
	"sync"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type healthInput struct {
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace to check (omit for cluster-wide health)"`
}

func handleHealth(ctx context.Context, _ *mcp.CallToolRequest, input healthInput) (*mcp.CallToolResult, any, error) {
	ns := input.Namespace // Empty = all namespaces

	var (
		mu       sync.Mutex
		wg       sync.WaitGroup
		response = HealthResponse{
			Scope: "cluster",
		}
	)
	if ns != "" {
		response.Scope = "namespace:" + ns
	}

	// Parallel scans for all resource types
	wg.Add(4)

	// 1. Scan pods
	go func() {
		defer wg.Done()
		pods, err := listResources(ctx, schema.GroupVersionResource{Version: "v1", Resource: "pods"}, ns)
		if err != nil {
			return
		}

		var running, unhealthy, total int
		var unhealthyResources []UnhealthyResource

		for i := range pods {
			pod := &pods[i]
			total++

			if isPodUnhealthy(pod) {
				unhealthy++
				status := getPodStatus(pod)
				reason, _ := getTerminationReason(pod)
				if reason == "" {
					reason = status
				}
				owner := getOwnerString(pod)

				unhealthyResources = append(unhealthyResources, UnhealthyResource{
					Kind:      "Pod",
					Namespace: pod.GetNamespace(),
					Name:      pod.GetName(),
					Status:    status,
					Reason:    reason,
					Restarts:  getRestartCount(pod),
					Age:       humanAge(pod.GetCreationTimestamp().Time),
					Owner:     owner,
				})
			} else {
				s := getPodStatus(pod)
				if s == "Running" || s == "Succeeded" {
					running++
				}
			}
		}

		// Sort unhealthy by restarts descending (worst first)
		sort.Slice(unhealthyResources, func(i, j int) bool {
			return unhealthyResources[i].Restarts > unhealthyResources[j].Restarts
		})

		mu.Lock()
		response.Summary.Pods = PodCount{Running: running, Unhealthy: unhealthy, Total: total}
		response.UnhealthyResources = append(response.UnhealthyResources, unhealthyResources...)
		mu.Unlock()
	}()

	// 2. Scan nodes
	go func() {
		defer wg.Done()
		nodes, err := listResources(ctx, schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, "")
		if err != nil {
			return
		}

		var ready, total int
		var conditions []NodeCondition

		for i := range nodes {
			node := &nodes[i]
			total++
			status := getNodeStatus(node)
			if status == "Ready" {
				ready++
			} else {
				conditions = append(conditions, NodeCondition{
					Name:   node.GetName(),
					Status: status,
				})
			}

			// Check for resource pressure conditions
			checkNodePressure(node, &conditions)
		}

		mu.Lock()
		response.Summary.Nodes = ResourceCount{Ready: ready, Total: total}
		response.NodeConditions = conditions
		mu.Unlock()
	}()

	// 3. Scan PVCs + Jobs
	go func() {
		defer wg.Done()

		// PVCs
		pvcs, err := listResources(ctx, schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, ns)
		if err == nil {
			var bound, total int
			for i := range pvcs {
				pvc := &pvcs[i]
				total++
				status, _ := nestedString(pvc.Object, "status", "phase")
				if status == "Bound" {
					bound++
				} else {
					mu.Lock()
					response.UnhealthyResources = append(response.UnhealthyResources, UnhealthyResource{
						Kind:      "PersistentVolumeClaim",
						Namespace: pvc.GetNamespace(),
						Name:      pvc.GetName(),
						Status:    status,
						Age:       humanAge(pvc.GetCreationTimestamp().Time),
					})
					mu.Unlock()
				}
			}
			mu.Lock()
			response.Summary.PVCs = ResourceCount{Ready: bound, Total: total}
			mu.Unlock()
		}

		// Failed Jobs (last 24h)
		jobs, err := listResources(ctx, schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, ns)
		if err == nil {
			cutoff := time.Now().Add(-24 * time.Hour)
			var failedCount int
			for i := range jobs {
				job := &jobs[i]
				if getJobStatus(job) == "Failed" && job.GetCreationTimestamp().After(cutoff) {
					failedCount++
				}
			}
			mu.Lock()
			response.Summary.JobsFailed24h = failedCount
			mu.Unlock()
		}
	}()

	// 4. Scan events (Warning only, last 30 minutes)
	go func() {
		defer wg.Done()
		eventNS := ns
		if eventNS == "" {
			eventNS = allNamespaces()
		}

		events, err := clientset.CoreV1().Events(eventNS).List(ctx, metav1.ListOptions{
			FieldSelector: "type=Warning",
		})
		if err != nil {
			return
		}

		cutoff := time.Now().Add(-30 * time.Minute)
		var errorEvents []ErrorEvent
		for _, e := range events.Items {
			eventTime := e.LastTimestamp.Time
			if eventTime.IsZero() {
				eventTime = e.CreationTimestamp.Time
			}
			if eventTime.Before(cutoff) {
				continue
			}

			errorEvents = append(errorEvents, ErrorEvent{
				Type:      e.Type,
				Reason:    e.Reason,
				Object:    e.InvolvedObject.Kind + "/" + e.InvolvedObject.Name,
				Namespace: e.Namespace,
				Message:   truncate(e.Message, 200),
				Age:       humanAge(eventTime),
				Count:     e.Count,
			})
		}

		// Sort by count descending (noisiest first)
		sort.Slice(errorEvents, func(i, j int) bool {
			return errorEvents[i].Count > errorEvents[j].Count
		})

		// Limit to top 20 events
		if len(errorEvents) > 20 {
			errorEvents = errorEvents[:20]
		}

		mu.Lock()
		response.RecentErrorEvents = errorEvents
		mu.Unlock()
	}()

	wg.Wait()

	// Determine overall health
	response.Overall = "HEALTHY"
	if len(response.UnhealthyResources) > 0 || len(response.NodeConditions) > 0 {
		response.Overall = "DEGRADED"
	}
	if response.Summary.Nodes.Ready < response.Summary.Nodes.Total {
		response.Overall = "CRITICAL"
	}
	// More than 20% pods unhealthy = critical
	if response.Summary.Pods.Total > 0 {
		unhealthyPct := float64(response.Summary.Pods.Unhealthy) / float64(response.Summary.Pods.Total)
		if unhealthyPct > 0.2 {
			response.Overall = "CRITICAL"
		}
	}

	return jsonMarshalResult(response), nil, nil
}

// listResources lists all resources of a given type, optionally filtered by namespace.
func listResources(ctx context.Context, gvr schema.GroupVersionResource, namespace string) ([]unstructured.Unstructured, error) {
	var res = dynClient.Resource(gvr)
	var iface = res.Namespace(namespace)

	list, err := iface.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}
	return list.Items, nil
}

// checkNodePressure checks for MemoryPressure, DiskPressure, PIDPressure conditions.
func checkNodePressure(node *unstructured.Unstructured, conditions *[]NodeCondition) {
	status, ok := node.Object["status"].(map[string]interface{})
	if !ok {
		return
	}
	condList, _ := status["conditions"].([]interface{})

	pressureTypes := []string{"MemoryPressure", "DiskPressure", "PIDPressure"}
	for _, c := range condList {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}
		cType, _ := cm["type"].(string)
		cStatus, _ := cm["status"].(string)

		for _, pt := range pressureTypes {
			if cType == pt && cStatus == "True" {
				msg, _ := cm["message"].(string)
				*conditions = append(*conditions, NodeCondition{
					Name:      node.GetName(),
					Status:    "Pressure",
					Condition: cType,
					Message:   truncate(msg, 200),
				})
			}
		}
	}
}

// nestedString extracts a string from a nested map path.
func nestedString(obj map[string]interface{}, fields ...string) (string, bool) {
	current := obj
	for i, f := range fields {
		if i == len(fields)-1 {
			val, ok := current[f].(string)
			return val, ok
		}
		next, ok := current[f].(map[string]interface{})
		if !ok {
			return "", false
		}
		current = next
	}
	return "", false
}

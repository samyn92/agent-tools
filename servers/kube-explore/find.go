/*
find.go — kube_find tool handler.

Fuzzy search across ALL namespaces and ALL resource types in a single call.
Matches against name, labels, annotations, status conditions.
Returns a ranked list with namespace, kind, name, status, age, and relevance score.
*/
package main

import (
	"context"
	"sort"
	"sync"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type findInput struct {
	Query     string `json:"query" jsonschema_description:"Search query — resource name (fuzzy), label value, or status keyword (failing, broken, unhealthy, pending, crash, oom)"`
	Kind      string `json:"kind,omitempty" jsonschema_description:"Filter to a specific resource kind (e.g. Pod, Deployment, Service)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Filter to a specific namespace (omit to search all namespaces)"`
	Status    string `json:"status,omitempty" jsonschema_description:"Filter by status keyword (e.g. failing, running, pending)"`
}

func handleFind(ctx context.Context, _ *mcp.CallToolRequest, input findInput) (*mcp.CallToolResult, any, error) {
	if input.Query == "" && input.Status == "" {
		return mcputil.ErrResult("Either 'query' or 'status' must be provided"), nil, nil
	}

	maxResults := 25

	// Determine which resource types to scan
	var searchGVRs []resourceInfo
	if input.Kind != "" {
		if info, ok := resolveKind(input.Kind); ok {
			searchGVRs = []resourceInfo{info}
		} else {
			return mcputil.ErrResult("Unknown resource kind: %s", input.Kind), nil, nil
		}
	} else {
		searchGVRs = defaultSearchGVRs
	}

	// Scan all resource types in parallel
	var (
		mu      sync.Mutex
		wg      sync.WaitGroup
		matches []FuzzyMatch
	)

	for _, gvrInfo := range searchGVRs {
		wg.Add(1)
		go func(info resourceInfo) {
			defer wg.Done()

			ns := input.Namespace // Empty string = all namespaces
			var iface = dynClient.Resource(info.GVR).Namespace(ns)

			list, err := iface.List(ctx, metav1.ListOptions{})
			if err != nil {
				return // Skip this resource type if we can't list it
			}

			var localMatches []FuzzyMatch
			for i := range list.Items {
				item := &list.Items[i]

				// Apply status filter first (cheap)
				if input.Status != "" && !matchesStatusFilter(item, input.Status) {
					continue
				}

				// Apply fuzzy matching
				if input.Query != "" {
					if m := fuzzyMatchObject(input.Query, item, info.Kind); m != nil {
						localMatches = append(localMatches, *m)
					}
				} else {
					// Status-only search — include all that pass the status filter
					localMatches = append(localMatches, FuzzyMatch{
						Object:      item,
						Kind:        info.Kind,
						Score:       0.8,
						MatchReason: "status matches '" + input.Status + "'",
					})
				}
			}

			if len(localMatches) > 0 {
				mu.Lock()
				matches = append(matches, localMatches...)
				mu.Unlock()
			}
		}(gvrInfo)
	}

	wg.Wait()

	// Sort by relevance score descending
	sort.Slice(matches, func(i, j int) bool {
		return matches[i].Score > matches[j].Score
	})

	// Truncate to max results
	if len(matches) > maxResults {
		matches = matches[:maxResults]
	}

	// Build response
	results := make([]FindResult, 0, len(matches))
	for _, m := range matches {
		obj := m.Object
		result := FindResult{
			Kind:        m.Kind,
			Namespace:   obj.GetNamespace(),
			Name:        obj.GetName(),
			Status:      getResourceStatus(obj),
			Age:         humanAge(obj.GetCreationTimestamp().Time),
			Labels:      filterLabels(obj.GetLabels()),
			Relevance:   m.Score,
			MatchReason: m.MatchReason,
		}

		// Add kind-specific fields
		switch m.Kind {
		case "Deployment", "StatefulSet", "DaemonSet", "ReplicaSet":
			result.Replicas = getReplicaCounts(obj)
		case "Pod":
			if node, ok := nestedString(obj.Object, "spec", "nodeName"); ok {
				result.Node = node
			}
		}

		results = append(results, result)
	}

	query := input.Query
	if query == "" {
		query = "status:" + input.Status
	}

	return jsonMarshalResult(FindResponse{
		Query:        query,
		TotalMatches: len(matches),
		Results:      results,
	}), nil, nil
}

// filterLabels returns labels, excluding noisy system labels to keep the output clean.
func filterLabels(labels map[string]string) map[string]string {
	if len(labels) == 0 {
		return nil
	}

	noisy := map[string]bool{
		"pod-template-hash":                  true,
		"controller-revision-hash":           true,
		"statefulset.kubernetes.io/pod-name": true,
		"batch.kubernetes.io/controller-uid": true,
		"batch.kubernetes.io/job-name":       true,
		"job-name":                           true,
	}

	result := make(map[string]string)
	for k, v := range labels {
		if noisy[k] {
			continue
		}
		result[k] = v
	}
	if len(result) == 0 {
		return nil
	}
	return result
}

/*
topology.go — kube_topology tool handler.

Relationship graph for a workload: Deployment -> ReplicaSet -> Pods,
plus network (Services, Ingresses), storage (PVCs), and config
(ConfigMaps, Secrets referenced by the workload).

Returns a tree structure the agent can reason about.
*/
package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

type topologyInput struct {
	Name      string `json:"name" jsonschema_description:"Resource name (fuzzy matching supported)"`
	Kind      string `json:"kind,omitempty" jsonschema_description:"Resource kind (e.g. Deployment, StatefulSet). If omitted, searches all types."`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace (omit to search all namespaces)"`
}

func handleTopology(ctx context.Context, _ *mcp.CallToolRequest, input topologyInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return mcputil.ErrResult("'name' is required"), nil, nil
	}

	// Find the resource
	obj, kind, err := fuzzyFindOne(ctx, input.Name, input.Kind, input.Namespace)
	if err != nil {
		return mcputil.ErrResult("Resource not found: %v", err), nil, nil
	}

	// Walk up to the root owner (e.g. Pod -> ReplicaSet -> Deployment)
	root, err := getRootOwner(ctx, obj)
	if err != nil {
		root = obj
	}
	rootKind := root.GetKind()
	if rootKind == "" {
		rootKind = kind
	}

	response := TopologyResponse{
		Root: TopologyNode{
			Kind:   rootKind,
			Name:   root.GetName(),
			Status: getResourceStatus(root),
		},
	}

	// Add replicas for workload controllers
	switch rootKind {
	case "Deployment", "StatefulSet", "DaemonSet":
		response.Root.Replicas = getReplicaCounts(root)
	}

	// Build tree: find children
	response.Tree = buildTopologyTree(ctx, root, rootKind)

	// Find related network, storage, and config resources
	related, err := findRelatedResources(ctx, root)
	if err == nil {
		// Network
		for _, svc := range related.Services {
			response.Network = append(response.Network, NetworkResource{
				Kind:     svc.Kind,
				Name:     svc.Name,
				Type:     svc.Type,
				Ports:    svc.Ports,
				Selector: buildSelectorString(root),
			})
		}
		for _, ing := range related.Ingresses {
			response.Network = append(response.Network, NetworkResource{
				Kind: ing.Kind,
				Name: ing.Name,
				Host: ing.Type,
				Path: firstOrEmpty(ing.Ports),
			})
		}

		// Storage
		for _, pvc := range related.PVCs {
			pvcObj, err := dynClient.Resource(schema.GroupVersionResource{
				Version: "v1", Resource: "persistentvolumeclaims",
			}).Namespace(pvc.Namespace).Get(ctx, pvc.Name, metav1.GetOptions{})
			if err == nil {
				response.Storage = append(response.Storage, StorageResource{
					Kind:         "PersistentVolumeClaim",
					Name:         pvc.Name,
					Status:       getResourceStatus(pvcObj),
					Size:         extractPVCSize(pvcObj),
					StorageClass: extractPVCStorageClass(pvcObj),
				})
			} else {
				response.Storage = append(response.Storage, StorageResource{
					Kind: "PersistentVolumeClaim",
					Name: pvc.Name,
				})
			}
		}

		// Config
		for _, cm := range related.ConfigMaps {
			response.Config = append(response.Config, ConfigResource{
				Kind:         cm.Kind,
				Name:         cm.Name,
				ReferencedBy: cm.Type,
			})
		}
		for _, sec := range related.Secrets {
			response.Config = append(response.Config, ConfigResource{
				Kind:         sec.Kind,
				Name:         sec.Name,
				ReferencedBy: sec.Type,
			})
		}
	}

	return jsonMarshalResult(response), nil, nil
}

// buildTopologyTree builds the child tree for a workload controller.
func buildTopologyTree(ctx context.Context, root *unstructured.Unstructured, kind string) []TopologyNode {
	switch kind {
	case "Deployment":
		return buildDeploymentTree(ctx, root)
	case "StatefulSet":
		return buildStatefulSetTree(ctx, root)
	case "DaemonSet":
		return buildDaemonSetTree(ctx, root)
	case "ReplicaSet":
		return buildReplicaSetTree(ctx, root)
	case "Job":
		return buildJobTree(ctx, root)
	case "CronJob":
		return buildCronJobTree(ctx, root)
	default:
		return nil
	}
}

func buildDeploymentTree(ctx context.Context, deploy *unstructured.Unstructured) []TopologyNode {
	rsGVR := schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}
	replicaSets, err := findOwnedResources(ctx, deploy, rsGVR)
	if err != nil {
		return nil
	}

	var nodes []TopologyNode
	for _, rs := range replicaSets {
		rsNode := TopologyNode{
			Kind:     "ReplicaSet",
			Name:     rs.GetName(),
			Status:   getReplicaCounts(rs),
			Children: buildReplicaSetTree(ctx, rs),
		}
		nodes = append(nodes, rsNode)
	}
	return nodes
}

func buildStatefulSetTree(ctx context.Context, sts *unstructured.Unstructured) []TopologyNode {
	return buildPodChildren(ctx, sts)
}

func buildDaemonSetTree(ctx context.Context, ds *unstructured.Unstructured) []TopologyNode {
	return buildPodChildren(ctx, ds)
}

func buildReplicaSetTree(ctx context.Context, rs *unstructured.Unstructured) []TopologyNode {
	return buildPodChildren(ctx, rs)
}

func buildJobTree(ctx context.Context, job *unstructured.Unstructured) []TopologyNode {
	return buildPodChildren(ctx, job)
}

func buildCronJobTree(ctx context.Context, cj *unstructured.Unstructured) []TopologyNode {
	jobGVR := schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}
	jobs, err := findOwnedResources(ctx, cj, jobGVR)
	if err != nil {
		return nil
	}

	var nodes []TopologyNode
	for _, job := range jobs {
		jobNode := TopologyNode{
			Kind:     "Job",
			Name:     job.GetName(),
			Status:   getJobStatus(job),
			Children: buildJobTree(ctx, job),
		}
		nodes = append(nodes, jobNode)
	}
	return nodes
}

func buildPodChildren(ctx context.Context, owner *unstructured.Unstructured) []TopologyNode {
	podGVR := schema.GroupVersionResource{Version: "v1", Resource: "pods"}
	pods, err := findOwnedResources(ctx, owner, podGVR)
	if err != nil {
		return nil
	}

	var nodes []TopologyNode
	for _, pod := range pods {
		nodeName, _ := nestedString(pod.Object, "spec", "nodeName")
		nodes = append(nodes, TopologyNode{
			Kind:   "Pod",
			Name:   pod.GetName(),
			Status: getPodStatus(pod),
			Node:   nodeName,
		})
	}
	return nodes
}

func buildSelectorString(obj *unstructured.Unstructured) string {
	labels := getPodLabels(obj)
	if len(labels) == 0 {
		return ""
	}
	priorities := []string{"app", "app.kubernetes.io/name", "name"}
	for _, key := range priorities {
		if val, ok := labels[key]; ok {
			return fmt.Sprintf("%s=%s", key, val)
		}
	}
	for k, v := range labels {
		return fmt.Sprintf("%s=%s", k, v)
	}
	return ""
}

func extractPVCSize(pvc *unstructured.Unstructured) string {
	spec, _ := pvc.Object["spec"].(map[string]interface{})
	if spec == nil {
		return ""
	}
	resources, _ := spec["resources"].(map[string]interface{})
	if resources == nil {
		return ""
	}
	requests, _ := resources["requests"].(map[string]interface{})
	if requests == nil {
		return ""
	}
	storage, _ := requests["storage"].(string)
	return storage
}

func extractPVCStorageClass(pvc *unstructured.Unstructured) string {
	spec, _ := pvc.Object["spec"].(map[string]interface{})
	if spec == nil {
		return ""
	}
	sc, _ := spec["storageClassName"].(string)
	return sc
}

func firstOrEmpty(s []string) string {
	if len(s) > 0 {
		return s[0]
	}
	return ""
}

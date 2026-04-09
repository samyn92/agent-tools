/*
resolve.go — Resource type resolution.

Resolves user-provided resource type strings (like "pods", "deploy", "agents")
to their GroupVersionResource + namespaced flag. Ported from the existing
kubernetes tool with the same shortcut map + server discovery fallback.
*/
package main

import (
	"fmt"
	"strings"

	"k8s.io/apimachinery/pkg/runtime/schema"
)

// resourceInfo holds a resolved resource type with its GVR and namespace scope.
type resourceInfo struct {
	GVR        schema.GroupVersionResource
	Namespaced bool
	Kind       string // Human-readable kind (e.g. "Pod", "Deployment")
}

// defaultSearchGVRs is the curated list of resource types scanned by kube_find
// when no specific kind filter is provided. Ordered by priority/usefulness.
var defaultSearchGVRs = []resourceInfo{
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true, Kind: "Pod"},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true, Kind: "Deployment"},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, Namespaced: true, Kind: "StatefulSet"},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, Namespaced: true, Kind: "DaemonSet"},
	{GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true, Kind: "ReplicaSet"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Namespaced: true, Kind: "Service"},
	{GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true, Kind: "Ingress"},
	{GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, Namespaced: true, Kind: "Job"},
	{GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true, Kind: "CronJob"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespaced: true, Kind: "ConfigMap"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Namespaced: true, Kind: "Secret"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, Namespaced: true, Kind: "PersistentVolumeClaim"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, Namespaced: true, Kind: "ServiceAccount"},
	{GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Namespaced: false, Kind: "Node"},
	// AgentOps CRDs
	{GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agents"}, Namespaced: true, Kind: "Agent"},
	{GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agentruns"}, Namespaced: true, Kind: "AgentRun"},
	{GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agenttools"}, Namespaced: true, Kind: "AgentTool"},
	{GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "channels"}, Namespaced: true, Kind: "Channel"},
	{GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "mcpservers"}, Namespaced: true, Kind: "MCPServer"},
}

// shortcutMap maps common resource aliases to their canonical GVR.
var shortcutMap = map[string]resourceInfo{
	"po":   {GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true, Kind: "Pod"},
	"pods": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true, Kind: "Pod"},
	"pod":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "pods"}, Namespaced: true, Kind: "Pod"},

	"svc":      {GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Namespaced: true, Kind: "Service"},
	"services": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Namespaced: true, Kind: "Service"},
	"service":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "services"}, Namespaced: true, Kind: "Service"},

	"deploy":      {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true, Kind: "Deployment"},
	"deployments": {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true, Kind: "Deployment"},
	"deployment":  {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "deployments"}, Namespaced: true, Kind: "Deployment"},

	"ds":         {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, Namespaced: true, Kind: "DaemonSet"},
	"daemonsets": {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, Namespaced: true, Kind: "DaemonSet"},
	"daemonset":  {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "daemonsets"}, Namespaced: true, Kind: "DaemonSet"},

	"sts":          {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, Namespaced: true, Kind: "StatefulSet"},
	"statefulsets": {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, Namespaced: true, Kind: "StatefulSet"},
	"statefulset":  {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "statefulsets"}, Namespaced: true, Kind: "StatefulSet"},

	"rs":          {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true, Kind: "ReplicaSet"},
	"replicasets": {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true, Kind: "ReplicaSet"},
	"replicaset":  {GVR: schema.GroupVersionResource{Group: "apps", Version: "v1", Resource: "replicasets"}, Namespaced: true, Kind: "ReplicaSet"},

	"cm":         {GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespaced: true, Kind: "ConfigMap"},
	"configmaps": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespaced: true, Kind: "ConfigMap"},
	"configmap":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "configmaps"}, Namespaced: true, Kind: "ConfigMap"},

	"secret":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Namespaced: true, Kind: "Secret"},
	"secrets": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "secrets"}, Namespaced: true, Kind: "Secret"},

	"ns":         {GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Namespaced: false, Kind: "Namespace"},
	"namespaces": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Namespaced: false, Kind: "Namespace"},
	"namespace":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "namespaces"}, Namespaced: false, Kind: "Namespace"},

	"no":    {GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Namespaced: false, Kind: "Node"},
	"nodes": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Namespaced: false, Kind: "Node"},
	"node":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "nodes"}, Namespaced: false, Kind: "Node"},

	"pvc":                    {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, Namespaced: true, Kind: "PersistentVolumeClaim"},
	"persistentvolumeclaims": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, Namespaced: true, Kind: "PersistentVolumeClaim"},
	"persistentvolumeclaim":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumeclaims"}, Namespaced: true, Kind: "PersistentVolumeClaim"},

	"pv":                {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, Namespaced: false, Kind: "PersistentVolume"},
	"persistentvolumes": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, Namespaced: false, Kind: "PersistentVolume"},
	"persistentvolume":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "persistentvolumes"}, Namespaced: false, Kind: "PersistentVolume"},

	"ing":       {GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true, Kind: "Ingress"},
	"ingresses": {GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true, Kind: "Ingress"},
	"ingress":   {GVR: schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}, Namespaced: true, Kind: "Ingress"},

	"job":  {GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, Namespaced: true, Kind: "Job"},
	"jobs": {GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "jobs"}, Namespaced: true, Kind: "Job"},

	"cj":       {GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true, Kind: "CronJob"},
	"cronjobs": {GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true, Kind: "CronJob"},
	"cronjob":  {GVR: schema.GroupVersionResource{Group: "batch", Version: "v1", Resource: "cronjobs"}, Namespaced: true, Kind: "CronJob"},

	"sa":              {GVR: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, Namespaced: true, Kind: "ServiceAccount"},
	"serviceaccounts": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, Namespaced: true, Kind: "ServiceAccount"},
	"serviceaccount":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "serviceaccounts"}, Namespaced: true, Kind: "ServiceAccount"},

	"events": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "events"}, Namespaced: true, Kind: "Event"},
	"ev":     {GVR: schema.GroupVersionResource{Version: "v1", Resource: "events"}, Namespaced: true, Kind: "Event"},
	"event":  {GVR: schema.GroupVersionResource{Version: "v1", Resource: "events"}, Namespaced: true, Kind: "Event"},

	"ep":        {GVR: schema.GroupVersionResource{Version: "v1", Resource: "endpoints"}, Namespaced: true, Kind: "Endpoints"},
	"endpoints": {GVR: schema.GroupVersionResource{Version: "v1", Resource: "endpoints"}, Namespaced: true, Kind: "Endpoints"},

	// AgentOps CRDs
	"agent":      {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agents"}, Namespaced: true, Kind: "Agent"},
	"agents":     {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agents"}, Namespaced: true, Kind: "Agent"},
	"agentrun":   {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agentruns"}, Namespaced: true, Kind: "AgentRun"},
	"agentruns":  {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agentruns"}, Namespaced: true, Kind: "AgentRun"},
	"agenttool":  {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "agenttools"}, Namespaced: true, Kind: "AgentTool"},
	"channel":    {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "channels"}, Namespaced: true, Kind: "Channel"},
	"channels":   {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "channels"}, Namespaced: true, Kind: "Channel"},
	"mcpserver":  {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "mcpservers"}, Namespaced: true, Kind: "MCPServer"},
	"mcpservers": {GVR: schema.GroupVersionResource{Group: "agents.agentops.io", Version: "v1alpha1", Resource: "mcpservers"}, Namespaced: true, Kind: "MCPServer"},
}

// resolveResource resolves a user-provided resource type string to a GVR.
func resolveResource(resource string) (resourceInfo, error) {
	lower := strings.ToLower(strings.TrimSpace(resource))
	if info, ok := shortcutMap[lower]; ok {
		return info, nil
	}

	// Try server discovery for unknown resource types
	resources, err := clientset.Discovery().ServerPreferredResources()
	if err != nil {
		return resourceInfo{}, fmt.Errorf("discovery failed: %v", err)
	}

	for _, resList := range resources {
		gv, _ := schema.ParseGroupVersion(resList.GroupVersion)
		for _, r := range resList.APIResources {
			if strings.EqualFold(r.Name, lower) || strings.EqualFold(r.Kind, resource) {
				return resourceInfo{
					GVR: schema.GroupVersionResource{
						Group:    gv.Group,
						Version:  gv.Version,
						Resource: r.Name,
					},
					Namespaced: r.Namespaced,
					Kind:       r.Kind,
				}, nil
			}
			for _, sn := range r.ShortNames {
				if strings.EqualFold(sn, lower) {
					return resourceInfo{
						GVR: schema.GroupVersionResource{
							Group:    gv.Group,
							Version:  gv.Version,
							Resource: r.Name,
						},
						Namespaced: r.Namespaced,
						Kind:       r.Kind,
					}, nil
				}
			}
		}
	}

	return resourceInfo{}, fmt.Errorf("unknown resource type: %s", resource)
}

// resolveKind returns the resourceInfo for a kind string ("Pod", "Deployment", etc.)
func resolveKind(kind string) (resourceInfo, bool) {
	lower := strings.ToLower(strings.TrimSpace(kind))
	// Check shortcut map first
	if info, ok := shortcutMap[lower]; ok {
		return info, true
	}
	// Check defaultSearchGVRs by kind name
	for _, info := range defaultSearchGVRs {
		if strings.EqualFold(info.Kind, kind) {
			return info, true
		}
	}
	return resourceInfo{}, false
}

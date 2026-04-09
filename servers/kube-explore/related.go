/*
related.go — Related resource discovery.

Given a workload (typically a Deployment, StatefulSet, or Pod), discovers
all related resources: Services, Ingresses, PVCs, ConfigMaps, and Secrets.

Uses label selector matching, volume references, and env var references
to build the relationship graph.
*/
package main

import (
	"context"
	"fmt"
	"strings"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
)

// RelatedResourceSet holds all discovered related resources for a workload.
type RelatedResourceSet struct {
	Services   []RelatedResource
	Ingresses  []RelatedResource
	PVCs       []RelatedResource
	ConfigMaps []RelatedResource
	Secrets    []RelatedResource
}

// Flatten returns all related resources as a single slice.
func (r *RelatedResourceSet) Flatten() []RelatedResource {
	var all []RelatedResource
	all = append(all, r.Services...)
	all = append(all, r.Ingresses...)
	all = append(all, r.PVCs...)
	all = append(all, r.ConfigMaps...)
	all = append(all, r.Secrets...)
	return all
}

// findRelatedResources discovers all resources related to the given object.
// It extracts pod spec from Deployments/StatefulSets/DaemonSets and uses
// it to find Services (by label matching), Ingresses (by service backend),
// and config references (from volumes and env vars).
func findRelatedResources(ctx context.Context, obj *unstructured.Unstructured) (*RelatedResourceSet, error) {
	namespace := obj.GetNamespace()
	if namespace == "" {
		return &RelatedResourceSet{}, nil
	}

	// Get pod labels — either from the object itself (if Pod) or from
	// spec.template.metadata.labels (if Deployment/StatefulSet/etc.)
	podLabels := getPodLabels(obj)
	podSpec := getPodSpec(obj)

	result := &RelatedResourceSet{}

	// Find matching Services
	if len(podLabels) > 0 {
		svcs, err := findMatchingServices(ctx, namespace, podLabels)
		if err == nil {
			result.Services = svcs
		}
	}

	// Find Ingresses that reference discovered Services
	if len(result.Services) > 0 {
		ings, err := findMatchingIngresses(ctx, namespace, result.Services)
		if err == nil {
			result.Ingresses = ings
		}
	}

	// Extract PVCs, ConfigMaps, and Secrets from pod spec
	if podSpec != nil {
		result.PVCs = extractPVCReferences(namespace, podSpec)
		result.ConfigMaps = extractConfigMapReferences(namespace, podSpec)
		result.Secrets = extractSecretReferences(namespace, podSpec)
	}

	return result, nil
}

// getPodLabels extracts the labels that would be on pods created by this resource.
func getPodLabels(obj *unstructured.Unstructured) map[string]string {
	kind := obj.GetKind()

	if kind == "Pod" {
		return obj.GetLabels()
	}

	// For workload controllers, get spec.template.metadata.labels
	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	template, ok := spec["template"].(map[string]interface{})
	if !ok {
		return nil
	}
	metadata, ok := template["metadata"].(map[string]interface{})
	if !ok {
		return nil
	}
	labels, ok := metadata["labels"].(map[string]interface{})
	if !ok {
		return nil
	}

	result := make(map[string]string, len(labels))
	for k, v := range labels {
		if s, ok := v.(string); ok {
			result[k] = s
		}
	}
	return result
}

// getPodSpec extracts the pod spec from any resource that contains one.
func getPodSpec(obj *unstructured.Unstructured) map[string]interface{} {
	kind := obj.GetKind()

	if kind == "Pod" {
		spec, _ := obj.Object["spec"].(map[string]interface{})
		return spec
	}

	// For workload controllers: spec.template.spec
	spec, ok := obj.Object["spec"].(map[string]interface{})
	if !ok {
		return nil
	}
	template, ok := spec["template"].(map[string]interface{})
	if !ok {
		return nil
	}
	podSpec, _ := template["spec"].(map[string]interface{})
	return podSpec
}

// findMatchingServices finds Services whose selector matches the given pod labels.
func findMatchingServices(ctx context.Context, namespace string, podLabels map[string]string) ([]RelatedResource, error) {
	svcGVR := schema.GroupVersionResource{Version: "v1", Resource: "services"}
	list, err := dynClient.Resource(svcGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var results []RelatedResource
	for _, svc := range list.Items {
		spec, ok := svc.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}

		selector, ok := spec["selector"].(map[string]interface{})
		if !ok || len(selector) == 0 {
			continue
		}

		// Check if all selector labels match the pod labels
		matches := true
		for k, v := range selector {
			selectorVal, ok := v.(string)
			if !ok {
				matches = false
				break
			}
			if podLabels[k] != selectorVal {
				matches = false
				break
			}
		}

		if matches {
			svcType, _ := spec["type"].(string)
			if svcType == "" {
				svcType = "ClusterIP"
			}

			ports := extractServicePorts(spec)

			results = append(results, RelatedResource{
				Kind:      "Service",
				Name:      svc.GetName(),
				Namespace: namespace,
				Type:      svcType,
				Ports:     ports,
			})
		}
	}

	return results, nil
}

// extractServicePorts returns port strings like "8080/TCP".
func extractServicePorts(spec map[string]interface{}) []string {
	ports, ok := spec["ports"].([]interface{})
	if !ok {
		return nil
	}

	var result []string
	for _, p := range ports {
		pm, ok := p.(map[string]interface{})
		if !ok {
			continue
		}
		port := fmt.Sprintf("%v", pm["port"])
		protocol, _ := pm["protocol"].(string)
		if protocol == "" {
			protocol = "TCP"
		}
		result = append(result, port+"/"+protocol)
	}
	return result
}

// findMatchingIngresses finds Ingresses that reference any of the given services.
func findMatchingIngresses(ctx context.Context, namespace string, services []RelatedResource) ([]RelatedResource, error) {
	ingGVR := schema.GroupVersionResource{Group: "networking.k8s.io", Version: "v1", Resource: "ingresses"}
	list, err := dynClient.Resource(ingGVR).Namespace(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	svcNames := make(map[string]bool, len(services))
	for _, svc := range services {
		svcNames[svc.Name] = true
	}

	var results []RelatedResource
	for _, ing := range list.Items {
		spec, ok := ing.Object["spec"].(map[string]interface{})
		if !ok {
			continue
		}

		// Check rules for service backend references
		rules, _ := spec["rules"].([]interface{})
		for _, rule := range rules {
			rm, ok := rule.(map[string]interface{})
			if !ok {
				continue
			}

			host, _ := rm["host"].(string)
			http, ok := rm["http"].(map[string]interface{})
			if !ok {
				continue
			}

			paths, _ := http["paths"].([]interface{})
			for _, path := range paths {
				pm, ok := path.(map[string]interface{})
				if !ok {
					continue
				}

				// Extract service name from backend
				svcName := extractIngressBackendService(pm)
				if svcNames[svcName] {
					pathStr, _ := pm["path"].(string)
					hasTLS := ingressHasTLS(spec, host)

					results = append(results, RelatedResource{
						Kind:      "Ingress",
						Name:      ing.GetName(),
						Namespace: namespace,
						Type:      host,
						Ports:     []string{pathStr},
					})
					// Use Type field for host and Ports for path (overloading for the response)
					_ = hasTLS
					break
				}
			}
		}
	}

	return results, nil
}

// extractIngressBackendService extracts the service name from an Ingress path backend.
func extractIngressBackendService(pathSpec map[string]interface{}) string {
	backend, ok := pathSpec["backend"].(map[string]interface{})
	if !ok {
		return ""
	}
	service, ok := backend["service"].(map[string]interface{})
	if !ok {
		return ""
	}
	name, _ := service["name"].(string)
	return name
}

// ingressHasTLS returns true if the ingress has TLS config for the given host.
func ingressHasTLS(spec map[string]interface{}, host string) bool {
	tls, ok := spec["tls"].([]interface{})
	if !ok {
		return false
	}
	for _, t := range tls {
		tm, ok := t.(map[string]interface{})
		if !ok {
			continue
		}
		hosts, _ := tm["hosts"].([]interface{})
		for _, h := range hosts {
			if hs, ok := h.(string); ok && hs == host {
				return true
			}
		}
	}
	return false
}

// extractPVCReferences extracts PVC names from the pod spec's volumes.
func extractPVCReferences(namespace string, podSpec map[string]interface{}) []RelatedResource {
	volumes, ok := podSpec["volumes"].([]interface{})
	if !ok {
		return nil
	}

	var results []RelatedResource
	seen := make(map[string]bool)

	for _, v := range volumes {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		pvc, ok := vm["persistentVolumeClaim"].(map[string]interface{})
		if !ok {
			continue
		}
		claimName, _ := pvc["claimName"].(string)
		if claimName != "" && !seen[claimName] {
			seen[claimName] = true
			results = append(results, RelatedResource{
				Kind:      "PersistentVolumeClaim",
				Name:      claimName,
				Namespace: namespace,
			})
		}
	}

	return results
}

// extractConfigMapReferences extracts ConfigMap references from volumes and env vars.
func extractConfigMapReferences(namespace string, podSpec map[string]interface{}) []RelatedResource {
	seen := make(map[string]bool)
	var results []RelatedResource

	addRef := func(name, source string) {
		if name != "" && !seen[name] {
			seen[name] = true
			results = append(results, RelatedResource{
				Kind:      "ConfigMap",
				Name:      name,
				Namespace: namespace,
				Type:      source,
			})
		}
	}

	// From volumes
	volumes, _ := podSpec["volumes"].([]interface{})
	for _, v := range volumes {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if cm, ok := vm["configMap"].(map[string]interface{}); ok {
			name, _ := cm["name"].(string)
			addRef(name, "volume")
		}
		// projected volumes
		if projected, ok := vm["projected"].(map[string]interface{}); ok {
			sources, _ := projected["sources"].([]interface{})
			for _, src := range sources {
				srcMap, ok := src.(map[string]interface{})
				if !ok {
					continue
				}
				if cm, ok := srcMap["configMap"].(map[string]interface{}); ok {
					name, _ := cm["name"].(string)
					addRef(name, "projected volume")
				}
			}
		}
	}

	// From containers
	containers, _ := podSpec["containers"].([]interface{})
	containers = append(containers, getSlice(podSpec, "initContainers")...)

	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// envFrom[].configMapRef
		envFrom, _ := cm["envFrom"].([]interface{})
		for _, ef := range envFrom {
			efm, ok := ef.(map[string]interface{})
			if !ok {
				continue
			}
			if cmRef, ok := efm["configMapRef"].(map[string]interface{}); ok {
				name, _ := cmRef["name"].(string)
				addRef(name, "envFrom")
			}
		}

		// env[].valueFrom.configMapKeyRef
		env, _ := cm["env"].([]interface{})
		for _, e := range env {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if vf, ok := em["valueFrom"].(map[string]interface{}); ok {
				if cmRef, ok := vf["configMapKeyRef"].(map[string]interface{}); ok {
					name, _ := cmRef["name"].(string)
					addRef(name, "env")
				}
			}
		}
	}

	return results
}

// extractSecretReferences extracts Secret references from volumes and env vars.
func extractSecretReferences(namespace string, podSpec map[string]interface{}) []RelatedResource {
	seen := make(map[string]bool)
	var results []RelatedResource

	addRef := func(name, source string) {
		if name != "" && !seen[name] {
			seen[name] = true
			results = append(results, RelatedResource{
				Kind:      "Secret",
				Name:      name,
				Namespace: namespace,
				Type:      source,
			})
		}
	}

	// From volumes
	volumes, _ := podSpec["volumes"].([]interface{})
	for _, v := range volumes {
		vm, ok := v.(map[string]interface{})
		if !ok {
			continue
		}
		if sec, ok := vm["secret"].(map[string]interface{}); ok {
			name, _ := sec["secretName"].(string)
			addRef(name, "volume")
		}
		// projected volumes
		if projected, ok := vm["projected"].(map[string]interface{}); ok {
			sources, _ := projected["sources"].([]interface{})
			for _, src := range sources {
				srcMap, ok := src.(map[string]interface{})
				if !ok {
					continue
				}
				if sec, ok := srcMap["secret"].(map[string]interface{}); ok {
					name, _ := sec["name"].(string)
					addRef(name, "projected volume")
				}
			}
		}
	}

	// From containers
	containers, _ := podSpec["containers"].([]interface{})
	containers = append(containers, getSlice(podSpec, "initContainers")...)

	for _, c := range containers {
		cm, ok := c.(map[string]interface{})
		if !ok {
			continue
		}

		// envFrom[].secretRef
		envFrom, _ := cm["envFrom"].([]interface{})
		for _, ef := range envFrom {
			efm, ok := ef.(map[string]interface{})
			if !ok {
				continue
			}
			if secRef, ok := efm["secretRef"].(map[string]interface{}); ok {
				name, _ := secRef["name"].(string)
				addRef(name, "envFrom")
			}
		}

		// env[].valueFrom.secretKeyRef
		env, _ := cm["env"].([]interface{})
		for _, e := range env {
			em, ok := e.(map[string]interface{})
			if !ok {
				continue
			}
			if vf, ok := em["valueFrom"].(map[string]interface{}); ok {
				if secRef, ok := vf["secretKeyRef"].(map[string]interface{}); ok {
					name, _ := secRef["name"].(string)
					addRef(name, "env")
				}
			}
		}
	}

	return results
}

// getSlice is a helper to extract a []interface{} from a map.
func getSlice(m map[string]interface{}, key string) []interface{} {
	v, ok := m[key].([]interface{})
	if !ok {
		return nil
	}
	return v
}

// selectorToString converts a label selector map to "key=value,key=value" string.
func selectorToString(selector map[string]interface{}) string {
	parts := make([]string, 0, len(selector))
	for k, v := range selector {
		parts = append(parts, fmt.Sprintf("%s=%v", k, v))
	}
	return strings.Join(parts, ",")
}

/*
owners.go — Owner reference chain traversal.

Walks metadata.ownerReferences upward to build the full ownership chain
for any Kubernetes resource (e.g. Pod -> ReplicaSet -> Deployment).

Maximum depth of 5 to prevent infinite loops.
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

const maxOwnerDepth = 5

// getOwnerChain walks ownerReferences upward, returning the chain from
// immediate parent to the top-level owner.
func getOwnerChain(ctx context.Context, obj *unstructured.Unstructured) ([]OwnerRef, error) {
	var chain []OwnerRef
	current := obj

	for i := 0; i < maxOwnerDepth; i++ {
		owners := current.GetOwnerReferences()
		if len(owners) == 0 {
			break
		}

		// Follow the controller owner (or first owner if none is controller)
		var owner metav1.OwnerReference
		found := false
		for _, o := range owners {
			if o.Controller != nil && *o.Controller {
				owner = o
				found = true
				break
			}
		}
		if !found {
			owner = owners[0]
		}

		// Resolve the owner resource
		ownerObj, err := getOwnerObject(ctx, owner, current.GetNamespace())
		if err != nil {
			// Can't resolve this owner, add what we know and stop
			chain = append(chain, OwnerRef{
				Kind:      owner.Kind,
				Name:      owner.Name,
				Namespace: current.GetNamespace(),
			})
			break
		}

		ref := OwnerRef{
			Kind:      owner.Kind,
			Name:      owner.Name,
			Namespace: ownerObj.GetNamespace(),
		}

		// Add replica info for workload controllers
		switch owner.Kind {
		case "Deployment", "StatefulSet", "ReplicaSet", "DaemonSet":
			ref.Replicas = getReplicaCounts(ownerObj)
		}

		chain = append(chain, ref)
		current = ownerObj
	}

	return chain, nil
}

// getOwnerObject fetches the owner resource from the cluster.
func getOwnerObject(ctx context.Context, owner metav1.OwnerReference, namespace string) (*unstructured.Unstructured, error) {
	// Resolve the GVR from the owner's APIVersion and Kind
	gv, err := schema.ParseGroupVersion(owner.APIVersion)
	if err != nil {
		return nil, fmt.Errorf("parsing group version %q: %w", owner.APIVersion, err)
	}

	// Use the REST mapper to find the resource name
	mapping, err := mapper.RESTMapping(schema.GroupKind{
		Group: gv.Group,
		Kind:  owner.Kind,
	}, gv.Version)
	if err != nil {
		return nil, fmt.Errorf("mapping %s/%s: %w", gv.Group, owner.Kind, err)
	}

	var res = dynClient.Resource(mapping.Resource)
	if mapping.Scope.Name() == "namespace" {
		return res.Namespace(namespace).Get(ctx, owner.Name, metav1.GetOptions{})
	}
	return res.Get(ctx, owner.Name, metav1.GetOptions{})
}

// getRootOwner walks up to the top-level owner and returns it.
// If the object has no owners, it returns itself.
func getRootOwner(ctx context.Context, obj *unstructured.Unstructured) (*unstructured.Unstructured, error) {
	current := obj
	for i := 0; i < maxOwnerDepth; i++ {
		owners := current.GetOwnerReferences()
		if len(owners) == 0 {
			return current, nil
		}

		var owner metav1.OwnerReference
		found := false
		for _, o := range owners {
			if o.Controller != nil && *o.Controller {
				owner = o
				found = true
				break
			}
		}
		if !found {
			owner = owners[0]
		}

		ownerObj, err := getOwnerObject(ctx, owner, current.GetNamespace())
		if err != nil {
			return current, nil // Return last resolvable object
		}
		current = ownerObj
	}
	return current, nil
}

// getOwnerString returns a short "Kind/Name" string for the first controller owner.
func getOwnerString(obj *unstructured.Unstructured) string {
	owners := obj.GetOwnerReferences()
	for _, o := range owners {
		if o.Controller != nil && *o.Controller {
			return o.Kind + "/" + o.Name
		}
	}
	if len(owners) > 0 {
		return owners[0].Kind + "/" + owners[0].Name
	}
	return ""
}

// findOwnedResources finds all resources of a given type owned by the given object.
func findOwnedResources(ctx context.Context, owner *unstructured.Unstructured, childGVR schema.GroupVersionResource) ([]*unstructured.Unstructured, error) {
	namespace := owner.GetNamespace()
	ownerUID := string(owner.GetUID())

	var res = dynClient.Resource(childGVR)
	var iface = res.Namespace(namespace)

	list, err := iface.List(ctx, metav1.ListOptions{})
	if err != nil {
		return nil, err
	}

	var owned []*unstructured.Unstructured
	for i := range list.Items {
		item := &list.Items[i]
		for _, ref := range item.GetOwnerReferences() {
			if string(ref.UID) == ownerUID {
				owned = append(owned, item)
				break
			}
		}
	}

	return owned, nil
}

// ownerChainString returns a human-readable chain like "Deployment/worker -> ReplicaSet/worker-abc -> Pod/worker-abc-xyz"
func ownerChainString(chain []OwnerRef, leafKind, leafName string) string {
	parts := make([]string, 0, len(chain)+1)
	// Chain is parent-first, reverse it for display
	for i := len(chain) - 1; i >= 0; i-- {
		parts = append(parts, chain[i].Kind+"/"+chain[i].Name)
	}
	parts = append(parts, leafKind+"/"+leafName)
	return strings.Join(parts, " -> ")
}

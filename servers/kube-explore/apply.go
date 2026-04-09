/*
apply.go — kube_apply tool handler.

Server-side apply for YAML/JSON manifests. Ported from the existing
kubernetes tool with minimal changes.
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/types"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
	"k8s.io/client-go/dynamic"
)

type applyInput struct {
	YAML      string `json:"yaml" jsonschema_description:"YAML or JSON manifest content to apply (multi-document supported)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace override (uses manifest namespace if not set)"`
}

func handleApply(ctx context.Context, _ *mcp.CallToolRequest, input applyInput) (*mcp.CallToolResult, any, error) {
	if input.YAML == "" {
		return errResult("'yaml' is required"), nil, nil
	}

	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(input.YAML), 4096)
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

		data, err := json.Marshal(obj)
		if err != nil {
			return errResult("Error marshaling: %v", err), nil, nil
		}

		result, err := res.Patch(ctx, obj.GetName(), types.ApplyPatchType, data, metav1.PatchOptions{
			FieldManager: "kube-explore-mcp-tool",
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

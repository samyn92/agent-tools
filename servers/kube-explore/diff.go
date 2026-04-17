/*
diff.go — kube_diff tool handler.

Compare desired vs live state. For Flux/GitOps resources: show what has drifted.
Accepts inline YAML or a manifest path as the desired source.
Returns a structured diff showing field-level changes.
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"reflect"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	yamlutil "k8s.io/apimachinery/pkg/util/yaml"
)

type diffInput struct {
	Name      string `json:"name" jsonschema_description:"Resource name"`
	Kind      string `json:"kind" jsonschema_description:"Resource kind (e.g. Deployment, Service)"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
	Source    string `json:"source,omitempty" jsonschema_description:"Desired state as inline YAML/JSON manifest. If omitted, shows the live state summary."`
}

func handleDiff(ctx context.Context, _ *mcp.CallToolRequest, input diffInput) (*mcp.CallToolResult, any, error) {
	if input.Name == "" {
		return mcputil.ErrResult("'name' is required"), nil, nil
	}
	if input.Kind == "" {
		return mcputil.ErrResult("'kind' is required"), nil, nil
	}

	// Get the live resource
	liveObj, _, err := fuzzyFindOne(ctx, input.Name, input.Kind, input.Namespace)
	if err != nil {
		return mcputil.ErrResult("Resource not found: %v", err), nil, nil
	}

	response := DiffResponse{
		Resource:  input.Kind,
		Namespace: liveObj.GetNamespace(),
		Name:      liveObj.GetName(),
	}

	if input.Source == "" {
		// No source provided — just return a summary of the live state
		response.Drifted = false
		response.Source = "(no source provided — showing live state)"

		// Summarize the live spec
		if spec, ok := liveObj.Object["spec"]; ok {
			specJSON, _ := json.MarshalIndent(spec, "", "  ")
			response.Changes = []DiffItem{
				{Path: "spec", Actual: string(specJSON), Type: "info"},
			}
		}
		return jsonMarshalResult(response), nil, nil
	}

	// Parse the desired state from the source YAML
	response.Source = "inline manifest"
	decoder := yamlutil.NewYAMLOrJSONDecoder(strings.NewReader(input.Source), 4096)
	var rawDesired map[string]interface{}
	if err := decoder.Decode(&rawDesired); err != nil {
		return mcputil.ErrResult("Error parsing source manifest: %v", err), nil, nil
	}
	desiredObj := &unstructured.Unstructured{Object: rawDesired}

	// Compare spec sections
	liveSpec, _ := liveObj.Object["spec"].(map[string]interface{})
	desiredSpec, _ := desiredObj.Object["spec"].(map[string]interface{})

	if liveSpec != nil && desiredSpec != nil {
		changes := diffMaps("spec", desiredSpec, liveSpec)
		response.Changes = changes
		response.Drifted = len(changes) > 0
	}

	// Compare labels
	if !reflect.DeepEqual(desiredObj.GetLabels(), liveObj.GetLabels()) {
		desiredLabels, _ := json.Marshal(desiredObj.GetLabels())
		liveLabels, _ := json.Marshal(liveObj.GetLabels())
		response.Changes = append(response.Changes, DiffItem{
			Path:     "metadata.labels",
			Expected: string(desiredLabels),
			Actual:   string(liveLabels),
			Type:     "changed",
		})
		response.Drifted = true
	}

	// Compare annotations (excluding system annotations)
	desiredAnns := filterAnnotations(desiredObj.GetAnnotations())
	liveAnns := filterAnnotations(liveObj.GetAnnotations())
	if !reflect.DeepEqual(desiredAnns, liveAnns) {
		desiredAnnsJSON, _ := json.Marshal(desiredAnns)
		liveAnnsJSON, _ := json.Marshal(liveAnns)
		response.Changes = append(response.Changes, DiffItem{
			Path:     "metadata.annotations",
			Expected: string(desiredAnnsJSON),
			Actual:   string(liveAnnsJSON),
			Type:     "changed",
		})
		response.Drifted = true
	}

	return jsonMarshalResult(response), nil, nil
}

// diffMaps compares two maps and returns a list of differences.
func diffMaps(prefix string, desired, live map[string]interface{}) []DiffItem {
	var diffs []DiffItem

	// Check for fields in desired that differ in live
	for key, desiredVal := range desired {
		path := prefix + "." + key
		liveVal, exists := live[key]

		if !exists {
			diffs = append(diffs, DiffItem{
				Path:     path,
				Expected: formatValue(desiredVal),
				Type:     "added",
			})
			continue
		}

		// Both exist — compare recursively if maps, or directly
		desiredMap, desiredIsMap := desiredVal.(map[string]interface{})
		liveMap, liveIsMap := liveVal.(map[string]interface{})

		if desiredIsMap && liveIsMap {
			diffs = append(diffs, diffMaps(path, desiredMap, liveMap)...)
		} else if !reflect.DeepEqual(desiredVal, liveVal) {
			diffs = append(diffs, DiffItem{
				Path:     path,
				Expected: formatValue(desiredVal),
				Actual:   formatValue(liveVal),
				Type:     "changed",
			})
		}
	}

	// Check for fields in live that are not in desired (drift = extra fields)
	for key, liveVal := range live {
		if _, exists := desired[key]; !exists {
			path := prefix + "." + key
			diffs = append(diffs, DiffItem{
				Path:   path,
				Actual: formatValue(liveVal),
				Type:   "removed",
			})
		}
	}

	return diffs
}

// formatValue converts a value to a JSON string for display.
func formatValue(v interface{}) string {
	switch val := v.(type) {
	case string:
		return val
	case float64:
		if val == float64(int64(val)) {
			return fmt.Sprintf("%d", int64(val))
		}
		return fmt.Sprintf("%g", val)
	case bool:
		return fmt.Sprintf("%t", val)
	default:
		data, _ := json.Marshal(v)
		return string(data)
	}
}

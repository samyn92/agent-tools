package main

import (
	"context"
	"fmt"
	"sort"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
	"gopkg.in/yaml.v3"
)

// ── Input types ──

type showValuesInput struct {
	Chart   string `json:"chart" jsonschema_description:"Chart reference (e.g. oci://registry/org/chart or repo/chart)"`
	Version string `json:"version,omitempty" jsonschema_description:"Chart version (required for OCI charts)"`
}

type showChartInput struct {
	Chart   string `json:"chart" jsonschema_description:"Chart reference (e.g. oci://registry/org/chart or repo/chart)"`
	Version string `json:"version,omitempty" jsonschema_description:"Chart version"`
}

type valuesDiffInput struct {
	Chart      string `json:"chart" jsonschema_description:"Chart reference (e.g. oci://harbor.example.com/org/mychart)"`
	OldVersion string `json:"oldVersion" jsonschema_description:"Old chart version to compare from"`
	NewVersion string `json:"newVersion" jsonschema_description:"New chart version to compare to"`
}

type getValuesInput struct {
	Release   string `json:"release" jsonschema_description:"Helm release name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace of the release"`
	All       bool   `json:"all,omitempty" jsonschema_description:"Show all values (including chart defaults), not just user-supplied"`
}

type driftInput struct {
	Release   string `json:"release" jsonschema_description:"Helm release name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace of the release"`
	Chart     string `json:"chart,omitempty" jsonschema_description:"Chart reference (auto-detected from release if omitted)"`
	Version   string `json:"version,omitempty" jsonschema_description:"Chart version (auto-detected from release if omitted)"`
}

// ── Handlers ──

func handleShowValues(ctx context.Context, _ *mcp.CallToolRequest, in showValuesInput) (*mcp.CallToolResult, any, error) {
	if in.Chart == "" {
		return mcputil.ErrResult("chart is required"), nil, nil
	}
	args := []string{"show", "values", in.Chart}
	if in.Version != "" {
		args = append(args, "--version", in.Version)
	}
	return helm(ctx, args...), nil, nil
}

func handleShowChart(ctx context.Context, _ *mcp.CallToolRequest, in showChartInput) (*mcp.CallToolResult, any, error) {
	if in.Chart == "" {
		return mcputil.ErrResult("chart is required"), nil, nil
	}
	args := []string{"show", "chart", in.Chart}
	if in.Version != "" {
		args = append(args, "--version", in.Version)
	}
	return helm(ctx, args...), nil, nil
}

func handleValuesDiff(ctx context.Context, _ *mcp.CallToolRequest, in valuesDiffInput) (*mcp.CallToolResult, any, error) {
	if in.Chart == "" || in.OldVersion == "" || in.NewVersion == "" {
		return mcputil.ErrResult("chart, oldVersion, and newVersion are all required"), nil, nil
	}

	oldValues, err := helmOutput(ctx, "show", "values", in.Chart, "--version", in.OldVersion)
	if err != nil {
		return mcputil.ErrResult("failed to get old values (%s): %s", in.OldVersion, err), nil, nil
	}

	newValues, err := helmOutput(ctx, "show", "values", in.Chart, "--version", in.NewVersion)
	if err != nil {
		return mcputil.ErrResult("failed to get new values (%s): %s", in.NewVersion, err), nil, nil
	}

	diff := diffYAML(oldValues, newValues)
	if diff == "" {
		diff = "No differences in default values between " + in.OldVersion + " and " + in.NewVersion
	}

	header := fmt.Sprintf("## Values diff: %s %s → %s\n\n", in.Chart, in.OldVersion, in.NewVersion)
	return mcputil.TextResult(header + diff), nil, nil
}

func handleGetValues(ctx context.Context, _ *mcp.CallToolRequest, in getValuesInput) (*mcp.CallToolResult, any, error) {
	if in.Release == "" {
		return mcputil.ErrResult("release is required"), nil, nil
	}
	args := []string{"get", "values", in.Release, "-o", "yaml"}
	if in.Namespace != "" {
		args = append(args, "-n", in.Namespace)
	}
	if in.All {
		args = append(args, "--all")
	}
	return helm(ctx, args...), nil, nil
}

func handleDrift(ctx context.Context, _ *mcp.CallToolRequest, in driftInput) (*mcp.CallToolResult, any, error) {
	if in.Release == "" {
		return mcputil.ErrResult("release is required"), nil, nil
	}

	ns := in.Namespace
	if ns == "" {
		ns = "default"
	}

	// Get release's current effective values (all = defaults + overrides)
	allArgs := []string{"get", "values", in.Release, "-n", ns, "-o", "yaml", "--all"}
	releaseValues, err := helmOutput(ctx, allArgs...)
	if err != nil {
		return mcputil.ErrResult("failed to get release values: %s", err), nil, nil
	}

	// Get chart reference and version from release metadata if not provided
	chart := in.Chart
	version := in.Version
	if chart == "" || version == "" {
		metaOut, err := helmOutput(ctx, "list", "-n", ns, "-f", "^"+in.Release+"$", "-o", "yaml")
		if err != nil {
			return mcputil.ErrResult("failed to get release metadata: %s", err), nil, nil
		}
		c, v := parseReleaseChartInfo(metaOut)
		if chart == "" {
			chart = c
		}
		if version == "" {
			version = v
		}
	}
	if chart == "" {
		return mcputil.ErrResult("could not determine chart reference — provide it explicitly"), nil, nil
	}

	// Get chart defaults
	defaultArgs := []string{"show", "values", chart}
	if version != "" {
		defaultArgs = append(defaultArgs, "--version", version)
	}
	defaultValues, err := helmOutput(ctx, defaultArgs...)
	if err != nil {
		return mcputil.ErrResult("failed to get chart defaults for %s@%s: %s", chart, version, err), nil, nil
	}

	diff := diffYAML(defaultValues, releaseValues)
	if diff == "" {
		diff = "No drift — release values match chart defaults exactly."
	}

	header := fmt.Sprintf("## Drift report: %s (%s@%s)\n\n", in.Release, chart, version)
	return mcputil.TextResult(header + diff), nil, nil
}

// ── YAML diff engine ──

// diffYAML compares two YAML documents and returns a human-readable diff
// showing added, removed, and changed keys with their values.
func diffYAML(oldYAML, newYAML string) string {
	oldMap := make(map[string]any)
	newMap := make(map[string]any)
	_ = yaml.Unmarshal([]byte(oldYAML), &oldMap)
	_ = yaml.Unmarshal([]byte(newYAML), &newMap)

	oldFlat := flatten("", oldMap)
	newFlat := flatten("", newMap)

	var added, removed, changed []string

	for k, v := range newFlat {
		if ov, exists := oldFlat[k]; !exists {
			added = append(added, fmt.Sprintf("  + %s: %s", k, formatVal(v)))
		} else if fmt.Sprintf("%v", ov) != fmt.Sprintf("%v", v) {
			changed = append(changed, fmt.Sprintf("  ~ %s: %s → %s", k, formatVal(ov), formatVal(v)))
		}
	}
	for k, v := range oldFlat {
		if _, exists := newFlat[k]; !exists {
			removed = append(removed, fmt.Sprintf("  - %s: %s", k, formatVal(v)))
		}
	}

	sort.Strings(added)
	sort.Strings(removed)
	sort.Strings(changed)

	var sb strings.Builder
	if len(added) > 0 {
		sb.WriteString("### Added keys\n```\n")
		sb.WriteString(strings.Join(added, "\n"))
		sb.WriteString("\n```\n\n")
	}
	if len(removed) > 0 {
		sb.WriteString("### Removed keys\n```\n")
		sb.WriteString(strings.Join(removed, "\n"))
		sb.WriteString("\n```\n\n")
	}
	if len(changed) > 0 {
		sb.WriteString("### Changed values\n```\n")
		sb.WriteString(strings.Join(changed, "\n"))
		sb.WriteString("\n```\n\n")
	}
	return sb.String()
}

// flatten recursively flattens a nested map into dot-separated keys.
func flatten(prefix string, m map[string]any) map[string]any {
	result := make(map[string]any)
	for k, v := range m {
		key := k
		if prefix != "" {
			key = prefix + "." + k
		}
		switch val := v.(type) {
		case map[string]any:
			for fk, fv := range flatten(key, val) {
				result[fk] = fv
			}
		default:
			result[key] = v
			_ = val
		}
	}
	return result
}

// formatVal formats a value for display, truncating long strings.
func formatVal(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

// parseReleaseChartInfo extracts chart name and version from `helm list -o yaml` output.
func parseReleaseChartInfo(listYAML string) (chart, version string) {
	// helm list output is a YAML array with fields like:
	//   chart: mychart-1.2.3
	//   app_version: ...
	var releases []map[string]any
	if err := yaml.Unmarshal([]byte(listYAML), &releases); err != nil || len(releases) == 0 {
		return "", ""
	}
	chartField, _ := releases[0]["chart"].(string)
	// chartField is like "tcaas-sre-assistant-0.1.8" — split at last hyphen before version
	// This is unreliable for OCI charts; the user should provide chart ref explicitly.
	// We'll try to extract version from the end.
	if idx := strings.LastIndex(chartField, "-"); idx > 0 {
		version = chartField[idx+1:]
	}
	return "", version
}

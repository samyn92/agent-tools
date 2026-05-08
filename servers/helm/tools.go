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
	SourceRef string `json:"sourceRef" jsonschema_description:"Name of the Flux HelmRepository sourceRef (e.g. mavenir-oci, t5g-oci, agentops)"`
	Chart     string `json:"chart" jsonschema_description:"Chart name from the HelmRelease spec.chart.spec.chart field"`
	Version   string `json:"version,omitempty" jsonschema_description:"Chart version"`
}

type showChartInput struct {
	SourceRef string `json:"sourceRef" jsonschema_description:"Name of the Flux HelmRepository sourceRef (e.g. mavenir-oci, t5g-oci, agentops)"`
	Chart     string `json:"chart" jsonschema_description:"Chart name from the HelmRelease spec.chart.spec.chart field"`
	Version   string `json:"version,omitempty" jsonschema_description:"Chart version"`
}

type valuesDiffInput struct {
	SourceRef  string `json:"sourceRef" jsonschema_description:"Name of the Flux HelmRepository sourceRef (e.g. mavenir-oci, t5g-oci, agentops)"`
	Chart      string `json:"chart" jsonschema_description:"Chart name from the HelmRelease spec.chart.spec.chart field"`
	OldVersion string `json:"oldVersion" jsonschema_description:"Old chart version to compare from"`
	NewVersion string `json:"newVersion" jsonschema_description:"New chart version to compare to"`
}

type getValuesInput struct {
	Release   string `json:"release" jsonschema_description:"Helm release name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace of the release"`
	All       bool   `json:"all,omitempty" jsonschema_description:"Show all values (including chart defaults), not just user-supplied"`
}

type driftInput struct {
	SourceRef string `json:"sourceRef,omitempty" jsonschema_description:"Name of the Flux HelmRepository sourceRef (e.g. mavenir-oci, t5g-oci, agentops)"`
	Chart     string `json:"chart,omitempty" jsonschema_description:"Chart name from the HelmRelease spec.chart.spec.chart field"`
	Release   string `json:"release" jsonschema_description:"Helm release name"`
	Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace of the release"`
	Version   string `json:"version,omitempty" jsonschema_description:"Chart version (auto-detected from release if omitted)"`
}

// ── Handlers ──

func handleShowValues(ctx context.Context, _ *mcp.CallToolRequest, in showValuesInput) (*mcp.CallToolResult, any, error) {
	chartURL, err := resolveChartURL(in.SourceRef, in.Chart)
	if err != nil {
		return mcputil.ErrResult("%s", err), nil, nil
	}
	args := []string{"show", "values", chartURL}
	if in.Version != "" {
		args = append(args, "--version", in.Version)
	}
	return helm(ctx, args...), nil, nil
}

func handleShowChart(ctx context.Context, _ *mcp.CallToolRequest, in showChartInput) (*mcp.CallToolResult, any, error) {
	chartURL, err := resolveChartURL(in.SourceRef, in.Chart)
	if err != nil {
		return mcputil.ErrResult("%s", err), nil, nil
	}
	args := []string{"show", "chart", chartURL}
	if in.Version != "" {
		args = append(args, "--version", in.Version)
	}
	return helm(ctx, args...), nil, nil
}

func handleValuesDiff(ctx context.Context, _ *mcp.CallToolRequest, in valuesDiffInput) (*mcp.CallToolResult, any, error) {
	if in.OldVersion == "" || in.NewVersion == "" {
		return mcputil.ErrResult("oldVersion and newVersion are required"), nil, nil
	}

	chartURL, err := resolveChartURL(in.SourceRef, in.Chart)
	if err != nil {
		return mcputil.ErrResult("%s", err), nil, nil
	}

	oldValues, err := helmOutput(ctx, "show", "values", chartURL, "--version", in.OldVersion)
	if err != nil {
		return mcputil.ErrResult("failed to get old values (%s): %s", in.OldVersion, err), nil, nil
	}

	newValues, err := helmOutput(ctx, "show", "values", chartURL, "--version", in.NewVersion)
	if err != nil {
		return mcputil.ErrResult("failed to get new values (%s): %s", in.NewVersion, err), nil, nil
	}

	diff := diffYAML(oldValues, newValues)
	if diff == "" {
		diff = "No differences in default values between " + in.OldVersion + " and " + in.NewVersion
	}

	header := fmt.Sprintf("## Values diff: %s %s → %s\n\n", chartURL, in.OldVersion, in.NewVersion)
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

	// Resolve chart URL
	chartURL := ""
	version := in.Version
	if in.SourceRef != "" && in.Chart != "" {
		chartURL, err = resolveChartURL(in.SourceRef, in.Chart)
		if err != nil {
			return mcputil.ErrResult("%s", err), nil, nil
		}
	}

	// Try to get version from release metadata if not provided
	if chartURL == "" || version == "" {
		metaOut, merr := helmOutput(ctx, "list", "-n", ns, "-f", "^"+in.Release+"$", "-o", "yaml")
		if merr != nil {
			return mcputil.ErrResult("failed to get release metadata: %s", merr), nil, nil
		}
		_, v := parseReleaseChartInfo(metaOut)
		if version == "" {
			version = v
		}
	}
	if chartURL == "" {
		return mcputil.ErrResult("could not determine chart — provide sourceRef and chart name"), nil, nil
	}

	// Get chart defaults
	defaultArgs := []string{"show", "values", chartURL}
	if version != "" {
		defaultArgs = append(defaultArgs, "--version", version)
	}
	defaultValues, err := helmOutput(ctx, defaultArgs...)
	if err != nil {
		return mcputil.ErrResult("failed to get chart defaults for %s@%s: %s", chartURL, version, err), nil, nil
	}

	diff := diffYAML(defaultValues, releaseValues)
	if diff == "" {
		diff = "No drift — release values match chart defaults exactly."
	}

	header := fmt.Sprintf("## Drift report: %s (%s@%s)\n\n", in.Release, chartURL, version)
	return mcputil.TextResult(header + diff), nil, nil
}

// ── YAML diff engine ──

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

func formatVal(v any) string {
	s := fmt.Sprintf("%v", v)
	if len(s) > 80 {
		return s[:77] + "..."
	}
	return s
}

func parseReleaseChartInfo(listYAML string) (chart, version string) {
	var releases []map[string]any
	if err := yaml.Unmarshal([]byte(listYAML), &releases); err != nil || len(releases) == 0 {
		return "", ""
	}
	chartField, _ := releases[0]["chart"].(string)
	if idx := strings.LastIndex(chartField, "-"); idx > 0 {
		version = chartField[idx+1:]
	}
	return "", version
}

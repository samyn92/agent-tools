package main

import (
	"context"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Input types ─────────────────────────────────────────────────────

type searchInput struct {
	Agent string `json:"agent,omitempty" jsonschema_description:"Agent name to filter by (exact match). Omit to search all agents."`
	Since string `json:"since,omitempty" jsonschema_description:"Time window: '1h', '6h', '24h', '72h', '7d', or RFC3339 timestamp. Default: 24h."`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Max traces to return (default 50, max 200)."`
}

type getInput struct {
	TraceID string `json:"traceId" jsonschema_description:"The trace ID to fetch (hex string from tempo_search results)."`
}

type agentStatsInput struct {
	Agent string `json:"agent" jsonschema_description:"Agent name to compute stats for."`
	Since string `json:"since,omitempty" jsonschema_description:"Time window: '1h', '6h', '24h', '72h', '7d'. Default: 24h."`
}

type slowToolsInput struct {
	Agent string `json:"agent,omitempty" jsonschema_description:"Agent name to filter by. Omit for all agents."`
	Since string `json:"since,omitempty" jsonschema_description:"Time window. Default: 24h."`
	Limit int    `json:"limit,omitempty" jsonschema_description:"Max results (default 20)."`
}

type errorsInput struct {
	Agent string `json:"agent,omitempty" jsonschema_description:"Agent name to filter by. Omit for all agents."`
	Since string `json:"since,omitempty" jsonschema_description:"Time window. Default: 24h."`
}

type compareInput struct {
	TraceA string `json:"traceA" jsonschema_description:"First trace ID (typically the 'before' trace)."`
	TraceB string `json:"traceB" jsonschema_description:"Second trace ID (typically the 'after' trace)."`
}

// ── Handlers ────────────────────────────────────────────────────────

func handleSearch(_ context.Context, _ *mcp.CallToolRequest, in searchInput) (*mcp.CallToolResult, any, error) {
	// Build TraceQL query
	var query string
	if in.Agent != "" {
		query = fmt.Sprintf(`{ resource.agent.name = "%s" } | select(resource.agent.name, resource.agent.mode)`, in.Agent)
	} else {
		query = `{ resource.agent.name =~ ".+" } | select(resource.agent.name, resource.agent.mode)`
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 50
	}
	if limit > 200 {
		limit = 200
	}

	start, end := parseTimeRange(in.Since)

	resp, err := tempoSearch(query, limit, start, end)
	if err != nil {
		return errResult("tempo search: %v", err), nil, nil
	}

	// For each trace, fetch the full trace to extract detailed metadata.
	// To avoid overloading Tempo, we only do this for up to 50 traces.
	// For larger result sets, we return the summary from the search response.
	result := traceSearchResult{
		Query: query,
		Since: or(in.Since, "24h"),
		Total: len(resp.Traces),
	}

	for _, t := range resp.Traces {
		ts := traceSummary{
			TraceID:    t.TraceID,
			DurationMs: float64(t.DurationMs),
			Duration:   fmtDuration(float64(t.DurationMs)),
			RootSpan:   t.RootTraceName,
		}

		// Parse start time from nanos
		if nanos, err := strconv.ParseInt(t.StartTimeUnixNano, 10, 64); err == nil {
			ts.StartTime = time.Unix(0, nanos).UTC().Format(time.RFC3339)
		}

		// Extract agent name/mode from spanset attributes if available
		// Tempo returns both spanSet (singular) and spanSets (plural).
		// The select() in TraceQL outputs span-level attribute keys (without resource. prefix).
		allSpanSets := t.SpanSets
		if t.SpanSet != nil {
			allSpanSets = append(allSpanSets, *t.SpanSet)
		}
		for _, ss := range allSpanSets {
			for _, span := range ss.Spans {
				for _, attr := range span.Attributes {
					switch attr.Key {
					case "resource.agent.name", "agent.name":
						if attr.Value.StringValue != "" {
							ts.AgentName = attr.Value.StringValue
						}
					case "resource.agent.mode", "agent.mode":
						if attr.Value.StringValue != "" {
							ts.AgentMode = attr.Value.StringValue
						}
					}
				}
			}
		}

		// If we have agent name from service name, use it as fallback
		if ts.AgentName == "" {
			ts.AgentName = t.RootServiceName
		}

		result.Traces = append(result.Traces, ts)
	}

	return jsonResult(result), nil, nil
}

func handleGet(_ context.Context, _ *mcp.CallToolRequest, in getInput) (*mcp.CallToolResult, any, error) {
	if in.TraceID == "" {
		return errResult("traceId is required"), nil, nil
	}

	trace, err := tempoGetTrace(in.TraceID)
	if err != nil {
		return errResult("fetching trace: %v", err), nil, nil
	}

	spans := extractSpans(trace)
	if len(spans) == 0 {
		return errResult("trace %s has no spans", in.TraceID), nil, nil
	}

	root := findRootSpan(spans)
	if root == nil {
		return errResult("no root span found in trace"), nil, nil
	}

	detail := traceDetail{
		TraceID:   in.TraceID,
		AgentName: getAttr(root, "agent.name"),
		AgentMode: getAttr(root, "agent.mode"),
		Model:     getAttr(root, "gen_ai.request.model"),
		SpanCount: len(spans),
	}

	// Duration from root span
	detail.DurationMs = root.DurationMs
	detail.Duration = fmtDuration(root.DurationMs)
	if root.StartTimeUnix > 0 {
		detail.StartTime = time.Unix(0, root.StartTimeUnix).UTC().Format(time.RFC3339)
	}

	// Count steps
	stepSpans := filterSpansByName(spans, "agent.step")
	detail.StepCount = len(stepSpans)

	// Extract token usage from root span attributes or step spans
	detail.InputTokens = attrInt(root, "gen_ai.usage.input_tokens")
	detail.OutputTokens = attrInt(root, "gen_ai.usage.output_tokens")

	// If no tokens on root, sum from steps
	if detail.InputTokens == 0 {
		for _, s := range stepSpans {
			detail.InputTokens += attrInt(&s, "gen_ai.usage.input_tokens")
			detail.OutputTokens += attrInt(&s, "gen_ai.usage.output_tokens")
		}
	}

	// Extract tool calls
	toolSpans := filterSpansByName(spans, "tool.execute")
	for _, s := range toolSpans {
		tc := toolCallInfo{
			Name:       getAttr(&s, "tool.name"),
			Type:       getAttr(&s, "tool.type"),
			DurationMs: s.DurationMs,
			Duration:   fmtDuration(s.DurationMs),
			Step:       getAttr(&s, "tool.step"),
		}
		if errMsg := getAttr(&s, "tool.error"); errMsg != "" {
			tc.HasError = true
			tc.Error = errMsg
		}
		if isErrorStatus(s.Status.Code) { // ERROR
			tc.HasError = true
			if tc.Error == "" {
				tc.Error = s.Status.Message
			}
		}
		detail.ToolCalls = append(detail.ToolCalls, tc)
	}

	// Extract memory operations
	memSpans := filterSpansByName(spans, "memory.")
	for _, s := range memSpans {
		mo := memoryOpInfo{
			Operation:  s.Name,
			DurationMs: s.DurationMs,
			Project:    getAttr(&s, "memory.project"),
		}
		detail.MemoryOps = append(detail.MemoryOps, mo)
	}

	// Extract errors
	for _, s := range spans {
		if isErrorStatus(s.Status.Code) {
			detail.Errors = append(detail.Errors, spanError{
				SpanName: s.Name,
				Error:    s.Status.Message,
				Type:     getAttr(&s, "tool.type"),
			})
		}
		if errMsg := getAttr(&s, "tool.error"); errMsg != "" {
			detail.Errors = append(detail.Errors, spanError{
				SpanName: s.Name,
				Error:    errMsg,
				Type:     "tool_error",
			})
		}
	}

	// Extract delegation info
	if parentTrace := getAttr(root, "delegation.parent_trace_id"); parentTrace != "" {
		detail.Delegation = &delegationInfo{
			ParentTraceID: parentTrace,
			ParentAgent:   getAttr(root, "delegation.parent_agent"),
			RunName:       getAttr(root, "delegation.run_name"),
		}
	}

	return jsonResult(detail), nil, nil
}

func handleAgentStats(_ context.Context, _ *mcp.CallToolRequest, in agentStatsInput) (*mcp.CallToolResult, any, error) {
	if in.Agent == "" {
		return errResult("agent name is required"), nil, nil
	}

	query := fmt.Sprintf(`{ resource.agent.name = "%s" } | select(resource.agent.name, resource.agent.mode)`, in.Agent)
	start, end := parseTimeRange(in.Since)

	resp, err := tempoSearch(query, 200, start, end)
	if err != nil {
		return errResult("tempo search: %v", err), nil, nil
	}

	if len(resp.Traces) == 0 {
		return errResult("no traces found for agent %q in the last %s", in.Agent, or(in.Since, "24h")), nil, nil
	}

	// Fetch all traces to compute detailed stats
	var durations []float64
	var stepsPerTrace []int
	var inputTokens, outputTokens []int64
	var errorCount int
	toolDurations := map[string][]float64{}
	toolCounts := map[string]int{}
	toolErrors := map[string]int{}
	modelCounts := map[string]int{}

	for _, t := range resp.Traces {
		trace, err := tempoGetTrace(t.TraceID)
		if err != nil {
			continue
		}

		spans := extractSpans(trace)
		root := findRootSpan(spans)
		if root == nil {
			continue
		}

		durations = append(durations, root.DurationMs)

		// Steps
		steps := filterSpansByName(spans, "agent.step")
		stepsPerTrace = append(stepsPerTrace, len(steps))

		// Tokens
		inTok := attrInt(root, "gen_ai.usage.input_tokens")
		outTok := attrInt(root, "gen_ai.usage.output_tokens")
		if inTok == 0 {
			for _, s := range steps {
				inTok += attrInt(&s, "gen_ai.usage.input_tokens")
				outTok += attrInt(&s, "gen_ai.usage.output_tokens")
			}
		}
		inputTokens = append(inputTokens, inTok)
		outputTokens = append(outputTokens, outTok)

		// Model
		if m := getAttr(root, "gen_ai.request.model"); m != "" {
			modelCounts[m]++
		}

		// Errors
		hasErr := false
		for _, s := range spans {
			if isErrorStatus(s.Status.Code) {
				hasErr = true
			}
		}
		if hasErr {
			errorCount++
		}

		// Tool stats
		toolSpans := filterSpansByName(spans, "tool.execute")
		for _, s := range toolSpans {
			name := getAttr(&s, "tool.name")
			if name == "" {
				name = s.Name
			}
			toolDurations[name] = append(toolDurations[name], s.DurationMs)
			toolCounts[name]++
			if isErrorStatus(s.Status.Code) || getAttr(&s, "tool.error") != "" {
				toolErrors[name]++
			}
		}
	}

	result := agentStatsResult{
		AgentName:  in.Agent,
		Since:      or(in.Since, "24h"),
		TraceCount: len(resp.Traces),
		ErrorCount: errorCount,
		ModelUsage: modelCounts,
	}

	// Duration percentiles
	sorted := sortedFloat64s(durations)
	result.Duration = durationStats{
		AvgMs: math.Round(avg(durations)*100) / 100,
		P50Ms: math.Round(percentile(sorted, 50)*100) / 100,
		P95Ms: math.Round(percentile(sorted, 95)*100) / 100,
		P99Ms: math.Round(percentile(sorted, 99)*100) / 100,
		Avg:   fmtDuration(avg(durations)),
		P50:   fmtDuration(percentile(sorted, 50)),
		P95:   fmtDuration(percentile(sorted, 95)),
		P99:   fmtDuration(percentile(sorted, 99)),
		Min:   fmtDuration(sorted[0]),
		Max:   fmtDuration(sorted[len(sorted)-1]),
	}

	// Steps
	if len(stepsPerTrace) > 0 {
		minS, maxS := stepsPerTrace[0], stepsPerTrace[0]
		sumS := 0
		for _, s := range stepsPerTrace {
			sumS += s
			if s < minS {
				minS = s
			}
			if s > maxS {
				maxS = s
			}
		}
		result.Steps = statsBlock{
			Avg: float64(sumS) / float64(len(stepsPerTrace)),
			Min: minS,
			Max: maxS,
		}
	}

	// Tokens
	if len(inputTokens) > 0 {
		result.Tokens = tokenStats{
			AvgInput:    sumInt64(inputTokens) / int64(len(inputTokens)),
			AvgOutput:   sumInt64(outputTokens) / int64(len(outputTokens)),
			TotalInput:  sumInt64(inputTokens),
			TotalOutput: sumInt64(outputTokens),
		}
	}

	// Error rate
	if len(resp.Traces) > 0 {
		result.ErrorRate = math.Round(float64(errorCount)/float64(len(resp.Traces))*10000) / 100
	}

	// Slowest tools (by avg duration)
	type toolAvg struct {
		name   string
		avgMs  float64
		maxMs  float64
		count  int
		errors int
	}
	var toolAvgs []toolAvg
	for name, durations := range toolDurations {
		sorted := sortedFloat64s(durations)
		toolAvgs = append(toolAvgs, toolAvg{
			name:   name,
			avgMs:  avg(durations),
			maxMs:  sorted[len(sorted)-1],
			count:  toolCounts[name],
			errors: toolErrors[name],
		})
	}
	sort.Slice(toolAvgs, func(i, j int) bool { return toolAvgs[i].avgMs > toolAvgs[j].avgMs })
	for i, ta := range toolAvgs {
		if i >= 10 {
			break
		}
		result.SlowestTools = append(result.SlowestTools, toolStatEntry{
			Name:        ta.name,
			Count:       ta.count,
			AvgMs:       math.Round(ta.avgMs*100) / 100,
			MaxMs:       math.Round(ta.maxMs*100) / 100,
			AvgDuration: fmtDuration(ta.avgMs),
			MaxDuration: fmtDuration(ta.maxMs),
			ErrorCount:  ta.errors,
		})
	}

	// Most called tools
	sort.Slice(toolAvgs, func(i, j int) bool { return toolAvgs[i].count > toolAvgs[j].count })
	for i, ta := range toolAvgs {
		if i >= 10 {
			break
		}
		result.MostCalledTools = append(result.MostCalledTools, toolStatEntry{
			Name:        ta.name,
			Count:       ta.count,
			AvgMs:       math.Round(ta.avgMs*100) / 100,
			AvgDuration: fmtDuration(ta.avgMs),
			ErrorCount:  ta.errors,
		})
	}

	return jsonResult(result), nil, nil
}

func handleSlowTools(_ context.Context, _ *mcp.CallToolRequest, in slowToolsInput) (*mcp.CallToolResult, any, error) {
	var query string
	if in.Agent != "" {
		query = fmt.Sprintf(`{ resource.agent.name = "%s" } | select(resource.agent.name)`, in.Agent)
	} else {
		query = `{ resource.agent.name =~ ".+" } | select(resource.agent.name)`
	}

	start, end := parseTimeRange(in.Since)

	resp, err := tempoSearch(query, 100, start, end)
	if err != nil {
		return errResult("tempo search: %v", err), nil, nil
	}

	limit := in.Limit
	if limit <= 0 {
		limit = 20
	}

	var entries []slowToolEntry

	for _, t := range resp.Traces {
		trace, err := tempoGetTrace(t.TraceID)
		if err != nil {
			continue
		}
		spans := extractSpans(trace)
		root := findRootSpan(spans)
		agentName := ""
		if root != nil {
			agentName = getAttr(root, "agent.name")
		}

		toolSpans := filterSpansByName(spans, "tool.execute")
		for _, s := range toolSpans {
			entry := slowToolEntry{
				TraceID:    t.TraceID,
				AgentName:  agentName,
				ToolName:   getAttr(&s, "tool.name"),
				DurationMs: s.DurationMs,
				Duration:   fmtDuration(s.DurationMs),
			}
			if s.StartTimeUnix > 0 {
				entry.Time = time.Unix(0, s.StartTimeUnix).UTC().Format(time.RFC3339)
			}
			if errMsg := getAttr(&s, "tool.error"); errMsg != "" {
				entry.HasError = true
				entry.Error = errMsg
			}
			if isErrorStatus(s.Status.Code) {
				entry.HasError = true
				if entry.Error == "" {
					entry.Error = s.Status.Message
				}
			}
			entries = append(entries, entry)
		}
	}

	// Sort by duration desc
	sort.Slice(entries, func(i, j int) bool { return entries[i].DurationMs > entries[j].DurationMs })

	if len(entries) > limit {
		entries = entries[:limit]
	}

	result := slowToolsResult{
		Since: or(in.Since, "24h"),
		Tools: entries,
	}
	if in.Agent != "" {
		result.AgentName = in.Agent
	}

	return jsonResult(result), nil, nil
}

func handleErrors(_ context.Context, _ *mcp.CallToolRequest, in errorsInput) (*mcp.CallToolResult, any, error) {
	var query string
	if in.Agent != "" {
		query = fmt.Sprintf(`{ resource.agent.name = "%s" && status = error } | select(resource.agent.name)`, in.Agent)
	} else {
		query = `{ status = error } | select(resource.agent.name)`
	}

	start, end := parseTimeRange(in.Since)

	resp, err := tempoSearch(query, 200, start, end)
	if err != nil {
		// Fallback: if status=error TraceQL not supported, search all and filter
		if in.Agent != "" {
			query = fmt.Sprintf(`{ resource.agent.name = "%s" } | select(resource.agent.name)`, in.Agent)
		} else {
			query = `{ resource.agent.name =~ ".+" } | select(resource.agent.name)`
		}
		resp, err = tempoSearch(query, 200, start, end)
		if err != nil {
			return errResult("tempo search: %v", err), nil, nil
		}
	}

	result := errorsResult{
		Since:   or(in.Since, "24h"),
		ByAgent: map[string]int{},
		ByType:  map[string]int{},
	}
	if in.Agent != "" {
		result.AgentName = in.Agent
	}

	for _, t := range resp.Traces {
		trace, err := tempoGetTrace(t.TraceID)
		if err != nil {
			continue
		}
		spans := extractSpans(trace)
		root := findRootSpan(spans)
		agentName := ""
		if root != nil {
			agentName = getAttr(root, "agent.name")
		}

		for _, s := range spans {
			errMsg := ""
			errType := ""

			if isErrorStatus(s.Status.Code) {
				errMsg = s.Status.Message
				errType = "span_error"
			}
			if toolErr := getAttr(&s, "tool.error"); toolErr != "" {
				errMsg = toolErr
				errType = "tool_error"
			}

			if errMsg == "" {
				continue
			}

			// Classify error type
			if strings.Contains(errMsg, "timeout") || strings.Contains(errMsg, "deadline exceeded") {
				errType = "timeout"
			} else if strings.Contains(errMsg, "OOM") || strings.Contains(errMsg, "out of memory") {
				errType = "oom"
			} else if strings.Contains(strings.ToLower(errMsg), "fallback") {
				errType = "model_fallback"
			} else if strings.Contains(errMsg, "rate limit") || strings.Contains(errMsg, "429") {
				errType = "rate_limit"
			}

			entry := errorEntry{
				TraceID:   t.TraceID,
				AgentName: agentName,
				SpanName:  s.Name,
				Error:     errMsg,
				ErrorType: errType,
			}
			if s.StartTimeUnix > 0 {
				entry.Time = time.Unix(0, s.StartTimeUnix).UTC().Format(time.RFC3339)
				entry.DurationMs = s.DurationMs
			}

			result.Errors = append(result.Errors, entry)
			result.ByAgent[agentName]++
			result.ByType[errType]++
		}
	}

	result.Total = len(result.Errors)

	return jsonResult(result), nil, nil
}

func handleCompare(_ context.Context, _ *mcp.CallToolRequest, in compareInput) (*mcp.CallToolResult, any, error) {
	if in.TraceA == "" || in.TraceB == "" {
		return errResult("both traceA and traceB are required"), nil, nil
	}

	traceA, err := tempoGetTrace(in.TraceA)
	if err != nil {
		return errResult("fetching trace A: %v", err), nil, nil
	}
	traceB, err := tempoGetTrace(in.TraceB)
	if err != nil {
		return errResult("fetching trace B: %v", err), nil, nil
	}

	briefA := buildBrief(in.TraceA, traceA)
	briefB := buildBrief(in.TraceB, traceB)

	result := compareResult{
		TraceA: briefA,
		TraceB: briefB,
	}

	// Compare metrics
	if briefB.DurationMs < briefA.DurationMs {
		pct := (briefA.DurationMs - briefB.DurationMs) / briefA.DurationMs * 100
		result.Improvements = append(result.Improvements,
			fmt.Sprintf("Duration improved: %s -> %s (%.1f%% faster)", briefA.Duration, briefB.Duration, pct))
	} else if briefB.DurationMs > briefA.DurationMs {
		pct := (briefB.DurationMs - briefA.DurationMs) / briefA.DurationMs * 100
		result.Regressions = append(result.Regressions,
			fmt.Sprintf("Duration regressed: %s -> %s (%.1f%% slower)", briefA.Duration, briefB.Duration, pct))
	} else {
		result.Unchanged = append(result.Unchanged, fmt.Sprintf("Duration unchanged: %s", briefA.Duration))
	}

	if briefB.StepCount < briefA.StepCount {
		result.Improvements = append(result.Improvements,
			fmt.Sprintf("Steps reduced: %d -> %d", briefA.StepCount, briefB.StepCount))
	} else if briefB.StepCount > briefA.StepCount {
		result.Regressions = append(result.Regressions,
			fmt.Sprintf("Steps increased: %d -> %d", briefA.StepCount, briefB.StepCount))
	} else {
		result.Unchanged = append(result.Unchanged, fmt.Sprintf("Steps unchanged: %d", briefA.StepCount))
	}

	if briefB.ToolCalls < briefA.ToolCalls {
		result.Improvements = append(result.Improvements,
			fmt.Sprintf("Tool calls reduced: %d -> %d", briefA.ToolCalls, briefB.ToolCalls))
	} else if briefB.ToolCalls > briefA.ToolCalls {
		result.Regressions = append(result.Regressions,
			fmt.Sprintf("Tool calls increased: %d -> %d", briefA.ToolCalls, briefB.ToolCalls))
	}

	totalTokensA := briefA.InputTokens + briefA.OutputTokens
	totalTokensB := briefB.InputTokens + briefB.OutputTokens
	if totalTokensB < totalTokensA && totalTokensA > 0 {
		pct := float64(totalTokensA-totalTokensB) / float64(totalTokensA) * 100
		result.Improvements = append(result.Improvements,
			fmt.Sprintf("Token usage reduced: %d -> %d (%.1f%% less)", totalTokensA, totalTokensB, pct))
	} else if totalTokensB > totalTokensA && totalTokensA > 0 {
		pct := float64(totalTokensB-totalTokensA) / float64(totalTokensA) * 100
		result.Regressions = append(result.Regressions,
			fmt.Sprintf("Token usage increased: %d -> %d (%.1f%% more)", totalTokensA, totalTokensB, pct))
	}

	if briefB.ErrorCount < briefA.ErrorCount {
		result.Improvements = append(result.Improvements,
			fmt.Sprintf("Errors reduced: %d -> %d", briefA.ErrorCount, briefB.ErrorCount))
	} else if briefB.ErrorCount > briefA.ErrorCount {
		result.Regressions = append(result.Regressions,
			fmt.Sprintf("Errors increased: %d -> %d", briefA.ErrorCount, briefB.ErrorCount))
	}

	// Tool-level diff
	toolCountsA := buildToolCounts(traceA)
	toolCountsB := buildToolCounts(traceB)
	allTools := map[string]bool{}
	for k := range toolCountsA {
		allTools[k] = true
	}
	for k := range toolCountsB {
		allTools[k] = true
	}
	for tool := range allTools {
		ca, cb := toolCountsA[tool], toolCountsB[tool]
		result.ToolDiff = append(result.ToolDiff, toolDiffEntry{
			ToolName:     tool,
			CountA:       ca.count,
			CountB:       cb.count,
			AvgDurationA: ca.avgMs,
			AvgDurationB: cb.avgMs,
		})
	}
	sort.Slice(result.ToolDiff, func(i, j int) bool {
		return result.ToolDiff[i].ToolName < result.ToolDiff[j].ToolName
	})

	return jsonResult(result), nil, nil
}

// ── compare helpers ─────────────────────────────────────────────────

func buildBrief(traceID string, trace *otlpTraceResponse) traceBrief {
	spans := extractSpans(trace)
	root := findRootSpan(spans)

	brief := traceBrief{TraceID: traceID}
	if root != nil {
		brief.AgentName = getAttr(root, "agent.name")
		brief.DurationMs = root.DurationMs
		brief.Duration = fmtDuration(root.DurationMs)
		if root.StartTimeUnix > 0 {
			brief.StartTime = time.Unix(0, root.StartTimeUnix).UTC().Format(time.RFC3339)
		}

		brief.InputTokens = attrInt(root, "gen_ai.usage.input_tokens")
		brief.OutputTokens = attrInt(root, "gen_ai.usage.output_tokens")
	}

	brief.StepCount = len(filterSpansByName(spans, "agent.step"))
	brief.ToolCalls = len(filterSpansByName(spans, "tool.execute"))

	for _, s := range spans {
		if isErrorStatus(s.Status.Code) || getAttr(&s, "tool.error") != "" {
			brief.ErrorCount++
		}
	}

	// Sum tokens from steps if not on root
	if brief.InputTokens == 0 {
		for _, s := range filterSpansByName(spans, "agent.step") {
			brief.InputTokens += attrInt(&s, "gen_ai.usage.input_tokens")
			brief.OutputTokens += attrInt(&s, "gen_ai.usage.output_tokens")
		}
	}

	return brief
}

type toolCountInfo struct {
	count int
	avgMs float64
}

func buildToolCounts(trace *otlpTraceResponse) map[string]toolCountInfo {
	spans := extractSpans(trace)
	durations := map[string][]float64{}
	for _, s := range filterSpansByName(spans, "tool.execute") {
		name := getAttr(&s, "tool.name")
		if name == "" {
			name = s.Name
		}
		durations[name] = append(durations[name], s.DurationMs)
	}

	result := map[string]toolCountInfo{}
	for name, durs := range durations {
		result[name] = toolCountInfo{
			count: len(durs),
			avgMs: math.Round(avg(durs)*100) / 100,
		}
	}
	return result
}

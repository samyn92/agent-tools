package main

// ── Tempo API response types ────────────────────────────────────────

// searchResponse is the Tempo /api/search response.
type searchResponse struct {
	Traces []searchTrace `json:"traces"`
}

type searchTrace struct {
	TraceID           string         `json:"traceID"`
	RootServiceName   string         `json:"rootServiceName"`
	RootTraceName     string         `json:"rootTraceName"`
	StartTimeUnixNano string         `json:"startTimeUnixNano"`
	DurationMs        int            `json:"durationMs"`
	SpanSet           *spanSet       `json:"spanSet,omitempty"`
	SpanSets          []spanSet      `json:"spanSets,omitempty"`
	ServiceStats      map[string]any `json:"serviceStats,omitempty"`
}

type spanSet struct {
	Spans      []spanSetSpan `json:"spans"`
	Matched    int           `json:"matched"`
	Attributes []any         `json:"attributes,omitempty"`
}

type spanSetSpan struct {
	SpanID            string          `json:"spanID"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	DurationNanos     string          `json:"durationNanos"`
	Attributes        []otlpAttribute `json:"attributes,omitempty"`
}

// ── OTLP trace response types ───────────────────────────────────────

// otlpTraceResponse is Tempo's GET /api/traces/{traceID} JSON response.
type otlpTraceResponse struct {
	Batches []resourceSpans `json:"batches"`
}

type resourceSpans struct {
	Resource   otlpResource `json:"resource"`
	ScopeSpans []scopeSpans `json:"scopeSpans"`
}

type otlpResource struct {
	Attributes []otlpAttribute `json:"attributes"`
}

type scopeSpans struct {
	Scope otlpScope  `json:"scope"`
	Spans []otlpSpan `json:"spans"`
}

type otlpScope struct {
	Name    string `json:"name"`
	Version string `json:"version"`
}

type otlpSpan struct {
	TraceID           string          `json:"traceId"`
	SpanID            string          `json:"spanId"`
	ParentSpanID      string          `json:"parentSpanId"`
	Name              string          `json:"name"`
	Kind              string          `json:"kind"`
	StartTimeUnixNano string          `json:"startTimeUnixNano"`
	EndTimeUnixNano   string          `json:"endTimeUnixNano"`
	Attributes        []otlpAttribute `json:"attributes"`
	Events            []otlpEvent     `json:"events"`
	Status            otlpStatus      `json:"status"`
	Links             []otlpLink      `json:"links"`
}

type otlpAttribute struct {
	Key   string    `json:"key"`
	Value attrValue `json:"value"`
}

type attrValue struct {
	StringValue string  `json:"stringValue,omitempty"`
	IntValue    string  `json:"intValue,omitempty"`
	DoubleValue float64 `json:"doubleValue,omitempty"`
	BoolValue   bool    `json:"boolValue,omitempty"`
}

type otlpEvent struct {
	TimeUnixNano string          `json:"timeUnixNano"`
	Name         string          `json:"name"`
	Attributes   []otlpAttribute `json:"attributes"`
}

type otlpStatus struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type otlpLink struct {
	TraceID    string          `json:"traceId"`
	SpanID     string          `json:"spanId"`
	Attributes []otlpAttribute `json:"attributes"`
}

// ── Internal flat span type ─────────────────────────────────────────

// flatSpan is a denormalized span with resource attributes attached.
type flatSpan struct {
	TraceID       string            `json:"traceId"`
	SpanID        string            `json:"spanId"`
	ParentSpanID  string            `json:"parentSpanId"`
	Name          string            `json:"name"`
	Kind          string            `json:"kind"`
	StartTimeUnix int64             `json:"-"`
	EndTimeUnix   int64             `json:"-"`
	DurationMs    float64           `json:"durationMs"`
	Attributes    map[string]string `json:"attributes"`
	ResAttributes map[string]string `json:"-"`
	Events        []otlpEvent       `json:"-"`
	Status        otlpStatus        `json:"-"`
	Links         []otlpLink        `json:"-"`
}

// ── Output types ────────────────────────────────────────────────────

type traceSearchResult struct {
	Traces []traceSummary `json:"traces"`
	Total  int            `json:"total"`
	Query  string         `json:"query"`
	Since  string         `json:"since"`
}

type traceSummary struct {
	TraceID      string  `json:"traceId"`
	AgentName    string  `json:"agentName"`
	AgentMode    string  `json:"agentMode"`
	DurationMs   float64 `json:"durationMs"`
	Duration     string  `json:"duration"`
	StartTime    string  `json:"startTime"`
	StepCount    int     `json:"stepCount"`
	ToolCalls    int     `json:"toolCalls"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	HasErrors    bool    `json:"hasErrors"`
	ErrorCount   int     `json:"errorCount,omitempty"`
	RootSpan     string  `json:"rootSpan"`
}

type traceDetail struct {
	TraceID      string          `json:"traceId"`
	AgentName    string          `json:"agentName"`
	AgentMode    string          `json:"agentMode"`
	Model        string          `json:"model"`
	DurationMs   float64         `json:"durationMs"`
	Duration     string          `json:"duration"`
	StartTime    string          `json:"startTime"`
	StepCount    int             `json:"stepCount"`
	InputTokens  int64           `json:"inputTokens"`
	OutputTokens int64           `json:"outputTokens"`
	ToolCalls    []toolCallInfo  `json:"toolCalls"`
	Errors       []spanError     `json:"errors,omitempty"`
	MemoryOps    []memoryOpInfo  `json:"memoryOps,omitempty"`
	Delegation   *delegationInfo `json:"delegation,omitempty"`
	SpanCount    int             `json:"spanCount"`
}

type toolCallInfo struct {
	Name       string  `json:"name"`
	Type       string  `json:"type,omitempty"`
	DurationMs float64 `json:"durationMs"`
	Duration   string  `json:"duration"`
	HasError   bool    `json:"hasError"`
	Error      string  `json:"error,omitempty"`
	Step       string  `json:"step,omitempty"`
}

type spanError struct {
	SpanName string `json:"spanName"`
	Error    string `json:"error"`
	Type     string `json:"type,omitempty"`
}

type memoryOpInfo struct {
	Operation  string  `json:"operation"`
	DurationMs float64 `json:"durationMs"`
	Project    string  `json:"project,omitempty"`
}

type delegationInfo struct {
	ParentTraceID string `json:"parentTraceId,omitempty"`
	ParentAgent   string `json:"parentAgent,omitempty"`
	RunName       string `json:"runName,omitempty"`
}

type agentStatsResult struct {
	AgentName       string          `json:"agentName"`
	Since           string          `json:"since"`
	TraceCount      int             `json:"traceCount"`
	Duration        durationStats   `json:"duration"`
	Steps           statsBlock      `json:"steps"`
	Tokens          tokenStats      `json:"tokens"`
	ErrorRate       float64         `json:"errorRate"`
	ErrorCount      int             `json:"errorCount"`
	SlowestTools    []toolStatEntry `json:"slowestTools"`
	MostCalledTools []toolStatEntry `json:"mostCalledTools"`
	ModelUsage      map[string]int  `json:"modelUsage,omitempty"`
}

type durationStats struct {
	Avg   string  `json:"avg"`
	P50   string  `json:"p50"`
	P95   string  `json:"p95"`
	P99   string  `json:"p99"`
	Min   string  `json:"min"`
	Max   string  `json:"max"`
	AvgMs float64 `json:"avgMs"`
	P50Ms float64 `json:"p50Ms"`
	P95Ms float64 `json:"p95Ms"`
	P99Ms float64 `json:"p99Ms"`
}

type statsBlock struct {
	Avg float64 `json:"avg"`
	Min int     `json:"min"`
	Max int     `json:"max"`
}

type tokenStats struct {
	AvgInput    int64 `json:"avgInput"`
	AvgOutput   int64 `json:"avgOutput"`
	TotalInput  int64 `json:"totalInput"`
	TotalOutput int64 `json:"totalOutput"`
}

type toolStatEntry struct {
	Name        string  `json:"name"`
	Count       int     `json:"count,omitempty"`
	AvgMs       float64 `json:"avgMs,omitempty"`
	MaxMs       float64 `json:"maxMs,omitempty"`
	AvgDuration string  `json:"avgDuration,omitempty"`
	MaxDuration string  `json:"maxDuration,omitempty"`
	ErrorCount  int     `json:"errorCount,omitempty"`
}

type slowToolsResult struct {
	Since     string          `json:"since"`
	AgentName string          `json:"agentName,omitempty"`
	Tools     []slowToolEntry `json:"tools"`
}

type slowToolEntry struct {
	TraceID    string  `json:"traceId"`
	AgentName  string  `json:"agentName"`
	ToolName   string  `json:"toolName"`
	DurationMs float64 `json:"durationMs"`
	Duration   string  `json:"duration"`
	HasError   bool    `json:"hasError"`
	Error      string  `json:"error,omitempty"`
	Time       string  `json:"time"`
}

type errorsResult struct {
	Since     string         `json:"since"`
	AgentName string         `json:"agentName,omitempty"`
	Total     int            `json:"total"`
	ByAgent   map[string]int `json:"byAgent"`
	ByType    map[string]int `json:"byType"`
	Errors    []errorEntry   `json:"errors"`
}

type errorEntry struct {
	TraceID    string  `json:"traceId"`
	AgentName  string  `json:"agentName"`
	SpanName   string  `json:"spanName"`
	Error      string  `json:"error"`
	ErrorType  string  `json:"errorType"`
	DurationMs float64 `json:"durationMs,omitempty"`
	Time       string  `json:"time"`
}

type compareResult struct {
	TraceA       traceBrief      `json:"traceA"`
	TraceB       traceBrief      `json:"traceB"`
	Improvements []string        `json:"improvements"`
	Regressions  []string        `json:"regressions"`
	Unchanged    []string        `json:"unchanged"`
	ToolDiff     []toolDiffEntry `json:"toolDiff,omitempty"`
}

type traceBrief struct {
	TraceID      string  `json:"traceId"`
	AgentName    string  `json:"agentName"`
	DurationMs   float64 `json:"durationMs"`
	Duration     string  `json:"duration"`
	StepCount    int     `json:"stepCount"`
	ToolCalls    int     `json:"toolCalls"`
	InputTokens  int64   `json:"inputTokens"`
	OutputTokens int64   `json:"outputTokens"`
	ErrorCount   int     `json:"errorCount"`
	StartTime    string  `json:"startTime"`
}

type toolDiffEntry struct {
	ToolName     string  `json:"toolName"`
	CountA       int     `json:"countA"`
	CountB       int     `json:"countB"`
	AvgDurationA float64 `json:"avgDurationMsA"`
	AvgDurationB float64 `json:"avgDurationMsB"`
}

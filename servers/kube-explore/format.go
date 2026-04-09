/*
format.go — JSON response structures for all kube-explore tools.

Every struct here maps 1:1 to the response structures defined in PLAN_intent-tools.md.
All tools return these structs serialized as JSON via jsonMarshalResult().
*/
package main

// ====================================================================
// kube_find response
// ====================================================================

// FindResponse is the top-level response for kube_find.
type FindResponse struct {
	Query        string       `json:"query"`
	TotalMatches int          `json:"total_matches"`
	Results      []FindResult `json:"results"`
}

// FindResult is a single search result in kube_find.
type FindResult struct {
	Kind        string            `json:"kind"`
	Namespace   string            `json:"namespace,omitempty"`
	Name        string            `json:"name"`
	Status      string            `json:"status,omitempty"`
	Replicas    string            `json:"replicas,omitempty"`
	Age         string            `json:"age"`
	Node        string            `json:"node,omitempty"`
	Labels      map[string]string `json:"labels,omitempty"`
	Relevance   float64           `json:"relevance"`
	MatchReason string            `json:"match_reason"`
}

// ====================================================================
// kube_health response
// ====================================================================

// HealthResponse is the top-level response for kube_health.
type HealthResponse struct {
	Scope              string              `json:"scope"`
	Overall            string              `json:"overall"`
	Summary            HealthSummary       `json:"summary"`
	UnhealthyResources []UnhealthyResource `json:"unhealthy_resources,omitempty"`
	RecentErrorEvents  []ErrorEvent        `json:"recent_error_events,omitempty"`
	NodeConditions     []NodeCondition     `json:"node_conditions,omitempty"`
}

// HealthSummary is the counts portion of the health response.
type HealthSummary struct {
	Nodes         ResourceCount `json:"nodes"`
	Pods          PodCount      `json:"pods"`
	PVCs          ResourceCount `json:"pvcs"`
	JobsFailed24h int           `json:"jobs_failed_24h"`
}

// ResourceCount is a simple ready/total count.
type ResourceCount struct {
	Ready int `json:"ready"`
	Total int `json:"total"`
}

// PodCount extends ResourceCount with an unhealthy count.
type PodCount struct {
	Running   int `json:"running"`
	Unhealthy int `json:"unhealthy"`
	Total     int `json:"total"`
}

// UnhealthyResource represents a resource in a bad state.
type UnhealthyResource struct {
	Kind      string `json:"kind"`
	Namespace string `json:"namespace"`
	Name      string `json:"name"`
	Status    string `json:"status"`
	Reason    string `json:"reason,omitempty"`
	Restarts  int64  `json:"restarts,omitempty"`
	Age       string `json:"age"`
	Owner     string `json:"owner,omitempty"`
}

// ErrorEvent represents a Warning event from the cluster.
type ErrorEvent struct {
	Type      string `json:"type"`
	Reason    string `json:"reason"`
	Object    string `json:"object"`
	Namespace string `json:"namespace"`
	Message   string `json:"message"`
	Age       string `json:"age"`
	Count     int32  `json:"count"`
}

// NodeCondition represents a node with a non-Ready condition.
type NodeCondition struct {
	Name      string `json:"name"`
	Status    string `json:"status"`
	Condition string `json:"condition,omitempty"`
	Message   string `json:"message,omitempty"`
}

// ====================================================================
// kube_inspect response
// ====================================================================

// InspectResponse is the top-level response for kube_inspect.
type InspectResponse struct {
	Resource         InspectResource   `json:"resource"`
	Status           InspectStatus     `json:"status"`
	OwnerChain       []OwnerRef        `json:"owner_chain,omitempty"`
	Events           []InspectEvent    `json:"events,omitempty"`
	Logs             *InspectLogs      `json:"logs,omitempty"`
	RelatedResources []RelatedResource `json:"related_resources,omitempty"`
}

// InspectResource is the basic resource metadata section.
type InspectResource struct {
	Kind        string            `json:"kind"`
	Namespace   string            `json:"namespace,omitempty"`
	Name        string            `json:"name"`
	Created     string            `json:"created"`
	Labels      map[string]string `json:"labels,omitempty"`
	Annotations map[string]string `json:"annotations,omitempty"`
}

// InspectStatus is the status section of an inspected resource.
type InspectStatus struct {
	Phase      string             `json:"phase,omitempty"`
	Conditions []InspectCondition `json:"conditions,omitempty"`
	Containers []ContainerStatus  `json:"containers,omitempty"`
	Replicas   string             `json:"replicas,omitempty"`
	Spec       map[string]any     `json:"spec,omitempty"`
}

// InspectCondition is a single status condition.
type InspectCondition struct {
	Type    string `json:"type"`
	Status  string `json:"status"`
	Reason  string `json:"reason,omitempty"`
	Message string `json:"message,omitempty"`
}

// ContainerStatus holds container state info for pods.
type ContainerStatus struct {
	Name            string           `json:"name"`
	Image           string           `json:"image"`
	State           string           `json:"state"`
	Reason          string           `json:"reason,omitempty"`
	Restarts        int64            `json:"restarts"`
	LastTermination *TerminationInfo `json:"last_termination,omitempty"`
}

// TerminationInfo holds details about the last container termination.
type TerminationInfo struct {
	Reason   string `json:"reason"`
	ExitCode int64  `json:"exit_code"`
}

// InspectEvent is a simplified event for the inspect response.
type InspectEvent struct {
	Type    string `json:"type"`
	Reason  string `json:"reason"`
	Message string `json:"message"`
	Age     string `json:"age"`
	Count   int32  `json:"count"`
}

// InspectLogs holds log data for pod/job inspection.
type InspectLogs struct {
	Current         string `json:"current,omitempty"`
	Previous        string `json:"previous,omitempty"`
	Truncated       bool   `json:"truncated"`
	TotalLines      int    `json:"total_lines,omitempty"`
	ErrorLinesShown bool   `json:"error_lines_shown"`
}

// RelatedResource represents a resource related to the inspected one.
type RelatedResource struct {
	Kind      string   `json:"kind"`
	Name      string   `json:"name"`
	Namespace string   `json:"namespace,omitempty"`
	Type      string   `json:"type,omitempty"`  // For Services: ClusterIP, NodePort, etc.
	Ports     []string `json:"ports,omitempty"` // For Services
}

// ====================================================================
// kube_topology response
// ====================================================================

// TopologyResponse is the top-level response for kube_topology.
type TopologyResponse struct {
	Root    TopologyNode      `json:"root"`
	Tree    []TopologyNode    `json:"tree,omitempty"`
	Network []NetworkResource `json:"network,omitempty"`
	Storage []StorageResource `json:"storage,omitempty"`
	Config  []ConfigResource  `json:"config,omitempty"`
}

// TopologyNode is a node in the topology tree.
type TopologyNode struct {
	Kind     string         `json:"kind"`
	Name     string         `json:"name"`
	Status   string         `json:"status,omitempty"`
	Replicas string         `json:"replicas,omitempty"`
	Node     string         `json:"node,omitempty"`
	Children []TopologyNode `json:"children,omitempty"`
}

// NetworkResource represents a network-related resource in the topology.
type NetworkResource struct {
	Kind     string   `json:"kind"`
	Name     string   `json:"name"`
	Type     string   `json:"type,omitempty"` // ClusterIP, NodePort, LoadBalancer
	Selector string   `json:"selector,omitempty"`
	Ports    []string `json:"ports,omitempty"`
	Host     string   `json:"host,omitempty"` // For Ingress
	Path     string   `json:"path,omitempty"` // For Ingress
	TLS      bool     `json:"tls,omitempty"`  // For Ingress
}

// StorageResource represents a storage resource in the topology.
type StorageResource struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	Status       string `json:"status,omitempty"`
	Size         string `json:"size,omitempty"`
	StorageClass string `json:"storage_class,omitempty"`
}

// ConfigResource represents a config resource in the topology.
type ConfigResource struct {
	Kind         string `json:"kind"`
	Name         string `json:"name"`
	ReferencedBy string `json:"referenced_by,omitempty"`
}

// ====================================================================
// kube_diff response
// ====================================================================

// DiffResponse is the top-level response for kube_diff.
type DiffResponse struct {
	Resource  string     `json:"resource"`
	Namespace string     `json:"namespace"`
	Name      string     `json:"name"`
	Drifted   bool       `json:"drifted"`
	Changes   []DiffItem `json:"changes,omitempty"`
	Source    string     `json:"source,omitempty"`
}

// DiffItem represents a single field difference.
type DiffItem struct {
	Path     string `json:"path"`
	Expected string `json:"expected,omitempty"`
	Actual   string `json:"actual,omitempty"`
	Type     string `json:"type"` // "added", "removed", "changed"
}

// ====================================================================
// kube_logs response
// ====================================================================

// LogsResponse is the enhanced response for kube_logs.
type LogsResponse struct {
	Pod          string `json:"pod"`
	Namespace    string `json:"namespace"`
	Container    string `json:"container,omitempty"`
	CrashLooping bool   `json:"crash_looping"`
	CurrentLogs  string `json:"current_logs,omitempty"`
	PreviousLogs string `json:"previous_logs,omitempty"`
	ErrorLines   string `json:"error_lines,omitempty"`
	TotalLines   int    `json:"total_lines"`
	Truncated    bool   `json:"truncated"`
	Restarts     int64  `json:"restarts"`
}

// ====================================================================
// Owner chain (shared between inspect and topology)
// ====================================================================

// OwnerRef represents an entry in the owner reference chain.
type OwnerRef struct {
	Kind      string `json:"kind"`
	Name      string `json:"name"`
	Namespace string `json:"namespace,omitempty"`
	Replicas  string `json:"replicas,omitempty"`
}

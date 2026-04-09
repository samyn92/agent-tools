/*
fuzzy.go — Fuzzy matching engine for Kubernetes resources.

Matches a user query against resource names, labels, annotations, and status.
Supports status keywords like "failing", "broken", "unhealthy" that map to
Kubernetes status conditions.

No external dependency — uses substring matching, Levenshtein distance, and
keyword mapping for relevance scoring.
*/
package main

import (
	"strings"
	"unicode/utf8"

	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
)

// FuzzyMatch represents a single search result with relevance scoring.
type FuzzyMatch struct {
	Object      *unstructured.Unstructured
	Kind        string
	Score       float64
	MatchReason string
}

// FuzzyOpts configures the fuzzy search behavior.
type FuzzyOpts struct {
	Kind       string // Filter to a specific resource kind (optional)
	Namespace  string // Filter to a specific namespace (optional)
	Status     string // Filter by status keyword (optional)
	MaxResults int    // Maximum results to return (default 20)
}

// statusKeywords maps user-friendly status terms to Kubernetes status strings.
var statusKeywords = map[string][]string{
	"failing":     {"CrashLoopBackOff", "Error", "Failed", "ImagePullBackOff", "ErrImagePull", "OOMKilled", "RunContainerError"},
	"broken":      {"CrashLoopBackOff", "Error", "Failed", "OOMKilled", "RunContainerError"},
	"unhealthy":   {"CrashLoopBackOff", "ImagePullBackOff", "Pending", "Unknown", "OOMKilled", "ErrImagePull", "RunContainerError", "NotReady"},
	"pending":     {"Pending", "ContainerCreating", "PodInitializing"},
	"oom":         {"OOMKilled"},
	"crash":       {"CrashLoopBackOff"},
	"crashloop":   {"CrashLoopBackOff"},
	"imagepull":   {"ImagePullBackOff", "ErrImagePull"},
	"error":       {"Error", "Failed", "CrashLoopBackOff", "OOMKilled"},
	"terminating": {"Terminating"},
	"evicted":     {"Evicted"},
	"completed":   {"Succeeded", "Complete"},
	"running":     {"Running"},
	"ready":       {"Ready", "Available"},
}

// fuzzyMatchObject scores how well a query matches a Kubernetes object.
// Returns nil if the object does not match at all.
func fuzzyMatchObject(query string, obj *unstructured.Unstructured, kind string) *FuzzyMatch {
	queryLower := strings.ToLower(query)
	name := obj.GetName()
	nameLower := strings.ToLower(name)
	namespace := obj.GetNamespace()

	var bestScore float64
	var bestReason string

	// 1. Exact name match (highest score)
	if nameLower == queryLower {
		return &FuzzyMatch{
			Object:      obj,
			Kind:        kind,
			Score:       1.0,
			MatchReason: "exact name match",
		}
	}

	// 2. Name contains query (high score)
	if strings.Contains(nameLower, queryLower) {
		// Score based on how much of the name the query covers
		coverage := float64(utf8.RuneCountInString(query)) / float64(utf8.RuneCountInString(name))
		score := 0.7 + (coverage * 0.25) // 0.7-0.95 range
		if score > bestScore {
			bestScore = score
			bestReason = "name contains '" + query + "'"
		}
	}

	// 3. Query contains name (e.g. query "nginx-deployment-abc" matches resource "nginx")
	if strings.Contains(queryLower, nameLower) && len(nameLower) > 2 {
		score := 0.6
		if score > bestScore {
			bestScore = score
			bestReason = "query contains resource name"
		}
	}

	// 4. Name prefix match
	if strings.HasPrefix(nameLower, queryLower) {
		score := 0.85
		if score > bestScore {
			bestScore = score
			bestReason = "name starts with '" + query + "'"
		}
	}

	// 5. Namespace/name combined match (e.g. "agents/worker")
	if strings.Contains(queryLower, "/") {
		parts := strings.SplitN(queryLower, "/", 2)
		nsQuery, nameQuery := parts[0], parts[1]
		if strings.Contains(strings.ToLower(namespace), nsQuery) &&
			strings.Contains(nameLower, nameQuery) {
			score := 0.9
			if score > bestScore {
				bestScore = score
				bestReason = "namespace/name match"
			}
		}
	}

	// 6. Label value match
	for key, val := range obj.GetLabels() {
		valLower := strings.ToLower(val)
		if strings.Contains(valLower, queryLower) || strings.Contains(queryLower, valLower) {
			score := 0.65
			if score > bestScore {
				bestScore = score
				bestReason = "label " + key + "=" + val + " matches"
			}
		}
	}

	// 7. Status keyword match
	if statusTerms, ok := statusKeywords[queryLower]; ok {
		resourceStatus := getResourceStatus(obj)
		for _, term := range statusTerms {
			if strings.EqualFold(resourceStatus, term) {
				score := 0.85
				if score > bestScore {
					bestScore = score
					bestReason = "status '" + resourceStatus + "' matches keyword '" + query + "'"
				}
				break
			}
		}
	}

	// 8. Fuzzy distance match (for typos) — only if no match yet and query is long enough
	if bestScore == 0 && len(queryLower) >= 3 {
		dist := levenshtein(queryLower, nameLower)
		maxDist := len(queryLower) / 3 // Allow ~33% edit distance
		if maxDist < 1 {
			maxDist = 1
		}
		if dist <= maxDist {
			score := 0.4 * (1.0 - float64(dist)/float64(len(queryLower)))
			if score > bestScore {
				bestScore = score
				bestReason = "fuzzy name match (edit distance " + strings.Repeat(".", dist) + ")"
			}
		}

		// Also try fuzzy matching against name segments (split by "-")
		nameSegments := strings.Split(nameLower, "-")
		for _, seg := range nameSegments {
			if len(seg) < 3 {
				continue
			}
			segDist := levenshtein(queryLower, seg)
			if segDist <= maxDist {
				score := 0.5 * (1.0 - float64(segDist)/float64(len(queryLower)))
				if score > bestScore {
					bestScore = score
					bestReason = "fuzzy segment match on '" + seg + "'"
				}
			}
		}
	}

	if bestScore == 0 {
		return nil
	}

	return &FuzzyMatch{
		Object:      obj,
		Kind:        kind,
		Score:       bestScore,
		MatchReason: bestReason,
	}
}

// matchesStatusFilter returns true if the object's status matches the given status filter.
func matchesStatusFilter(obj *unstructured.Unstructured, statusFilter string) bool {
	if statusFilter == "" {
		return true
	}

	filterLower := strings.ToLower(statusFilter)
	resourceStatus := strings.ToLower(getResourceStatus(obj))

	// Direct status match
	if strings.Contains(resourceStatus, filterLower) {
		return true
	}

	// Keyword match
	if statusTerms, ok := statusKeywords[filterLower]; ok {
		for _, term := range statusTerms {
			if strings.EqualFold(resourceStatus, term) {
				return true
			}
		}
	}

	return false
}

// levenshtein computes the Levenshtein edit distance between two strings.
func levenshtein(a, b string) int {
	la := utf8.RuneCountInString(a)
	lb := utf8.RuneCountInString(b)

	if la == 0 {
		return lb
	}
	if lb == 0 {
		return la
	}

	ra := []rune(a)
	rb := []rune(b)

	// Use two rows instead of full matrix for memory efficiency
	prev := make([]int, lb+1)
	curr := make([]int, lb+1)

	for j := 0; j <= lb; j++ {
		prev[j] = j
	}

	for i := 1; i <= la; i++ {
		curr[0] = i
		for j := 1; j <= lb; j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			curr[j] = min(
				prev[j]+1,      // deletion
				curr[j-1]+1,    // insertion
				prev[j-1]+cost, // substitution
			)
		}
		prev, curr = curr, prev
	}

	return prev[lb]
}

func min(a, b, c int) int {
	if a < b {
		if a < c {
			return a
		}
		return c
	}
	if b < c {
		return b
	}
	return c
}

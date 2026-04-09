package oci

import (
	"fmt"
	"strings"
)

// Reference holds parsed OCI reference components.
type Reference struct {
	Registry   string // e.g., "ghcr.io"
	Repository string // e.g., "ghcr.io/myorg/agent-tools/kubernetes"
	Tag        string // e.g., "0.1.0"
}

// ParseReference parses a full OCI reference like "ghcr.io/myorg/repo:tag".
// If no tag is specified, defaults to "latest".
func ParseReference(ref string) (Reference, error) {
	tag := "latest"
	repo := ref
	if idx := strings.LastIndex(ref, ":"); idx > 0 {
		afterColon := ref[idx+1:]
		if !strings.Contains(afterColon, "/") {
			tag = afterColon
			repo = ref[:idx]
		}
	}

	parts := strings.SplitN(repo, "/", 2)
	if len(parts) < 2 {
		return Reference{}, fmt.Errorf("invalid reference %q: must be in format registry/repo[:tag]", ref)
	}

	return Reference{
		Registry:   parts[0],
		Repository: repo,
		Tag:        tag,
	}, nil
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"sync"
)

// registryMap holds sourceRef name → OCI base URL mappings.
// Loaded once from HELM_REGISTRIES env var (JSON object).
var (
	registryMap  map[string]string
	registryOnce sync.Once
)

func loadRegistries() map[string]string {
	registryOnce.Do(func() {
		registryMap = make(map[string]string)
		raw := os.Getenv("HELM_REGISTRIES")
		if raw == "" {
			log.Warn("HELM_REGISTRIES env var not set — chart resolution will fail")
			return
		}
		if err := json.Unmarshal([]byte(raw), &registryMap); err != nil {
			log.Error("failed to parse HELM_REGISTRIES", "error", err, "raw", raw)
			return
		}
		log.Info("loaded registry mappings", "count", len(registryMap))
		for k, v := range registryMap {
			log.Info("  registry", "sourceRef", k, "url", v)
		}
	})
	return registryMap
}

// resolveChartURL maps a Flux HelmRepository sourceRef name + chart name
// to a full OCI chart URL.
//
// Example: resolveChartURL("mavenir-oci", "t5g-smf")
//
//	→ "oci://harbor.das-schiff.telekom.de/mavenir/t5g-smf"
func resolveChartURL(sourceRef, chartName string) (string, error) {
	regs := loadRegistries()
	baseURL, ok := regs[sourceRef]
	if !ok {
		known := make([]string, 0, len(regs))
		for k := range regs {
			known = append(known, k)
		}
		return "", fmt.Errorf("unknown sourceRef %q — known registries: %s", sourceRef, strings.Join(known, ", "))
	}
	// Ensure no trailing slash
	baseURL = strings.TrimRight(baseURL, "/")
	return baseURL + "/" + chartName, nil
}

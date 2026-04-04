package piagent

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"github.com/opencontainers/go-digest"
	"oras.land/oras-go/v2/registry/remote/auth"
)

// digestOf computes the SHA-256 digest of data.
func digestOf(data []byte) digest.Digest {
	return digest.FromBytes(data)
}

// encodeJSON marshals v to JSON bytes.
func encodeJSON(v any) ([]byte, error) {
	return json.Marshal(v)
}

// loadDockerCredentials attempts to read credentials from the Docker config file.
// It checks ~/.docker/config.json for stored credentials.
// Returns empty credentials if nothing is found.
func loadDockerCredentials(registry string) auth.Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return auth.EmptyCredential
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return auth.EmptyCredential
	}

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return auth.EmptyCredential
	}

	// Look for matching auth entry
	for host, entry := range config.Auths {
		if matchesRegistry(host, registry) {
			return decodeDockerAuth(entry.Auth)
		}
	}

	return auth.EmptyCredential
}

// dockerConfig represents the Docker config.json structure.
type dockerConfig struct {
	Auths map[string]dockerAuthEntry `json:"auths"`
}

// dockerAuthEntry represents a single auth entry in Docker config.
type dockerAuthEntry struct {
	Auth string `json:"auth"`
}

// matchesRegistry checks if a Docker config host matches the target registry.
func matchesRegistry(host, registry string) bool {
	// Normalize: strip https:// prefix and trailing slashes
	host = strings.TrimPrefix(host, "https://")
	host = strings.TrimPrefix(host, "http://")
	host = strings.TrimSuffix(host, "/")

	return host == registry
}

// decodeDockerAuth decodes a base64-encoded "user:password" auth string.
func decodeDockerAuth(encoded string) auth.Credential {
	if encoded == "" {
		return auth.EmptyCredential
	}

	decoded, err := base64.StdEncoding.DecodeString(encoded)
	if err != nil {
		return auth.EmptyCredential
	}

	parts := strings.SplitN(string(decoded), ":", 2)
	if len(parts) != 2 {
		return auth.EmptyCredential
	}

	return auth.Credential{
		Username: parts[0],
		Password: parts[1],
	}
}

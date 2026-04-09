package oci

import (
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
)

// LoadDockerCredentials attempts to read credentials from ~/.docker/config.json
// for the given registry. Returns EmptyCredential if no credentials are found.
func LoadDockerCredentials(registry string) auth.Credential {
	home, err := os.UserHomeDir()
	if err != nil {
		return auth.EmptyCredential
	}

	configPath := filepath.Join(home, ".docker", "config.json")
	data, err := os.ReadFile(configPath)
	if err != nil {
		return auth.EmptyCredential
	}

	var config struct {
		Auths map[string]struct {
			Auth string `json:"auth"`
		} `json:"auths"`
	}
	if err := json.Unmarshal(data, &config); err != nil {
		return auth.EmptyCredential
	}

	for host, entry := range config.Auths {
		h := strings.TrimPrefix(host, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.TrimSuffix(h, "/")
		if h == registry {
			if entry.Auth == "" {
				return auth.EmptyCredential
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
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
	}

	return auth.EmptyCredential
}

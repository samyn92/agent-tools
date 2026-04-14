package oci

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"oras.land/oras-go/v2/registry/remote/auth"
)

// LoadDockerCredentials attempts to read credentials from ~/.docker/config.json
// for the given registry. Supports auths (static), credHelpers (per-registry),
// and credsStore (default helper). Returns EmptyCredential if no credentials are found.
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

	var config dockerConfig
	if err := json.Unmarshal(data, &config); err != nil {
		return auth.EmptyCredential
	}

	// Priority 1: credHelpers — per-registry credential helper
	if helper, ok := config.CredHelpers[registry]; ok {
		if cred := callCredHelper(helper, registry); cred != auth.EmptyCredential {
			return cred
		}
	}

	// Priority 2: auths — static base64-encoded credentials
	for host, entry := range config.Auths {
		h := strings.TrimPrefix(host, "https://")
		h = strings.TrimPrefix(h, "http://")
		h = strings.TrimSuffix(h, "/")
		if h == registry {
			if entry.Auth == "" {
				continue
			}
			decoded, err := base64.StdEncoding.DecodeString(entry.Auth)
			if err != nil {
				continue
			}
			parts := strings.SplitN(string(decoded), ":", 2)
			if len(parts) != 2 {
				continue
			}
			return auth.Credential{
				Username: parts[0],
				Password: parts[1],
			}
		}
	}

	// Priority 3: credsStore — default credential helper for all registries
	if config.CredsStore != "" {
		if cred := callCredHelper(config.CredsStore, registry); cred != auth.EmptyCredential {
			return cred
		}
	}

	return auth.EmptyCredential
}

// dockerConfig represents the relevant fields of ~/.docker/config.json.
type dockerConfig struct {
	Auths map[string]struct {
		Auth string `json:"auth"`
	} `json:"auths"`
	CredHelpers map[string]string `json:"credHelpers"`
	CredsStore  string            `json:"credsStore"`
}

// callCredHelper invokes docker-credential-<helper> get and parses the response.
func callCredHelper(helper, registry string) auth.Credential {
	helperBin := fmt.Sprintf("docker-credential-%s", helper)
	cmd := exec.Command(helperBin, "get")
	cmd.Stdin = strings.NewReader(registry)
	out, err := cmd.Output()
	if err != nil {
		return auth.EmptyCredential
	}

	var resp struct {
		Username string `json:"Username"`
		Secret   string `json:"Secret"`
	}
	if err := json.Unmarshal(out, &resp); err != nil {
		return auth.EmptyCredential
	}
	if resp.Username == "" && resp.Secret == "" {
		return auth.EmptyCredential
	}

	return auth.Credential{
		Username: resp.Username,
		Password: resp.Secret,
	}
}

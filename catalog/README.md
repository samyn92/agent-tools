# Tool Catalog

This directory contains **tool package definitions** for building tool container images.

These are **NOT** CRDs to be applied to Kubernetes. They define:
- What CLI binary to install
- Embedded deny patterns (baked into the image for security)
- Environment variables the tool needs

## How It Works

1. Each tool package defines a CLI tool (kubectl, gh, terraform, etc.)
2. The `cmd/cli` tool builds images from these definitions using the capability-gateway binary
3. Deny patterns in the package are **immutable** - they can't be overridden by Agent capabilities
4. This provides a security baseline that users can't weaken

## Usage

Tool images built from this catalog are referenced in Capability CRDs:

```yaml
# 1. Define a Capability that uses a catalog tool image
apiVersion: agents.io/v1alpha1
kind: Capability
metadata:
  name: kubectl-readonly
  namespace: agents
spec:
  type: Container
  description: Read-only Kubernetes access
  container:
    image: docker.io/library/tool-kubectl:v1.30.0  # Built from catalog/kubectl
    serviceAccountName: kubectl-readonly
    commandPrefix: "kubectl "
    containerType: kubernetes
  permissions:
    allow:
      - "get *"
      - "describe *"
    deny:
      - "delete *"   # These stack with embedded deny patterns from the image

---
# 2. Reference the Capability from your Agent
apiVersion: agents.io/v1alpha1
kind: Agent
metadata:
  name: my-agent
  namespace: agents
spec:
  capabilityRefs:
    - name: kubectl-readonly
```

## Package Structure

```yaml
apiVersion: tools.agents.io/v1
kind: ToolPackage
metadata:
  name: kubectl
  version: "1.30.0"
  description: Kubernetes command-line tool

cli:
  binary: kubectl
  download:
    url: https://dl.k8s.io/release/v1.30.0/bin/linux/amd64/kubectl

# These deny patterns are EMBEDDED in the image and cannot be overridden
deny:
  - "kubectl delete namespace *"
  - "kubectl exec * -n kube-system *"
  - "*--force*"

env:
  - name: KUBECONFIG
    description: Path to kubeconfig file
```

## Available Tools

| Tool | Description | Catalog Path |
|------|-------------|-------------|
| `kubectl` | Kubernetes CLI | `catalog/kubectl/tool.yaml` |
| `github-cli` | GitHub CLI (gh) | `catalog/github-cli/tool.yaml` |
| `aws-cli` | AWS CLI v2 | `catalog/aws-cli/tool.yaml` |
| `terraform` | Terraform CLI | `catalog/terraform/tool.yaml` |

## Building Tool Images

```bash
# Build a specific tool from its manifest
just build-tool catalog/kubectl/tool.yaml docker.io/library/tool-kubectl:v1.30.0

# Build and push
just build-tool-push catalog/kubectl/tool.yaml ghcr.io/your-org/tool-kubectl:v1.30.0

# Shortcut targets for common tools
just build-tool-kubectl
just build-tool-gh
```

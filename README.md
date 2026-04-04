# agent-tools

Build-time tooling for creating secure, policy-enforced container images that AI agents use as capabilities.

This repository produces **tool images** (kubectl, terraform, gh, etc.) with embedded security policies. It is the build-time counterpart to the [agent-operator](https://github.com/samyn92/agent-operator), which handles runtime orchestration.

## Architecture Overview

```
                        BUILD TIME                              DEPLOY / RUNTIME
                    (this repository)                         (agent-operator repo)

  ToolPackage (tool.yaml)           Capability CRD              Agent CRD
  ┌──────────────────────┐    ┌──────────────────────┐   ┌──────────────────┐
  │ deny:                │    │ permissions:          │   │ capabilityRefs:  │
  │  - "delete namespace"│    │   allow:              │   │  - kubectl-ro    │
  │  - "*--force*"       │    │     - "get *"         │   │  - gh-tool       │
  └──────────┬───────────┘    │   deny:               │   └────────┬─────────┘
             │                │     - "delete *"      │            │
             │                └──────────┬────────────┘            │
             │                           │                         │
    agent-tools build             agent-operator             agent-operator
             │                  (reconcile Capability)      (reconcile Agent)
             ▼                           │                         │
    Container Image                      ▼                         ▼
    ┌──────────────────┐          Merged deny list          Pod with sidecars
    │ /usr/local/bin/  │       (image + CRD patterns)      ┌────────────────┐
    │   kubectl        │                │                   │ init container │
    │ /etc/tool/       │                │                   │  copies gateway│
    │   deny.txt  ─────┼────────────────┘                   ├────────────────┤
    └──────────────────┘                                    │ sidecar        │
                                                            │  entrypoint:   │
                                                            │  capability-   │
                                                            │    gateway     │
                                                            │  reads deny.txt│
                                                            │  + CRD deny    │
                                                            │  executes CLI  │
                                                            └────────────────┘
```

## How Deny Patterns Work

Deny patterns are the core security mechanism that prevents AI agents from running dangerous commands. They are enforced at **two layers** that stack together in a defense-in-depth model.

### Layer 1: Image-Embedded Deny Patterns (this repo)

Each ToolPackage manifest (`tool.yaml`) defines deny patterns that represent hard security boundaries set by the **tool author**:

```yaml
# catalog/kubectl/tool.yaml
deny:
  - "kubectl delete namespace *"
  - "kubectl exec * -n kube-system *"
  - "*--force*"
```

At build time, these patterns are written to `/etc/tool/deny.txt` inside the container image. They are **immutable** -- no one deploying the tool can weaken or remove them.

### Layer 2: Capability CRD Deny Patterns (agent-operator repo)

When deploying a tool, platform teams create a `Capability` CRD that can add **additional** restrictions:

```yaml
apiVersion: agents.io/v1alpha1
kind: Capability
metadata:
  name: kubectl-readonly
spec:
  container:
    image: tool-kubectl:v1.30.0
  permissions:
    allow:
      - "get *"
      - "describe *"
    deny:
      - "delete *"     # Additive -- stacks on top of image deny patterns
```

These CRD-level deny patterns **can only add restrictions, never remove image-level ones**. This means:

- Tool authors set the security floor (nobody can `kubectl delete namespace *`)
- Platform teams raise the floor further per-deployment (e.g., also block `get secrets`)
- AI agents operate within the intersection of what's allowed

### Enforcement: capability-gateway

Neither this repo nor the tool images contain the enforcement binary. The `capability-gateway` binary lives in the `agent-operator` repo and is **injected at runtime** by the operator:

1. The operator creates an **init container** that copies `capability-gateway` into a shared volume
2. Each tool sidecar's entrypoint is overridden to run `capability-gateway`
3. The gateway reads `/etc/tool/deny.txt` (from the image) and CRD-level deny patterns (from the operator)
4. Every command is evaluated against the merged deny list before the actual CLI binary executes

This decouples tool image versions from gateway versions -- gateway updates don't require rebuilding tool images.

## Repository Structure

```
agent-tools/
  catalog/              # ToolPackage manifests (tool.yaml per tool)
    kubectl/
    terraform/
    helm/
    git/
    github-cli/
    gitlab-cli/
    aws-cli/
  cmd/cli/              # CLI tool for building tool images
  pkg/
    toolpackage/        # ToolPackage parsing, Dockerfile generation, image building
    cmdvalidator/       # Shell metacharacter validation
```

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

Or use the CLI directly:

```bash
go run ./cmd/cli tool build --manifest catalog/kubectl/tool.yaml --tag tool-kubectl:v1.30.0
```

## Available Tools

| Tool | Description | Deny Patterns |
|------|-------------|---------------|
| `kubectl` | Kubernetes CLI | ~35 (cluster-destructive ops, exec into system namespaces, raw API, self-modification) |
| `terraform` | Terraform CLI | ~11 (destroy, auto-approve, state manipulation) |
| `helm` | Helm package manager | ~12 (install/upgrade/uninstall, repo/plugin management) |
| `git` | Git version control | ~17 (force-push, hard-reset, credential/config manipulation) |
| `github-cli` | GitHub CLI (gh) | ~8 (auth, repo delete, secrets, org management) |
| `gitlab-cli` | GitLab CLI (glab) | ~7 (auth, project delete, SSH keys, variables) |
| `aws-cli` | AWS CLI v2 | ~19 (IAM, STS, KMS, secrets, terminate instances) |

See [catalog/README.md](catalog/README.md) for details on the package format and Capability CRD integration.

# agent-tools

[![CI](https://github.com/samyn92/agent-tools/actions/workflows/ci.yaml/badge.svg)](https://github.com/samyn92/agent-tools/actions/workflows/ci.yaml)
[![License](https://img.shields.io/badge/license-Apache%202.0-blue.svg)](LICENSE)
[![Go](https://img.shields.io/badge/Go-1.25-00ADD8.svg)](https://go.dev/)

OCI-packaged MCP tool servers for the [AgentOps platform](https://samyn92.github.io/agentops/). This repository contains **7 MCP tool servers** that AI agents consume at runtime, a shared **mcputil SDK** that makes OpenTelemetry tracing structural and automatic, and a **CLI** for packaging and pushing servers as OCI artifacts. All servers are compiled Go binaries implementing the MCP stdio transport, compatible with the Fantasy runtime and any MCP-aware client.

---

## Table of Contents

- [Architecture](#architecture)
- [mcputil SDK](#mcputil-sdk)
- [MCP Servers](#mcp-servers)
- [CLI](#cli)
- [OCI Artifact Format](#oci-artifact-format)
- [Project Structure](#project-structure)
- [Development](#development)
- [CI/CD](#cicd)
- [Images](#images)
- [Related Projects](#related-projects)
- [License](#license)

## Architecture

```
              BUILD / PUSH TIME                           DEPLOY / RUNTIME
             (this repository)                          (agentops-core operator)

 MCP Server Source                  OCI Registry                Agent Pod
 ┌─────────────────────┐     ┌──────────────────────┐    ┌──────────────────────┐
 │ servers/kubectl/     │     │ ghcr.io/samyn92/     │    │                      │
 │   main.go            │────>│   agent-tools/       │    │  init container      │
 │   manifest.json      │     │   kubectl:0.8.2      │───>│    (crane) pulls OCI │
 │                      │     │                      │    │    extracts to       │
 │ servers/tempo/       │     │   agent-tools/       │    │      /tools/kubectl/ │
 │   main.go            │────>│     tempo:0.8.2      │───>│        bin/mcp-kubectl│
 └─────────────────────┘     └──────────────────────┘    └──────────────────────┘

 All servers link pkg/mcputil — every tool call gets an OTEL span automatically.
```

## mcputil SDK

`servers/pkg/mcputil/` is the shared SDK that all tool servers import. It wraps the MCP Go SDK with structural OpenTelemetry tracing — you cannot register a tool without getting a span.

### Features

- **Session-level root span** — `NewServer()` + `Run()` creates an `mcp.session` span that lives for the entire server lifecycle. All tool spans are children.
- **Automatic tool tracing** — `AddToolTo()` wraps every tool invocation in a `tool.<name>` span with duration, error status, and optional I/O recording.
- **Panic recovery** — handler panics are caught, stack traces recorded as span events, and error results returned to the agent instead of crashing the server.
- **I/O recording opt-in** — `WithInputOutput()` records tool inputs and outputs as span events for mutation/write tools. Output truncated to 2000 chars.
- **Health probes** — `Ready()` / `NotReady()` for liveness/readiness integration.
- **Structured logging** — `Logger()` returns an `*slog.Logger` configured for the server.
- **HTTP and exec helpers** — `DoJSON()` for API calls, `RunCommand()` for CLI wrappers, `K8sClientset()` for in-cluster Kubernetes access — all with span propagation.

### API

```go
// Create a server with session-level tracing
server := mcputil.NewServer("kubectl", "0.8.2", mcputil.WithMode(mode))

// Register a tool — every call gets a "tool.kubectl_get" span
mcputil.AddToolTo(server, "kubectl_get",
    "Get Kubernetes resources",
    handleGet,
)

// Register a mutation tool with I/O recording
mcputil.AddToolTo(server, "kubectl_apply",
    "Apply manifests",
    handleApply,
    mcputil.WithInputOutput(),
)

// Run with stdio transport — starts mcp.session root span
server.Run(ctx, mcp.NewStdioTransport())
```

### Package Files

| File | Purpose |
|------|---------|
| `init.go` | OTEL TracerProvider + MeterProvider bootstrap (OTLP gRPC) |
| `server.go` | `Server` wrapper with session-level root span |
| `tool.go` | `AddTool` / `AddToolTo` with automatic tracing + panic recovery |
| `http.go` | `DoJSON()` — traced HTTP client for API servers |
| `exec.go` | `RunCommand()` — traced subprocess execution for CLI wrappers |
| `k8s.go` | `K8sClientset()` — in-cluster Kubernetes client |
| `logging.go` | `Logger()` — structured slog logger |
| `result.go` | `TextResult()` / `ErrResult()` — MCP result builders |
| `health.go` | `Ready()` / `NotReady()` — health probe state |

## MCP Servers

All servers are compiled Go binaries named `mcp-{server}` implementing the MCP stdio transport. Each is a separate Go module under `servers/` with its own `go.mod` and a `manifest.json`:

```json
{
  "name": "kubectl",
  "command": "mcp-kubectl",
  "transport": "stdio",
  "description": "Kubernetes kubectl operations with readonly and readwrite modes."
}
```

### Server Catalog

| Server | Binary | Tools | Environment |
|--------|--------|-------|-------------|
| `kubectl` | `mcp-kubectl` | 16 (7 readonly + 9 readwrite) | `MODE=readonly` (default) or `readwrite` |
| `kube-explore` | `mcp-kube-explore` | 8 | In-cluster only |
| `flux` | `mcp-flux` | 15 (11 readonly + 4 readwrite) | `MODE=readonly` (default) or `readwrite` |
| `git` | `mcp-git` | 12 (5 readonly + 7 readwrite) | `MODE=readwrite` (default) |
| `github` | `mcp-github` | 12 | `GITHUB_TOKEN` required |
| `gitlab` | `mcp-gitlab` | 10 | `GITLAB_TOKEN` + `GITLAB_URL` required — **deprecated** (use a GitLab Integration) |
| `tempo` | `mcp-tempo` | 6 | `TEMPO_URL` required |

### Tool Reference

| Server | Tools |
|--------|-------|
| `kubectl` | **readonly:** `kubectl_get`, `kubectl_describe`, `kubectl_logs`, `kubectl_top`, `kubectl_events`, `kubectl_api_resources`, `kubectl_explain` — **readwrite** (MODE=readwrite): + `kubectl_exec`, `kubectl_apply`, `kubectl_delete`, `kubectl_run`, `kubectl_cp`, `kubectl_rollout`, `kubectl_scale`, `kubectl_label`, `kubectl_annotate` |
| `kube-explore` | `kube_find`, `kube_health`, `kube_inspect`, `kube_topology`, `kube_diff`, `kube_logs`, `kube_exec`, `kube_apply` |
| `flux` | **readonly:** `flux_get`, `flux_check`, `flux_stats`, `flux_logs`, `flux_events`, `flux_trace`, `flux_tree`, `flux_diff`, `flux_export`, `flux_debug`, `flux_version` — **readwrite** (MODE=readwrite): + `flux_reconcile`, `flux_suspend`, `flux_resume`, `flux_delete` |
| `git` | **readonly:** `git_status`, `git_diff`, `git_log`, `git_show`, `git_branch_list` — **readwrite** (MODE=readwrite, default): + `git_add`, `git_commit`, `git_push`, `git_pull`, `git_branch`, `git_clone`, `git_clone_or_pull` |
| `github` | `github_get_repo`, `github_list_prs`, `github_get_pr`, `github_get_pr_diff`, `github_create_pr`, `github_add_pr_comment`, `github_list_issues`, `github_get_issue`, `github_add_issue_comment`, `github_list_branches`, `github_get_check_runs`, `github_get_workflow_runs` |
| `gitlab` | `gitlab_get_project`, `gitlab_list_mrs`, `gitlab_get_mr`, `gitlab_get_mr_diff`, `gitlab_create_mr`, `gitlab_add_mr_note`, `gitlab_list_issues`, `gitlab_get_issue`, `gitlab_add_issue_note`, `gitlab_get_pipeline` |
| `tempo` | `tempo_search`, `tempo_get`, `tempo_agent_stats`, `tempo_slow_tools`, `tempo_errors`, `tempo_compare` |

### kube-explore

Intent-based Kubernetes discovery server designed to answer complex questions in a single call.

- **Fuzzy matching** — accepts partial names, label selectors, and status keywords (`failing`, `crash`, `oom`, `pending`, etc.)
- **Parallel scanning** — searches across all namespaces concurrently
- **Relationship traversal** — walks owner references and discovers related resources (Services, Ingresses, PVCs, ConfigMaps)
- **Native AgentOps CRD support** — understands `agents`, `agentruns`, `channels`, and `mcpservers` resources
- **Deep inspection** — `kube_inspect` returns logs, events, owner chain, and related resources in one response
- **Cluster health** — `kube_health` provides a full cluster health snapshot

### kubectl

General-purpose kubectl MCP server with two operating modes:

- **`MODE=readonly`** (default) — safe observability tools only
- **`MODE=readwrite`** — all readonly tools plus mutating operations (exec, apply, delete, scale, etc.)

Shells out to the real `kubectl` binary — requires `kubectl` in `$PATH`.

### flux

Flux CD GitOps MCP server with two operating modes:

- **`MODE=readonly`** (default) — observe and diagnose Flux resources
- **`MODE=readwrite`** — all readonly tools plus reconcile, suspend, resume, and delete

Shells out to the real `flux` binary — requires `flux` in `$PATH`.

### git

Git operations MCP server. Default mode is `readwrite` (most agents need to commit/push).

- **`MODE=readonly`** — status, diff, log, show, branch_list only
- **`MODE=readwrite`** (default) — all operations including add, commit, push, pull, clone

### github

GitHub API MCP server. Requires `GITHUB_TOKEN` (or `GH_TOKEN`) environment variable. All operations are API-based (no CLI dependency).

### gitlab

> **Deprecated.** Superseded by the agentops-runtime native `gitlab_*` tools
> (official GitLab Go SDK), auto-enabled from a bound `gitlab-group` /
> `gitlab-project` Integration with read-only mode + project allow-list. Prefer a
> GitLab Integration over binding this AgentTool. Retained for standalone MCP clients.

GitLab API MCP server. Requires `GITLAB_TOKEN` and `GITLAB_URL` environment variables. All operations are API-based (no CLI dependency).

### tempo

Grafana Tempo trace analysis MCP server. Requires `TEMPO_URL` environment variable pointing to the Tempo HTTP API (e.g. `http://tempo.observability.svc:3200`).

- **`tempo_search`** — search traces by service, operation, duration, status
- **`tempo_get`** — get full trace by ID with span tree
- **`tempo_agent_stats`** — aggregate agent execution statistics
- **`tempo_slow_tools`** — find slowest tool invocations
- **`tempo_errors`** — find error traces and patterns
- **`tempo_compare`** — compare two traces side-by-side

## CLI

### Installation

Download a pre-built binary from the [releases page](https://github.com/samyn92/agent-tools/releases), or build from source:

```bash
make build
```

### Usage

```
agent-tools push [directory] -t <oci-ref>
```

| Flag | Short | Description |
|------|-------|-------------|
| `--tag` | `-t` | **Required.** Full OCI reference (e.g. `ghcr.io/samyn92/agent-tools/kubectl:0.8.2`) |
| `--plain-http` | | Use HTTP instead of HTTPS for the registry |

Authentication is loaded from `~/.docker/config.json`.

### Example

```bash
# Build the server binary
cd servers/kubectl
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dist/bin/mcp-kubectl .
cp manifest.json dist/

# Push to registry
agent-tools push ./dist/ -t ghcr.io/samyn92/agent-tools/kubectl:0.8.2
```

Or use the Makefile:

```bash
make build-server SERVER=kubectl
make push-server SERVER=kubectl TAG=0.8.2
```

Pushed artifacts are referenced in AgentTool CRs:

```yaml
apiVersion: agents.agentops.io/v1alpha1
kind: AgentTool
metadata:
  name: kubectl
  namespace: agents
spec:
  category: infrastructure
  description: "Full kubectl wrapper — readonly and readwrite modes"
  oci:
    ref: ghcr.io/samyn92/agent-tools/kubectl:0.8.2
```

## OCI Artifact Format

| Field | Value |
|-------|-------|
| Artifact Type | `application/vnd.agents.io.mcp-tool.v1` |
| Layer Media Type | `application/vnd.agents.io.mcp-tool.code.v1.tar+gzip` |
| Config Media Type | `application/vnd.agents.io.mcp-tool.config.v1+json` |

### Source Validation

Directories must contain:
- `manifest.json` — server metadata (name, command, transport, description)
- `bin/` directory with at least one executable (named `mcp-{server}`)

## Project Structure

```
agent-tools/
  cmd/agent-tools/            # CLI binary
    main.go                   #   Root command + version
    push.go                   #   Push command (validate, package, push)
  internal/oci/               # OCI packaging engine
    pusher.go                 #   Validation, manifest building, ORAS push
    archive.go                #   tar+gzip archive creation
    credentials.go            #   Docker config credential loading
    reference.go              #   OCI reference parsing
  servers/                    # MCP tool servers
    pkg/mcputil/              #   Shared SDK — OTEL tracing, health, helpers
    kube-explore/             #   Intent-based Kubernetes discovery (8 tools)
    kubectl/                  #   kubectl CLI wrapper (16 tools)
    flux/                     #   Flux CD GitOps (15 tools)
    git/                      #   Git operations (12 tools)
    github/                   #   GitHub API (12 tools)
    gitlab/                   #   GitLab API (10 tools)
    tempo/                    #   Tempo trace analysis (6 tools)
  Makefile                    # Build, test, push workflows
  .github/workflows/          # CI + Release pipelines
```

## Development

### Requirements

- Go 1.25+
- Access to an OCI-compliant registry (e.g. `ghcr.io`)
- Docker credentials configured in `~/.docker/config.json`

### Makefile Targets

```bash
make build                              # Build CLI binary
make test                               # Run tests
make vet                                # Run go vet
make fmt                                # Format code

make build-server SERVER=kubectl        # Build a single MCP server
make build-servers                      # Build all MCP servers

make push-server SERVER=kubectl TAG=0.8.2  # Push a single server
make push-servers TAG=0.8.2                # Push all servers

make docker-build SERVER=kubectl        # Build Docker image for a server

make clean                              # Remove binaries and dist/ dirs
```

### Adding a New Server

1. Create a directory under `servers/` with its own `go.mod`
2. Add `servers/pkg/mcputil` as a local dependency:
   ```
   require github.com/samyn92/agent-tools/servers/pkg/mcputil v0.0.0
   replace github.com/samyn92/agent-tools/servers/pkg/mcputil => ../pkg/mcputil
   ```
3. Implement the server using `mcputil`:
   ```go
   package main

   import (
       "context"
       "github.com/modelcontextprotocol/go-sdk/mcp"
       "github.com/samyn92/agent-tools/servers/pkg/mcputil"
   )

   func main() {
       server := mcputil.NewServer("my-server", "0.1.0")

       mcputil.AddToolTo(server, "my_tool",
           "What this tool does",
           func(ctx context.Context, req *mcp.CallToolRequest, in MyInput) (*mcp.CallToolResult, any, error) {
               // implementation
               return mcputil.TextResult("result"), nil, nil
           },
       )

       mcputil.Ready()
       if err := server.Run(context.Background(), mcp.NewStdioTransport()); err != nil {
           mcputil.Logger().Error("server failed", "error", err)
       }
   }
   ```
4. Add a `manifest.json`:
   ```json
   {
     "name": "my-server",
     "command": "mcp-my-server",
     "transport": "stdio",
     "description": "What this server does"
   }
   ```
5. Build and push:
   ```bash
   make build-server SERVER=my-server
   make push-server SERVER=my-server TAG=0.1.0
   ```

## CI/CD

### CI (`.github/workflows/ci.yaml`)

Runs on push/PR to `main` or `dev`:

- **build-and-test** — `go build`, `go vet`, `go test`
- **validate-packages** — checks every `servers/*/` has a `manifest.json`

### Release (`.github/workflows/release.yaml`)

Triggered by `v*` tags:

1. **cli-release** — cross-compiles for linux/darwin amd64/arm64, uploads to GitHub Release
2. **server-packages** — builds each server as `mcp-{server}`, pushes as OCI artifact to `ghcr.io/samyn92/agent-tools/{name}:{version}` (+ `latest`)
3. **server-images** — builds Docker images for servers that bundle upstream CLIs (kubectl, flux, kube-explore)

Matrix strategy uses `fail-fast: false` so a single server failure does not cancel the rest.

## Images

### OCI Artifacts (all servers)

| Artifact | Source |
|:--|:--|
| `ghcr.io/samyn92/agent-tools/kubectl` | `servers/kubectl/` |
| `ghcr.io/samyn92/agent-tools/kube-explore` | `servers/kube-explore/` |
| `ghcr.io/samyn92/agent-tools/flux` | `servers/flux/` |
| `ghcr.io/samyn92/agent-tools/git` | `servers/git/` |
| `ghcr.io/samyn92/agent-tools/github` | `servers/github/` |
| `ghcr.io/samyn92/agent-tools/gitlab` | `servers/gitlab/` |
| `ghcr.io/samyn92/agent-tools/tempo` | `servers/tempo/` |

### Container Images (servers bundling upstream CLIs)

| Image | Source | Bundles |
|:--|:--|:--|
| `ghcr.io/samyn92/agent-tools/kubectl` | `servers/kubectl/Dockerfile` | `kubectl` CLI |
| `ghcr.io/samyn92/agent-tools/kube-explore` | `servers/kube-explore/Dockerfile` | In-cluster client (no CLI) |
| `ghcr.io/samyn92/agent-tools/flux` | `servers/flux/Dockerfile` | `flux` CLI |
| `ghcr.io/samyn92/agent-tools/git` | `servers/git/Dockerfile` | `git` CLI |
| `ghcr.io/samyn92/agent-tools/github` | `servers/github/Dockerfile` | API-only (no CLI) |
| `ghcr.io/samyn92/agent-tools/tempo` | `servers/tempo/Dockerfile` | API-only (no CLI) |

## Related Projects

| Repository | Description |
|:--|:--|
| [agentops-core](https://github.com/samyn92/agentops-core) | Kubernetes operator — CRDs, reconcilers, MCP gateway |
| [agentops-runtime](https://github.com/samyn92/agentops-runtime) | Fantasy SDK agent binary — memory, delegation, FEP |
| [agentops-console](https://github.com/samyn92/agentops-console) | Web console — Go BFF + SolidJS PWA |
| [agentops-memory](https://github.com/samyn92/agentops-memory) | Memory service — SQLite + FTS5 BM25 |
| [agentops-platform](https://github.com/samyn92/agentops-platform) | Umbrella Helm chart — one-command deployment |
| [agent-channels](https://github.com/samyn92/agent-channels) | Channel bridge images |
| [agentops](https://github.com/samyn92/agentops) | Documentation site |

## License

Apache License 2.0. See [LICENSE](LICENSE) for details.

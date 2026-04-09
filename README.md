# agent-tools

OCI packaging CLI and MCP tool servers for the AgentOps platform.

This repository contains **MCP tool servers** that AI agents consume at runtime and a **CLI** for packaging and pushing them as OCI artifacts to container registries. All servers are compiled Go binaries implementing the MCP stdio transport, compatible with the Fantasy runtime and any MCP-aware client.

## Architecture

```
              BUILD / PUSH TIME                           DEPLOY / RUNTIME
             (this repository)                          (agentops-core operator)

 MCP Server Source                  OCI Registry                Agent Pod
 ┌─────────────────────┐     ┌──────────────────────┐    ┌──────────────────────┐
 │ servers/kube-explore/│     │ ghcr.io/myorg/       │    │                      │
 │   main.go            │────>│   agent-tools/       │    │  init container      │
 │   manifest.json      │     │   kube-explore:0.2.0 │───>│    pulls OCI layer   │
 │                      │     │                      │    │    extracts to       │
 │ servers/git/         │     │   agent-tools/       │    │      /tools/         │
 │   main.go            │────>│     git:0.2.0        │───>│                      │
 └─────────────────────┘     └──────────────────────┘    └──────────────────────┘
```

## Repository Structure

```
agent-tools/
  cmd/agent-tools/        # CLI binary
    main.go               #   Root command + version
    push.go               #   Push command (validate, package, push)
  internal/oci/           # OCI packaging engine
    pusher.go             #   Validation, manifest building, ORAS push
    archive.go            #   tar+gzip archive creation
    credentials.go        #   Docker config credential loading
    reference.go          #   OCI reference parsing
  servers/                # MCP tool servers
    kube-explore/         #   Intent-based Kubernetes discovery
    git/                  #   Git operations
    github/               #   GitHub API
    gitlab/               #   GitLab API
  Makefile                # Build, test, push workflows
  .github/workflows/      # CI + Release pipelines
```

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
| `--tag` | `-t` | **Required.** Full OCI reference (e.g. `ghcr.io/myorg/agent-tools/kube-explore:0.2.0`) |
| `--plain-http` | | Use HTTP instead of HTTPS for the registry |

Authentication is loaded from `~/.docker/config.json`.

### Example

```bash
# Build the server binary
cd servers/kube-explore
CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o dist/bin/kube-explore .
cp manifest.json dist/

# Push to registry
agent-tools push ./dist/ -t ghcr.io/myorg/agent-tools/kube-explore:0.2.0
```

Or use the Makefile:

```bash
make build-server SERVER=kube-explore
make push-server SERVER=kube-explore TAG=0.2.0
```

Pushed artifacts can be referenced in Agent CRDs:

```yaml
spec:
  toolRefs:
    - name: kube-explore
      ref: ghcr.io/myorg/agent-tools/kube-explore:0.2.0
```

## OCI Artifact Format

| Field | Value |
|-------|-------|
| Artifact Type | `application/vnd.agents.io.mcp-tool.v1` |
| Layer Media Type | `application/vnd.agents.io.mcp-tool.code.v1.tar+gzip` |
| Config Media Type | `application/vnd.agents.io.mcp-tool.config.v1+json` |

### Source Validation

Directories must contain:
- `manifest.json` -- server metadata
- `bin/` directory with at least one executable

## MCP Servers

All servers are compiled Go binaries implementing the MCP stdio transport. Each is a separate Go module with its own `go.mod` and includes a `manifest.json`:

```json
{
  "name": "kube-explore",
  "command": "kube-explore",
  "transport": "stdio",
  "description": "Intent-based Kubernetes discovery & operations"
}
```

| Server | Tools Provided |
|--------|---------------|
| `servers/kube-explore` | `kube_find`, `kube_health`, `kube_inspect`, `kube_topology`, `kube_diff`, `kube_logs`, `kube_exec`, `kube_apply` |
| `servers/git` | `git_status`, `git_diff`, `git_log`, `git_add`, `git_commit`, `git_push`, `git_pull`, `git_branch`, `git_branch_list`, `git_show`, `git_clone`, `git_clone_or_pull` |
| `servers/github` | `github_get_repo`, `github_list_prs`, `github_get_pr`, `github_get_pr_diff`, `github_create_pr`, `github_add_pr_comment`, `github_list_issues`, `github_get_issue`, `github_add_issue_comment`, `github_list_branches`, `github_get_check_runs`, `github_get_workflow_runs` |
| `servers/gitlab` | `gitlab_get_project`, `gitlab_list_mrs`, `gitlab_get_mr`, `gitlab_get_mr_diff`, `gitlab_create_mr`, `gitlab_add_mr_note`, `gitlab_list_issues`, `gitlab_get_issue`, `gitlab_add_issue_note`, `gitlab_get_pipeline` |

### kube-explore

Intent-based Kubernetes discovery server designed to answer complex questions in a single call.

- **Fuzzy matching** -- Accepts partial names, label selectors, and status keywords (`failing`, `crash`, `oom`, `pending`, etc.)
- **Parallel scanning** -- Searches across all namespaces concurrently
- **Relationship traversal** -- Walks owner references and discovers related resources (Services, Ingresses, PVCs, ConfigMaps)
- **Native AgentOps CRD support** -- Understands `agents`, `agentruns`, `channels`, and `mcpservers` resources
- **Deep inspection** -- `kube_inspect` returns logs, events, owner chain, and related resources in one response
- **Cluster health** -- `kube_health` provides a full cluster health snapshot

## Development

### Requirements

- Go 1.26+
- Access to an OCI-compliant registry (e.g. `ghcr.io`)
- Docker credentials configured in `~/.docker/config.json`

### Makefile Targets

```bash
make build                            # Build CLI binary
make test                             # Run tests
make vet                              # Run go vet
make fmt                              # Format code

make build-server SERVER=kube-explore   # Build a single MCP server
make build-servers                      # Build all MCP servers

make push-server SERVER=kube-explore TAG=0.2.0  # Push a single server
make push-servers TAG=0.2.0                      # Push all servers

make docker-build SERVER=kube-explore   # Build Docker image for a server

make clean                            # Remove binaries and dist/ dirs
```

### Adding a New Server

1. Create a directory under `servers/` with its own `go.mod`
2. Implement an MCP server using `github.com/modelcontextprotocol/go-sdk` with stdio transport
3. Add a `manifest.json`:
   ```json
   {
     "name": "my-server",
     "command": "my-server",
     "transport": "stdio",
     "description": "What this server does"
   }
   ```
4. Build and push:
   ```bash
   make build-server SERVER=my-server
   make push-server SERVER=my-server TAG=0.1.0
   ```

## CI/CD

### CI (`.github/workflows/ci.yaml`)

Runs on push/PR to `main` or `dev`:

- **build-and-test** -- `go build`, `go vet`, `go test`
- **validate-packages** -- Checks every `servers/*/` has a `manifest.json`

### Release (`.github/workflows/release.yaml`)

Triggered by `v*` tags:

1. **cli-release** -- Cross-compiles for linux/darwin amd64/arm64, uploads to GitHub Release
2. **server-packages** -- Pushes each `servers/*/` as an OCI artifact to `ghcr.io/{owner}/agent-tools/{name}:{version}` (+ `latest`)

## Images

| Image | Source | Purpose |
|-------|--------|---------|
| `ghcr.io/samyn92/agent-tools/kube-explore` | `servers/kube-explore/Dockerfile` | Kube-explore MCP server |

## Related Repositories

| Repository | Purpose |
|-----------|---------|
| [agentops-core](https://github.com/samyn92/agentops-core) | Kubernetes operator that pulls and mounts these OCI artifacts |
| [agentops-runtime-fantasy](https://github.com/samyn92/agentops-runtime-fantasy) | Fantasy SDK agent runtime (Go, Charm Fantasy SDK) |
| [agent-channels](https://github.com/samyn92/agent-channels) | Channel bridge images (GitLab, webhook, etc.) |
| [agent-console](https://github.com/samyn92/agent-console) | Web console |
| [agent-factory](https://github.com/samyn92/agent-factory) | Helm chart |

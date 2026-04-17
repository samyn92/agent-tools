## agent-tools

OCI-packaged MCP tool servers for the AgentOps platform. 7 servers, 79 tools, shared `mcputil` SDK with automatic OTEL tracing.

## Repository Structure

```
agent-tools/
  cmd/agent-tools/            # OCI push CLI
  internal/oci/               # OCI packaging engine (ORAS-based)
  servers/
    pkg/mcputil/              # Shared SDK — OTEL tracing, health, exec, HTTP, K8s helpers
    kubectl/                  # CLI wrapper (shells out to kubectl binary)
    flux/                     # CLI wrapper (shells out to flux binary)
    kube-explore/             # In-cluster Kubernetes discovery (uses client-go)
    git/                      # CLI wrapper (shells out to git)
    github/                   # API-based (net/http, no CLI dependency)
    gitlab/                   # API-based (net/http, no CLI dependency)
    tempo/                    # API-based (net/http, queries Tempo HTTP API)
  Makefile
  .github/workflows/
    ci.yaml                   # Build, vet, test + manifest.json validation
    release.yaml              # Tag-triggered: OCI artifacts + container images + CLI binaries
```

Each server is its own Go module under `servers/` with a `go.mod` and `manifest.json`. They all import `servers/pkg/mcputil` via a Go `replace` directive.

## Key Dependencies

- `github.com/modelcontextprotocol/go-sdk v0.8.0` — MCP protocol implementation
- `servers/pkg/mcputil` — shared tracing SDK (local replace)
- Go 1.25+ (mcputil and servers use 1.26)

## mcputil SDK (`servers/pkg/mcputil/`)

The shared SDK that all servers MUST use. Makes OTEL tracing structural — you cannot register a tool without getting a span. **Never use the raw `mcp.AddTool()` directly. Always use `mcputil.AddToolTo()`.**

### Core API

```go
// Initialize OTEL tracing (no-op if OTEL_EXPORTER_OTLP_ENDPOINT is unset)
shutdown, _ := mcputil.Init(ctx, "mcp-tool-myserver")
defer shutdown(ctx)

// Create server with session-level root span
server := mcputil.NewServer("myserver", "0.1.0", mcputil.WithMode(mode))

// Register tool — every call gets a "tool.<name>" span automatically
mcputil.AddToolTo(server, "my_tool", "Description", handler)

// For mutation tools — opt-in I/O recording as span events
mcputil.AddToolTo(server, "my_write_tool", "Description", handler, mcputil.WithInputOutput())

// Signal readiness, then run
mcputil.Ready("mcp-tool-myserver")
server.Run(ctx, &mcp.StdioTransport{})
```

### Helpers by Server Pattern

| Pattern | Helper | Use case |
|---------|--------|----------|
| API-based server | `mcputil.TracedHTTP(ctx, method, url, opts...)` | GitHub, GitLab, Tempo |
| CLI wrapper | `mcputil.TracedExec(ctx, binary, args...)` | kubectl, flux, git |
| In-cluster K8s | `mcputil.K8sOp(ctx, verb, resource, ns)` | kube-explore |
| Results | `mcputil.TextResult(text)`, `mcputil.ErrResult(fmt, args...)` | All |
| Logging | `mcputil.Logger()` — slog with trace_id/span_id correlation | All |

### Package Files

| File | Purpose |
|------|---------|
| `init.go` | OTEL TracerProvider bootstrap. No-op when `OTEL_EXPORTER_OTLP_ENDPOINT` unset. |
| `server.go` | `Server` wrapper — session root span `mcp.session` with server metadata |
| `tool.go` | `AddTool`/`AddToolTo` — per-tool tracing + panic recovery |
| `http.go` | `TracedHTTP` — traced HTTP client. Sanitizes URLs (strips query params from spans). |
| `exec.go` | `TracedExec`/`TracedExecWithTimeout` — traced subprocess execution (30s default timeout) |
| `k8s.go` | `K8sOp`/`K8sOpSimple` — thin tracing wrapper (does NOT import client-go) |
| `logging.go` | `Logger`/`LoggerJSON` — slog handlers that inject trace_id/span_id |
| `result.go` | `TextResult`/`ErrResult` — MCP result builders + `truncate` helper |
| `health.go` | `Ready`/`NotReady` — writes `/tmp/<name>.ready` file for health probes |

## Three Server Patterns

### 1. API-based (github, gitlab, tempo)

No external binary dependency. Uses `mcputil.TracedHTTP()` for all API calls. Requires environment variables for auth tokens and base URLs.

```go
body, status, err := mcputil.TracedHTTP(ctx, "GET", apiBase+"/repos/...",
    mcputil.WithHeader("Authorization", "Bearer "+token),
    mcputil.WithHeader("Accept", "application/vnd.github+json"),
)
```

### 2. CLI wrapper (kubectl, flux, git)

Shells out to a real CLI binary using `mcputil.TracedExec()`. The binary must be in `$PATH` at runtime. For OCI artifacts, kubectl and flux binaries are co-bundled in `dist/bin/`.

```go
result := mcputil.TracedExec(ctx, kubectlBin, "get", "pods", "-n", namespace, "-o", "wide")
if result.Err != nil {
    return mcputil.ErrResult("kubectl failed: %s\n%s", result.Err, result.Output), nil, nil
}
return mcputil.TextResult(result.Output), nil, nil
```

### 3. In-cluster Kubernetes client (kube-explore)

Uses `client-go` directly with `mcputil.K8sOp()` for tracing. No kubectl dependency.

```go
ctx, finish := mcputil.K8sOp(ctx, "list", "pods", namespace)
pods, err := clientset.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
finish(len(pods.Items), err)
```

## MODE Environment Variable

Servers with mutable operations use `MODE` to gate tool registration at startup:

| Mode | Behavior |
|------|----------|
| `readonly` (default for kubectl, flux, kube-explore) | Only read/query tools registered |
| `readwrite` (default for git) | All tools registered including mutations |

Check `MODE` at startup and conditionally call `AddToolTo()`. The agent never sees tools that aren't registered.

## Conventions

### Binary Naming

All MCP server binaries MUST be named `mcp-{server}`:
- `mcp-kubectl`, `mcp-flux`, `mcp-git`, `mcp-github`, `mcp-gitlab`, `mcp-tempo`, `mcp-kube-explore`

Bundled upstream CLIs keep their original names: `kubectl`, `flux`.

### manifest.json

Every server directory has a `manifest.json`:

```json
{
  "name": "my-server",
  "command": "mcp-my-server",
  "transport": "stdio",
  "description": "What this server does"
}
```

The `command` field MUST match the binary name (`mcp-{server}`).

### Input Types

Use Go structs with `json` and `jsonschema_description` tags. The MCP SDK auto-generates JSON schemas from these:

```go
type getInput struct {
    Resource  string `json:"resource" jsonschema_description:"Resource type (e.g. pods, deployments)"`
    Name      string `json:"name,omitempty" jsonschema_description:"Resource name (omit to list all)"`
    Namespace string `json:"namespace,omitempty" jsonschema_description:"Namespace"`
}
```

Fields without `omitempty` become `required` in the JSON schema. Use `omitempty` for optional fields.

### Handler Signature

All tool handlers follow this signature (from MCP Go SDK v0.8.0):

```go
func handler(ctx context.Context, req *mcp.CallToolRequest, in InputType) (*mcp.CallToolResult, any, error)
```

- Return `mcputil.TextResult(...)` for success
- Return `mcputil.ErrResult(...)` for user-facing errors (don't return Go errors for expected failures)
- Return `nil, nil, err` only for truly unexpected errors
- The second return value (`any`) is metadata — always return `nil`

### WithInputOutput()

Use `mcputil.WithInputOutput()` for mutation/write tools. This records tool inputs and outputs as OTEL span events (`gen_ai.tool.input` / `gen_ai.tool.output`). Do NOT use for read-only tools (noise) or tools that receive sensitive data (tokens, secrets).

### File Organization

Each server follows this layout:

```
servers/my-server/
  go.mod              # Module with mcputil replace directive
  go.sum
  main.go             # Init, server creation, tool registration, signal handling
  readonly.go         # Read-only tool handlers + input types
  readwrite.go        # Mutation tool handlers + input types (if applicable)
  helpers.go          # Shared helpers (HTTP helpers, formatting, etc.)
  manifest.json       # OCI metadata
  Dockerfile          # Multi-stage: golang builder -> alpine runtime
```

Small servers (github, gitlab, tempo) may keep everything in `main.go` + one handler file.

## Building & Releasing

### Local Build

```bash
make build-server SERVER=my-server    # Builds dist/bin/mcp-my-server + dist/manifest.json
make push-server SERVER=my-server TAG=0.1.0  # Pushes OCI artifact to ghcr.io
```

### Release Pipeline

Triggered by pushing a tag matching `v*` (e.g. `git tag v0.9.0 && git push --tags`):

1. **create-release** — creates GitHub Release with auto-generated notes
2. **cli-release** — cross-compiles CLI for linux/darwin amd64/arm64
3. **server-packages** — builds each server as `mcp-{server}`, pushes OCI artifact to `ghcr.io/samyn92/agent-tools/{name}:{version}` (+ `latest`)
4. **server-images** — builds Docker images for servers (tagged `{name}-server:{version}`)

Matrix strategies use `fail-fast: false` — one server failure does not cancel others.

### OCI Artifact Format

| Field | Value |
|-------|-------|
| Artifact Type | `application/vnd.agents.io.mcp-tool.v1` |
| Layer Media Type | `application/vnd.agents.io.mcp-tool.code.v1.tar+gzip` |
| Config Media Type | `application/vnd.agents.io.mcp-tool.config.v1+json` |

The artifact contains: `manifest.json` + `bin/mcp-{server}` (+ optional co-bundled CLIs like `bin/kubectl`).

### Dockerfile Pattern

Multi-stage build. Builder copies `pkg/mcputil/` to `/pkg/mcputil/` (matching the replace directive path):

```dockerfile
FROM golang:1.26 AS builder
WORKDIR /app
COPY my-server/go.mod my-server/go.sum ./
COPY pkg/mcputil/ /pkg/mcputil/
RUN go mod download
COPY my-server/*.go ./
RUN CGO_ENABLED=0 GOOS=linux go build -ldflags="-s -w" -o mcp-my-server .

FROM alpine:3.21
RUN apk add --no-cache ca-certificates
COPY --from=builder /app/mcp-my-server /bin/mcp-my-server
COPY my-server/manifest.json /manifest.json
ENTRYPOINT ["/bin/mcp-my-server"]
```

For CLI wrappers, install the upstream CLI in the runtime stage (e.g. `RUN apk add --no-cache kubectl`).

## Adding a New Server — Checklist

1. Create `servers/my-server/` with `go.mod`:
   ```
   module github.com/samyn92/agent-tools/servers/my-server
   go 1.26.1
   require (
       github.com/modelcontextprotocol/go-sdk v0.8.0
       github.com/samyn92/agent-tools/servers/pkg/mcputil v0.0.0
   )
   replace github.com/samyn92/agent-tools/servers/pkg/mcputil => ../pkg/mcputil
   ```
2. Implement `main.go` following the patterns above (Init, NewServer, AddToolTo, Ready, Run)
3. Create `manifest.json` with `"command": "mcp-my-server"`
4. Create `Dockerfile` (multi-stage, copy `pkg/mcputil/`)
5. Add server name to **both** matrices in `.github/workflows/release.yaml`:
   - `server-packages.matrix.server` (OCI artifacts)
   - `server-images.matrix.server` (container images)
6. Test locally: `cd servers/my-server && go build -o mcp-my-server . && echo '{}' | ./mcp-my-server` (should start MCP handshake on stdio)
7. Create `AgentTool` CR and apply to cluster
8. Update `README.md` server catalog table

## Kubernetes Deployment

Tools are delivered to agent pods as OCI artifacts. The operator:

1. Reads `AgentTool` CRs in the `agents` namespace
2. Adds crane init containers to agent pod specs to pull OCI artifacts
3. Extracts to `/tools/{name}/bin/mcp-{name}` + `/tools/{name}/manifest.json`
4. The Fantasy runtime reads manifests and spawns MCP servers on stdio at startup

### AgentTool CR

```yaml
apiVersion: agents.agentops.io/v1alpha1
kind: AgentTool
metadata:
  name: my-server
  namespace: agents
spec:
  category: infrastructure
  description: "What this tool does"
  oci:
    ref: ghcr.io/samyn92/agent-tools/my-server:0.8.2
```

### Agent CR tool binding

```yaml
spec:
  tools:
    - name: my-server
  secrets:                          # If the tool needs env vars from secrets
    - name: MY_TOKEN
      secretRef:
        name: my-secret
        key: token
```

## Server Catalog (current: v0.8.2)

| Server | Tools | Pattern | Env vars required |
|--------|-------|---------|-------------------|
| kubectl | 16 | CLI wrapper | `MODE` (default: readonly) |
| kube-explore | 8 | In-cluster K8s | `MODE` (default: readonly) |
| flux | 15 | CLI wrapper | `MODE` (default: readonly) |
| git | 12 | CLI wrapper | `MODE` (default: readwrite) |
| github | 12 | API-based | `GITHUB_TOKEN` or `GH_TOKEN` |
| gitlab | 10 | API-based | `GITLAB_TOKEN`, `GITLAB_URL` |
| tempo | 6 | API-based | `TEMPO_URL` |

## Related Repositories

| Repo | Relevance |
|------|-----------|
| `agentops-core` | Operator that provisions AgentTool CRs into agent deployments |
| `agentops-runtime` | Fantasy SDK runtime that spawns these MCP servers and calls tools |
| `agentops-console` | Web console that displays tool call results |
| `agentops-platform` | Umbrella Helm chart that deploys the whole platform |

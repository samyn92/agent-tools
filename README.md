# agent-tools

OCI tool packages and Pi agent packaging for the Agent Operator platform.

This repository provides **tool packages** (git, file, github, gitlab) as OCI artifacts that AI agents consume at runtime via [tool-bridge](https://github.com/samyn92/agent-operator-core/tree/main/images/tool-bridge) (MCP) or direct import (PiAgents). It also provides the CLI for pushing these artifacts to OCI registries.

## Architecture Overview

```
                    BUILD / PUSH TIME                        DEPLOY / RUNTIME
                   (this repository)                     (agent-operator-core)

  Tool Package (TypeScript)         OCI Registry              Agent Pod
  ┌──────────────────────┐    ┌──────────────────┐   ┌─────────────────────┐
  │ tools/git/            │    │ ghcr.io/myorg/   │   │                     │
  │   index.ts            │───▶│   agent-tools/   │   │ tool-bridge         │
  │   (exports            │    │     git:0.1.0    │───▶│   (MCP stdio)      │
  │    AgentTool[])       │    │                  │   │   pulls OCI layer   │
  └──────────────────────┘    └──────────────────┘   │   imports tools     │
                                                     │   serves via MCP    │
  Pi Agent (TypeScript)                               └─────────────────────┘
  ┌──────────────────────┐    ┌──────────────────┐
  │ agents/issue-worker/  │    │ ghcr.io/myorg/   │   PiAgent Job
  │   index.ts            │───▶│   issue-worker:  │───▶ (imports directly)
  │   (agent logic)       │    │     v1.0.0       │
  └──────────────────────┘    └──────────────────┘
```

## Repository Structure

```
agent-tools/
  tools/                # OCI tool packages (AgentTool[] exports)
    git/                #   Git operations (clone, commit, push, etc.)
    file/               #   File system operations (read, write, search)
    github/             #   GitHub API operations (issues, PRs, repos)
    gitlab/             #   GitLab API operations (issues, MRs, projects)
  agents/               # Pi agent source code
    issue-worker/       #   Issue classification and routing agent
  cmd/cli/              # CLI for pushing OCI artifacts
  pkg/
    toolpush/           # OCI push logic for tool packages
    piagent/            # OCI push logic for Pi agents
    cmdvalidator/       # Command validation utilities
```

## Pushing OCI Artifacts

### Push a Tool Package

```bash
# Push a tool package to an OCI registry
agent-tools push tool ./tools/git/ -t ghcr.io/myorg/agent-tools/git:0.1.0

# The pushed artifact can be referenced in a Capability CRD:
#   spec:
#     mcp:
#       toolBridge:
#         toolRefs:
#           - ref: ghcr.io/myorg/agent-tools/git:0.1.0
```

### Push a Pi Agent

```bash
# Push a Pi agent to an OCI registry
agent-tools push piagent ./agents/issue-worker/ -t ghcr.io/myorg/issue-worker:v1.0.0

# The pushed artifact can be referenced in a PiAgent CRD:
#   spec:
#     source:
#       oci:
#         ref: ghcr.io/myorg/issue-worker:v1.0.0
```

## Available Tool Packages

| Package | Description | Served via |
|---------|-------------|------------|
| `tools/git` | Git operations (clone, commit, push, branch, diff) | tool-bridge MCP / PiAgent import |
| `tools/file` | File system operations (read, write, search, list) | tool-bridge MCP / PiAgent import |
| `tools/github` | GitHub API (issues, PRs, repos, reviews) | tool-bridge MCP / PiAgent import |
| `tools/gitlab` | GitLab API (issues, MRs, projects, pipelines) | tool-bridge MCP / PiAgent import |

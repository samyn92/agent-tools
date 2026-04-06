# agent-tools

OCI tool packages and agent packaging for the AgenticOps platform.

This repository provides **tool packages** (git, file, github, gitlab) as OCI artifacts that AI agents consume at runtime, and a CLI for pushing these artifacts to OCI registries.

## Architecture Overview

```
                    BUILD / PUSH TIME                        DEPLOY / RUNTIME
                   (this repository)                     (agenticops-core)

  Tool Package (JavaScript)         OCI Registry              Agent Pod
  ┌──────────────────────┐    ┌──────────────────┐   ┌─────────────────────┐
  │ tools/git/            │    │ ghcr.io/myorg/   │   │                     │
  │   index.js            │───▶│   agent-tools/   │   │ init container      │
  │   (exports            │    │     git:0.1.0    │───▶│   pulls OCI layer  │
  │    AgentTool[])       │    │                  │   │   mounts to /tools  │
  └──────────────────────┘    └──────────────────┘   └─────────────────────┘

  Agent Source (JavaScript)                            Agent Job
  ┌──────────────────────┐    ┌──────────────────┐   ┌─────────────────────┐
  │ agents/issue-worker/  │    │ ghcr.io/myorg/   │   │ agent-runtime       │
  │   index.js            │───▶│   issue-worker:  │───▶│   imports agent    │
  │   (agent config)      │    │     v1.0.0       │   │   logic directly   │
  └──────────────────────┘    └──────────────────┘   └─────────────────────┘
```

## Repository Structure

```
agent-tools/
  tools/                # OCI tool packages (AgentTool[] exports)
    git/                #   Git operations (clone, commit, push, etc.)
    file/               #   File system operations (read, write, search)
    github/             #   GitHub API operations (issues, PRs, repos)
    gitlab/             #   GitLab API operations (issues, MRs, projects)
  agents/               # Agent source code
    issue-worker/       #   Issue-to-MR automation agent
  cmd/cli/              # CLI for pushing OCI artifacts
  internal/ocipush/     # Shared OCI artifact push logic
  pkg/
    toolpush/           # OCI push for tool packages (custom media types)
    piagent/            # OCI push for agent packages (custom media types)
```

## Pushing OCI Artifacts

### Push a Tool Package

```bash
# Push a tool package to an OCI registry
agent-tools push tool ./tools/git/ -t ghcr.io/myorg/agent-tools/git:0.1.0

# The pushed artifact can be referenced in an Agent CRD:
#   spec:
#     toolRefs:
#       - name: git
#         ref: ghcr.io/myorg/agent-tools/git:0.1.0
```

### Push an Agent

```bash
# Push an agent to an OCI registry
agent-tools push piagent ./agents/issue-worker/ -t ghcr.io/myorg/issue-worker:v1.0.0

# The pushed artifact can be referenced in an Agent CRD (task mode):
#   spec:
#     mode: task
#     source:
#       oci:
#         ref: ghcr.io/myorg/issue-worker:v1.0.0
```

## Available Tool Packages

| Package | Description | Tools |
|---------|-------------|-------|
| `tools/git` | Git operations | clone, commit, push, pull, branch, diff, log, status, show |
| `tools/file` | File system operations | read, write, edit, list, search, mkdir |
| `tools/github` | GitHub API | PRs, issues, comments, checks, workflows, repos, branches |
| `tools/gitlab` | GitLab API | MRs, issues, notes, pipelines, diffs |

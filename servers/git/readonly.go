package main

import (
	"context"
	"fmt"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

// ── Input types ──

type statusInput struct {
	Cwd string `json:"cwd,omitempty" jsonschema_description:"Working directory (relative to WORKSPACE or absolute)"`
}

type diffInput struct {
	Cwd    string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Staged bool   `json:"staged,omitempty" jsonschema_description:"Show staged changes (--cached)"`
	Ref    string `json:"ref,omitempty" jsonschema_description:"Compare against a specific ref (commit/branch)"`
}

type logInput struct {
	Cwd     string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Count   int    `json:"count,omitempty" jsonschema_description:"Number of commits to show (default: 20)"`
	Oneline bool   `json:"oneline,omitempty" jsonschema_description:"One-line format"`
}

type branchListInput struct {
	Cwd string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	All bool   `json:"all,omitempty" jsonschema_description:"Show remote branches too (-a)"`
}

type showInput struct {
	Cwd string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Ref string `json:"ref,omitempty" jsonschema_description:"Commit ref to show (default: HEAD)"`
}

// ── Handlers ──

func registerReadonlyTools(s *mcp.Server) {
	add(s, "git_status", "Show the working tree status (modified, staged, untracked files).", handleStatus)
	add(s, "git_diff", "Show changes between commits, commit and working tree, etc.", handleDiff)
	add(s, "git_log", "Show commit logs. Returns recent commits with hash, author, date, and message.", handleLog)
	add(s, "git_branch_list", "List all local and optionally remote branches.", handleBranchList)
	add(s, "git_show", "Show the contents of a commit (diff + message).", handleShow)
}

func handleStatus(_ context.Context, _ *mcp.CallToolRequest, in statusInput) (*mcp.CallToolResult, any, error) {
	return git(in.Cwd, "status", "--short", "--branch"), nil, nil
}

func handleDiff(_ context.Context, _ *mcp.CallToolRequest, in diffInput) (*mcp.CallToolResult, any, error) {
	args := []string{"diff"}
	if in.Staged {
		args = append(args, "--cached")
	}
	if in.Ref != "" {
		args = append(args, in.Ref)
	}
	return git(in.Cwd, args...), nil, nil
}

func handleLog(_ context.Context, _ *mcp.CallToolRequest, in logInput) (*mcp.CallToolResult, any, error) {
	count := in.Count
	if count <= 0 {
		count = 20
	}
	args := []string{"log", fmt.Sprintf("-n%d", count)}
	if in.Oneline {
		args = append(args, "--oneline")
	} else {
		args = append(args, "--format=%h %ad %an: %s", "--date=short")
	}
	return git(in.Cwd, args...), nil, nil
}

func handleBranchList(_ context.Context, _ *mcp.CallToolRequest, in branchListInput) (*mcp.CallToolResult, any, error) {
	if in.All {
		return git(in.Cwd, "branch", "-a", "--format=%(refname:short) %(objectname:short) %(subject)"), nil, nil
	}
	return git(in.Cwd, "branch", "--format=%(refname:short) %(objectname:short) %(subject)"), nil, nil
}

func handleShow(_ context.Context, _ *mcp.CallToolRequest, in showInput) (*mcp.CallToolResult, any, error) {
	ref := or(in.Ref, "HEAD")
	return git(in.Cwd, "show", ref, "--stat", "--format=commit %H%nAuthor: %an <%ae>%nDate:   %ad%n%n    %s%n%n    %b"), nil, nil
}

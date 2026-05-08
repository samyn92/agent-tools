/*
MCP Tool: GitHub

An MCP stdio server providing GitHub API operations.
Uses net/http directly — no external dependencies beyond the MCP SDK.
Self-contained binary, no gh CLI needed.

Requires: GITHUB_TOKEN or GH_TOKEN env var.
Optional: GITHUB_API_URL (default: https://api.github.com)

Tools: github_get_repo, github_list_prs, github_get_pr, github_get_pr_diff,

	github_create_pr, github_add_pr_comment, github_list_issues,
	github_get_issue, github_add_issue_comment, github_list_branches,
	github_get_check_runs, github_get_workflow_runs
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/url"
	"os"
	"os/signal"
	"strings"
	"syscall"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

var (
	apiBase string
	token   string
	log     *slog.Logger
)

// version is set at build time via -ldflags "-X main.version=..."
var version = "dev"

func main() {
	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-github")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	apiBase = strings.TrimRight(or(os.Getenv("GITHUB_API_URL"), "https://api.github.com"), "/")
	token = or(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
	if token == "" {
		log.Error("GITHUB_TOKEN or GH_TOKEN environment variable is required")
		os.Exit(1)
	}

	server := mcputil.NewServer("github-tools", version)

	mcputil.AddToolTo(server, "github_get_repo", "Get repository info (description, stars, language, default branch).", handleGetRepo)
	mcputil.AddToolTo(server, "github_list_prs", "List pull requests for a repository.", handleListPRs)
	mcputil.AddToolTo(server, "github_get_pr", "Get details of a specific pull request.", handleGetPR)
	mcputil.AddToolTo(server, "github_get_pr_diff", "Get the diff of a pull request.", handleGetPRDiff)
	mcputil.AddToolTo(server, "github_create_pr", "Create a new pull request.", handleCreatePR, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "github_add_pr_comment", "Add a comment to a pull request.", handleAddPRComment, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "github_list_issues", "List issues for a repository.", handleListIssues)
	mcputil.AddToolTo(server, "github_get_issue", "Get details of a specific issue.", handleGetIssue)
	mcputil.AddToolTo(server, "github_add_issue_comment", "Add a comment to an issue.", handleAddIssueComment, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "github_list_branches", "List branches in a repository.", handleListBranches)
	mcputil.AddToolTo(server, "github_get_check_runs", "Get check runs for a commit ref.", handleGetCheckRuns)
	mcputil.AddToolTo(server, "github_get_workflow_runs", "Get recent workflow runs for a repository.", handleGetWorkflowRuns)

	mcputil.Ready("mcp-tool-github")
	defer mcputil.NotReady("mcp-tool-github")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
	}
}

// ── Input types ──

type repoInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner (user or org)"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
}

type listPRsInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
	State string `json:"state,omitempty" jsonschema_description:"PR state: open (default) / closed / all"`
}

type prInput struct {
	Owner  string `json:"owner" jsonschema_description:"Repository owner"`
	Repo   string `json:"repo" jsonschema_description:"Repository name"`
	Number int    `json:"number" jsonschema_description:"Pull request number"`
}

type createPRInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
	Title string `json:"title" jsonschema_description:"PR title"`
	Body  string `json:"body,omitempty" jsonschema_description:"PR description body"`
	Head  string `json:"head" jsonschema_description:"Branch with changes (e.g. feature-branch)"`
	Base  string `json:"base" jsonschema_description:"Branch to merge into (e.g. main)"`
	Draft bool   `json:"draft,omitempty" jsonschema_description:"Create as draft PR"`
}

type commentInput struct {
	Owner  string `json:"owner" jsonschema_description:"Repository owner"`
	Repo   string `json:"repo" jsonschema_description:"Repository name"`
	Number int    `json:"number" jsonschema_description:"Issue or PR number"`
	Body   string `json:"body" jsonschema_description:"Comment body (Markdown supported)"`
}

type listIssuesInput struct {
	Owner  string `json:"owner" jsonschema_description:"Repository owner"`
	Repo   string `json:"repo" jsonschema_description:"Repository name"`
	State  string `json:"state,omitempty" jsonschema_description:"Issue state: open (default) / closed / all"`
	Labels string `json:"labels,omitempty" jsonschema_description:"Comma-separated label filter"`
}

type issueInput struct {
	Owner  string `json:"owner" jsonschema_description:"Repository owner"`
	Repo   string `json:"repo" jsonschema_description:"Repository name"`
	Number int    `json:"number" jsonschema_description:"Issue number"`
}

type branchesInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
}

type checkRunsInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
	Ref   string `json:"ref" jsonschema_description:"Git ref (commit SHA or branch name)"`
}

type workflowRunsInput struct {
	Owner string `json:"owner" jsonschema_description:"Repository owner"`
	Repo  string `json:"repo" jsonschema_description:"Repository name"`
}

// ── Handlers ──

func handleGetRepo(ctx context.Context, _ *mcp.CallToolRequest, in repoInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s", url.PathEscape(in.Owner), url.PathEscape(in.Repo)), nil, nil
}

func handleListPRs(ctx context.Context, _ *mcp.CallToolRequest, in listPRsInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "open")
	return ghGet(ctx, "/repos/%s/%s/pulls?state=%s&per_page=30", url.PathEscape(in.Owner), url.PathEscape(in.Repo), url.QueryEscape(state)), nil, nil
}

func handleGetPR(ctx context.Context, _ *mcp.CallToolRequest, in prInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s/pulls/%d", url.PathEscape(in.Owner), url.PathEscape(in.Repo), in.Number), nil, nil
}

func handleGetPRDiff(ctx context.Context, _ *mcp.CallToolRequest, in prInput) (*mcp.CallToolResult, any, error) {
	u := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", apiBase, url.PathEscape(in.Owner), url.PathEscape(in.Repo), in.Number)
	body, status, err := mcputil.TracedHTTP(ctx, "GET", u,
		mcputil.WithHeader("Authorization", "Bearer "+token),
		mcputil.WithHeader("Accept", "application/vnd.github.v3.diff"),
	)
	if err != nil {
		return mcputil.ErrResult("request failed: %v", err), nil, nil
	}
	if status >= 400 {
		return mcputil.ErrResult("HTTP %d: %s", status, string(body)), nil, nil
	}
	return mcputil.TextResult(string(body)), nil, nil
}

func handleCreatePR(ctx context.Context, _ *mcp.CallToolRequest, in createPRInput) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"title": in.Title,
		"head":  in.Head,
		"base":  in.Base,
		"draft": in.Draft,
	}
	if in.Body != "" {
		payload["body"] = in.Body
	}
	return ghPost(ctx, fmt.Sprintf("/repos/%s/%s/pulls", url.PathEscape(in.Owner), url.PathEscape(in.Repo)), payload), nil, nil
}

func handleAddPRComment(ctx context.Context, _ *mcp.CallToolRequest, in commentInput) (*mcp.CallToolResult, any, error) {
	return ghPost(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(in.Owner), url.PathEscape(in.Repo), in.Number),
		map[string]any{"body": in.Body}), nil, nil
}

func handleListIssues(ctx context.Context, _ *mcp.CallToolRequest, in listIssuesInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "open")
	path := fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=30", url.PathEscape(in.Owner), url.PathEscape(in.Repo), url.QueryEscape(state))
	if in.Labels != "" {
		path += "&labels=" + url.QueryEscape(in.Labels)
	}
	return ghGet(ctx, "%s", path), nil, nil
}

func handleGetIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s/issues/%d", url.PathEscape(in.Owner), url.PathEscape(in.Repo), in.Number), nil, nil
}

func handleAddIssueComment(ctx context.Context, _ *mcp.CallToolRequest, in commentInput) (*mcp.CallToolResult, any, error) {
	return ghPost(ctx, fmt.Sprintf("/repos/%s/%s/issues/%d/comments", url.PathEscape(in.Owner), url.PathEscape(in.Repo), in.Number),
		map[string]any{"body": in.Body}), nil, nil
}

func handleListBranches(ctx context.Context, _ *mcp.CallToolRequest, in branchesInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s/branches?per_page=100", url.PathEscape(in.Owner), url.PathEscape(in.Repo)), nil, nil
}

func handleGetCheckRuns(ctx context.Context, _ *mcp.CallToolRequest, in checkRunsInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s/commits/%s/check-runs", url.PathEscape(in.Owner), url.PathEscape(in.Repo), url.PathEscape(in.Ref)), nil, nil
}

func handleGetWorkflowRuns(ctx context.Context, _ *mcp.CallToolRequest, in workflowRunsInput) (*mcp.CallToolResult, any, error) {
	return ghGet(ctx, "/repos/%s/%s/actions/runs?per_page=10", url.PathEscape(in.Owner), url.PathEscape(in.Repo)), nil, nil
}

// ── HTTP helpers ──

func ghGet(ctx context.Context, pathFmt string, args ...any) *mcp.CallToolResult {
	u := apiBase + fmt.Sprintf(pathFmt, args...)
	body, status, err := mcputil.TracedHTTP(ctx, "GET", u,
		mcputil.WithHeader("Authorization", "Bearer "+token),
		mcputil.WithHeader("Accept", "application/vnd.github+json"),
	)
	if err != nil {
		log.ErrorContext(ctx, "github API error", "url", u, "error", err)
		return mcputil.ErrResult("request failed: %v", err)
	}
	if status >= 400 {
		log.WarnContext(ctx, "github API non-200", "url", u, "status", status)
		return mcputil.ErrResult("HTTP %d: %s", status, string(body))
	}
	return prettyJSON(body)
}

func ghPost(ctx context.Context, path string, payload map[string]any) *mcp.CallToolResult {
	data, _ := json.Marshal(payload)
	u := apiBase + path
	body, status, err := mcputil.TracedHTTP(ctx, "POST", u,
		mcputil.WithHeader("Authorization", "Bearer "+token),
		mcputil.WithHeader("Accept", "application/vnd.github+json"),
		mcputil.WithHeader("Content-Type", "application/json"),
		mcputil.WithBody(strings.NewReader(string(data))),
	)
	if err != nil {
		log.ErrorContext(ctx, "github API error", "url", u, "error", err)
		return mcputil.ErrResult("request failed: %v", err)
	}
	if status >= 400 {
		log.WarnContext(ctx, "github API non-200", "url", u, "status", status)
		return mcputil.ErrResult("HTTP %d: %s", status, string(body))
	}
	return prettyJSON(body)
}

// prettyJSON formats JSON for display, falling back to raw text.
func prettyJSON(data []byte) *mcp.CallToolResult {
	var raw json.RawMessage
	if json.Unmarshal(data, &raw) == nil {
		if formatted, err := json.MarshalIndent(raw, "", "  "); err == nil {
			return mcputil.TextResult(string(formatted))
		}
	}
	return mcputil.TextResult(string(data))
}

// ── Helpers ──

func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

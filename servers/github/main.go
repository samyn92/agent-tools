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
	"io"
	"log"
	"net/http"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	apiBase string
	token   string
)

func main() {
	apiBase = strings.TrimRight(or(os.Getenv("GITHUB_API_URL"), "https://api.github.com"), "/")
	token = or(os.Getenv("GITHUB_TOKEN"), os.Getenv("GH_TOKEN"))
	if token == "" {
		log.Fatal("GITHUB_TOKEN or GH_TOKEN environment variable is required")
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "github-tools", Version: "0.1.0"},
		nil,
	)

	add(server, "github_get_repo", "Get repository info (description, stars, language, default branch).", handleGetRepo)
	add(server, "github_list_prs", "List pull requests for a repository.", handleListPRs)
	add(server, "github_get_pr", "Get details of a specific pull request.", handleGetPR)
	add(server, "github_get_pr_diff", "Get the diff of a pull request.", handleGetPRDiff)
	add(server, "github_create_pr", "Create a new pull request.", handleCreatePR)
	add(server, "github_add_pr_comment", "Add a comment to a pull request.", handleAddPRComment)
	add(server, "github_list_issues", "List issues for a repository.", handleListIssues)
	add(server, "github_get_issue", "Get details of a specific issue.", handleGetIssue)
	add(server, "github_add_issue_comment", "Add a comment to an issue.", handleAddIssueComment)
	add(server, "github_list_branches", "List branches in a repository.", handleListBranches)
	add(server, "github_get_check_runs", "Get check runs for a commit ref.", handleGetCheckRuns)
	add(server, "github_get_workflow_runs", "Get recent workflow runs for a repository.", handleGetWorkflowRuns)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
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

func handleGetRepo(_ context.Context, _ *mcp.CallToolRequest, in repoInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s", in.Owner, in.Repo), nil, nil
}

func handleListPRs(_ context.Context, _ *mcp.CallToolRequest, in listPRsInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "open")
	return ghGet("/repos/%s/%s/pulls?state=%s&per_page=30", in.Owner, in.Repo, state), nil, nil
}

func handleGetPR(_ context.Context, _ *mcp.CallToolRequest, in prInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s/pulls/%d", in.Owner, in.Repo, in.Number), nil, nil
}

func handleGetPRDiff(_ context.Context, _ *mcp.CallToolRequest, in prInput) (*mcp.CallToolResult, any, error) {
	url := fmt.Sprintf("%s/repos/%s/%s/pulls/%d", apiBase, in.Owner, in.Repo, in.Number)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github.v3.diff")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errResult("request failed: %v", err), nil, nil
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return errResult("HTTP %d: %s", resp.StatusCode, string(body)), nil, nil
	}
	return textResult(string(body)), nil, nil
}

func handleCreatePR(_ context.Context, _ *mcp.CallToolRequest, in createPRInput) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"title": in.Title,
		"head":  in.Head,
		"base":  in.Base,
		"draft": in.Draft,
	}
	if in.Body != "" {
		payload["body"] = in.Body
	}
	return ghPost(fmt.Sprintf("/repos/%s/%s/pulls", in.Owner, in.Repo), payload), nil, nil
}

func handleAddPRComment(_ context.Context, _ *mcp.CallToolRequest, in commentInput) (*mcp.CallToolResult, any, error) {
	return ghPost(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", in.Owner, in.Repo, in.Number),
		map[string]any{"body": in.Body}), nil, nil
}

func handleListIssues(_ context.Context, _ *mcp.CallToolRequest, in listIssuesInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "open")
	url := fmt.Sprintf("/repos/%s/%s/issues?state=%s&per_page=30", in.Owner, in.Repo, state)
	if in.Labels != "" {
		url += "&labels=" + in.Labels
	}
	return ghGet("%s", url), nil, nil
}

func handleGetIssue(_ context.Context, _ *mcp.CallToolRequest, in issueInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s/issues/%d", in.Owner, in.Repo, in.Number), nil, nil
}

func handleAddIssueComment(_ context.Context, _ *mcp.CallToolRequest, in commentInput) (*mcp.CallToolResult, any, error) {
	return ghPost(fmt.Sprintf("/repos/%s/%s/issues/%d/comments", in.Owner, in.Repo, in.Number),
		map[string]any{"body": in.Body}), nil, nil
}

func handleListBranches(_ context.Context, _ *mcp.CallToolRequest, in branchesInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s/branches?per_page=100", in.Owner, in.Repo), nil, nil
}

func handleGetCheckRuns(_ context.Context, _ *mcp.CallToolRequest, in checkRunsInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s/commits/%s/check-runs", in.Owner, in.Repo, in.Ref), nil, nil
}

func handleGetWorkflowRuns(_ context.Context, _ *mcp.CallToolRequest, in workflowRunsInput) (*mcp.CallToolResult, any, error) {
	return ghGet("/repos/%s/%s/actions/runs?per_page=10", in.Owner, in.Repo), nil, nil
}

// ── HTTP helpers ──

func ghGet(pathFmt string, args ...any) *mcp.CallToolResult {
	url := apiBase + fmt.Sprintf(pathFmt, args...)
	req, _ := http.NewRequest("GET", url, nil)
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	return doRequest(req)
}

func ghPost(path string, payload map[string]any) *mcp.CallToolResult {
	data, _ := json.Marshal(payload)
	url := apiBase + path
	req, _ := http.NewRequest("POST", url, strings.NewReader(string(data)))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("Content-Type", "application/json")
	return doRequest(req)
}

func doRequest(req *http.Request) *mcp.CallToolResult {
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return errResult("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if resp.StatusCode >= 400 {
		return errResult("HTTP %d: %s", resp.StatusCode, string(body))
	}

	// Pretty-print JSON
	var pretty json.RawMessage
	if json.Unmarshal(body, &pretty) == nil {
		formatted, err := json.MarshalIndent(pretty, "", "  ")
		if err == nil {
			return textResult(string(formatted))
		}
	}
	return textResult(string(body))
}

// ── Helpers ──

func add[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, h)
}

func textResult(text string) *mcp.CallToolResult {
	return &mcp.CallToolResult{Content: []mcp.Content{&mcp.TextContent{Text: text}}}
}

func errResult(format string, args ...any) *mcp.CallToolResult {
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf(format, args...)}},
		IsError: true,
	}
}

func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

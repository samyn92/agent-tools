/*
MCP Tool: GitLab

An MCP stdio server providing GitLab API operations.
Uses net/http directly — no external dependencies beyond the MCP SDK.
Self-contained binary, no glab CLI needed.

Requires: GITLAB_TOKEN env var.
Optional: GITLAB_URL (default: https://gitlab.com)

Tools: gitlab_get_project, gitlab_list_mrs, gitlab_get_mr, gitlab_get_mr_diff,
       gitlab_create_mr, gitlab_add_mr_note, gitlab_list_issues,
       gitlab_get_issue, gitlab_add_issue_note, gitlab_get_pipeline
*/
package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"os"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var (
	apiBase string
	token   string
)

func main() {
	glURL := strings.TrimRight(or(os.Getenv("GITLAB_URL"), "https://gitlab.com"), "/")
	apiBase = glURL + "/api/v4"
	token = os.Getenv("GITLAB_TOKEN")
	if token == "" {
		log.Fatal("GITLAB_TOKEN environment variable is required")
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "gitlab-tools", Version: "0.1.0"},
		nil,
	)

	add(server, "gitlab_get_project", "Get GitLab project info (description, visibility, default branch).", handleGetProject)
	add(server, "gitlab_list_mrs", "List merge requests for a project.", handleListMRs)
	add(server, "gitlab_get_mr", "Get details of a specific merge request.", handleGetMR)
	add(server, "gitlab_get_mr_diff", "Get the diff/changes of a merge request.", handleGetMRDiff)
	add(server, "gitlab_create_mr", "Create a new merge request.", handleCreateMR)
	add(server, "gitlab_add_mr_note", "Add a comment (note) to a merge request.", handleAddMRNote)
	add(server, "gitlab_list_issues", "List issues for a project.", handleListIssues)
	add(server, "gitlab_get_issue", "Get details of a specific issue.", handleGetIssue)
	add(server, "gitlab_add_issue_note", "Add a comment (note) to an issue.", handleAddIssueNote)
	add(server, "gitlab_get_pipeline", "Get details of a specific pipeline.", handleGetPipeline)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

// ── Input types ──

type projectInput struct {
	Project string `json:"project" jsonschema_description:"Project path (e.g. group/repo) or numeric ID"`
}

type listMRsInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	State   string `json:"state,omitempty" jsonschema_description:"MR state: opened (default) / closed / merged / all"`
}

type mrInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	IID     int    `json:"iid" jsonschema_description:"Merge request IID (project-scoped number)"`
}

type createMRInput struct {
	Project      string `json:"project" jsonschema_description:"Project path or ID"`
	Title        string `json:"title" jsonschema_description:"MR title"`
	Description  string `json:"description,omitempty" jsonschema_description:"MR description (Markdown)"`
	SourceBranch string `json:"source_branch" jsonschema_description:"Source branch with changes"`
	TargetBranch string `json:"target_branch" jsonschema_description:"Target branch to merge into (e.g. main)"`
}

type mrNoteInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	IID     int    `json:"iid" jsonschema_description:"Merge request IID"`
	Body    string `json:"body" jsonschema_description:"Note body (Markdown supported)"`
}

type listIssuesInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	State   string `json:"state,omitempty" jsonschema_description:"Issue state: opened (default) / closed / all"`
	Labels  string `json:"labels,omitempty" jsonschema_description:"Comma-separated label filter"`
}

type issueInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	IID     int    `json:"iid" jsonschema_description:"Issue IID (project-scoped number)"`
}

type issueNoteInput struct {
	Project string `json:"project" jsonschema_description:"Project path or ID"`
	IID     int    `json:"iid" jsonschema_description:"Issue IID"`
	Body    string `json:"body" jsonschema_description:"Note body (Markdown supported)"`
}

type pipelineInput struct {
	Project    string `json:"project" jsonschema_description:"Project path or ID"`
	PipelineID int    `json:"pipeline_id" jsonschema_description:"Pipeline ID"`
}

// ── Handlers ──

func handleGetProject(_ context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, any, error) {
	return glGet("/projects/%s", encode(in.Project)), nil, nil
}

func handleListMRs(_ context.Context, _ *mcp.CallToolRequest, in listMRsInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "opened")
	return glGet("/projects/%s/merge_requests?state=%s&per_page=30", encode(in.Project), state), nil, nil
}

func handleGetMR(_ context.Context, _ *mcp.CallToolRequest, in mrInput) (*mcp.CallToolResult, any, error) {
	return glGet("/projects/%s/merge_requests/%d", encode(in.Project), in.IID), nil, nil
}

func handleGetMRDiff(_ context.Context, _ *mcp.CallToolRequest, in mrInput) (*mcp.CallToolResult, any, error) {
	return glGet("/projects/%s/merge_requests/%d/changes", encode(in.Project), in.IID), nil, nil
}

func handleCreateMR(_ context.Context, _ *mcp.CallToolRequest, in createMRInput) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"title":         in.Title,
		"source_branch": in.SourceBranch,
		"target_branch": in.TargetBranch,
	}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	return glPost(fmt.Sprintf("/projects/%s/merge_requests", encode(in.Project)), payload), nil, nil
}

func handleAddMRNote(_ context.Context, _ *mcp.CallToolRequest, in mrNoteInput) (*mcp.CallToolResult, any, error) {
	return glPost(fmt.Sprintf("/projects/%s/merge_requests/%d/notes", encode(in.Project), in.IID),
		map[string]any{"body": in.Body}), nil, nil
}

func handleListIssues(_ context.Context, _ *mcp.CallToolRequest, in listIssuesInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "opened")
	u := fmt.Sprintf("/projects/%s/issues?state=%s&per_page=30", encode(in.Project), state)
	if in.Labels != "" {
		u += "&labels=" + in.Labels
	}
	return glGet(u), nil, nil
}

func handleGetIssue(_ context.Context, _ *mcp.CallToolRequest, in issueInput) (*mcp.CallToolResult, any, error) {
	return glGet("/projects/%s/issues/%d", encode(in.Project), in.IID), nil, nil
}

func handleAddIssueNote(_ context.Context, _ *mcp.CallToolRequest, in issueNoteInput) (*mcp.CallToolResult, any, error) {
	return glPost(fmt.Sprintf("/projects/%s/issues/%d/notes", encode(in.Project), in.IID),
		map[string]any{"body": in.Body}), nil, nil
}

func handleGetPipeline(_ context.Context, _ *mcp.CallToolRequest, in pipelineInput) (*mcp.CallToolResult, any, error) {
	return glGet("/projects/%s/pipelines/%d", encode(in.Project), in.PipelineID), nil, nil
}

// ── HTTP helpers ──

func glGet(pathFmt string, args ...any) *mcp.CallToolResult {
	u := apiBase + fmt.Sprintf(pathFmt, args...)
	req, _ := http.NewRequest("GET", u, nil)
	req.Header.Set("PRIVATE-TOKEN", token)
	return doRequest(req)
}

func glPost(path string, payload map[string]any) *mcp.CallToolResult {
	data, _ := json.Marshal(payload)
	u := apiBase + path
	req, _ := http.NewRequest("POST", u, strings.NewReader(string(data)))
	req.Header.Set("PRIVATE-TOKEN", token)
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

func encode(project string) string {
	return url.PathEscape(project)
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

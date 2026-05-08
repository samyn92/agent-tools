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
	shutdown, _ := mcputil.Init(context.Background(), "mcp-tool-gitlab")
	defer func() { shutdown(context.Background()) }()

	log = mcputil.Logger()

	glURL := strings.TrimRight(or(os.Getenv("GITLAB_URL"), "https://gitlab.com"), "/")
	apiBase = glURL + "/api/v4"
	token = os.Getenv("GITLAB_TOKEN")
	if token == "" {
		log.Error("GITLAB_TOKEN environment variable is required")
		os.Exit(1)
	}

	server := mcputil.NewServer("gitlab-tools", version)

	mcputil.AddToolTo(server, "gitlab_get_project", "Get GitLab project info (description, visibility, default branch).", handleGetProject)
	mcputil.AddToolTo(server, "gitlab_list_mrs", "List merge requests for a project.", handleListMRs)
	mcputil.AddToolTo(server, "gitlab_get_mr", "Get details of a specific merge request.", handleGetMR)
	mcputil.AddToolTo(server, "gitlab_get_mr_diff", "Get the diff/changes of a merge request.", handleGetMRDiff)
	mcputil.AddToolTo(server, "gitlab_create_mr", "Create a new merge request.", handleCreateMR, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "gitlab_add_mr_note", "Add a comment (note) to a merge request.", handleAddMRNote, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "gitlab_update_mr", "Update a merge request (title, description, labels, etc.).", handleUpdateMR, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "gitlab_list_issues", "List issues for a project.", handleListIssues)
	mcputil.AddToolTo(server, "gitlab_get_issue", "Get details of a specific issue.", handleGetIssue)
	mcputil.AddToolTo(server, "gitlab_add_issue_note", "Add a comment (note) to an issue.", handleAddIssueNote, mcputil.WithInputOutput())
	mcputil.AddToolTo(server, "gitlab_get_pipeline", "Get details of a specific pipeline.", handleGetPipeline)

	mcputil.Ready("mcp-tool-gitlab")
	defer mcputil.NotReady("mcp-tool-gitlab")

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	if err := server.Run(ctx, &mcp.StdioTransport{}); err != nil && ctx.Err() == nil {
		log.ErrorContext(ctx, "server exited with error", "error", err)
		os.Exit(1)
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

type updateMRInput struct {
	Project     string `json:"project" jsonschema_description:"Project path or ID"`
	IID         int    `json:"iid" jsonschema_description:"Merge request IID"`
	Title       string `json:"title,omitempty" jsonschema_description:"New MR title"`
	Description string `json:"description,omitempty" jsonschema_description:"New MR description (Markdown)"`
	Labels      string `json:"labels,omitempty" jsonschema_description:"Comma-separated labels to set"`
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

func handleGetProject(ctx context.Context, _ *mcp.CallToolRequest, in projectInput) (*mcp.CallToolResult, any, error) {
	return glGet(ctx, "/projects/%s", encode(in.Project)), nil, nil
}

func handleListMRs(ctx context.Context, _ *mcp.CallToolRequest, in listMRsInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "opened")
	return glGet(ctx, "/projects/%s/merge_requests?state=%s&per_page=30", encode(in.Project), state), nil, nil
}

func handleGetMR(ctx context.Context, _ *mcp.CallToolRequest, in mrInput) (*mcp.CallToolResult, any, error) {
	return glGet(ctx, "/projects/%s/merge_requests/%d", encode(in.Project), in.IID), nil, nil
}

func handleGetMRDiff(ctx context.Context, _ *mcp.CallToolRequest, in mrInput) (*mcp.CallToolResult, any, error) {
	return glGet(ctx, "/projects/%s/merge_requests/%d/changes", encode(in.Project), in.IID), nil, nil
}

func handleCreateMR(ctx context.Context, _ *mcp.CallToolRequest, in createMRInput) (*mcp.CallToolResult, any, error) {
	payload := map[string]any{
		"title":         in.Title,
		"source_branch": in.SourceBranch,
		"target_branch": in.TargetBranch,
	}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	return glPost(ctx, fmt.Sprintf("/projects/%s/merge_requests", encode(in.Project)), payload), nil, nil
}

func handleAddMRNote(ctx context.Context, _ *mcp.CallToolRequest, in mrNoteInput) (*mcp.CallToolResult, any, error) {
	return glPost(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d/notes", encode(in.Project), in.IID),
		map[string]any{"body": in.Body}), nil, nil
}

func handleUpdateMR(ctx context.Context, _ *mcp.CallToolRequest, in updateMRInput) (*mcp.CallToolResult, any, error) {
	payload := make(map[string]any)
	if in.Title != "" {
		payload["title"] = in.Title
	}
	if in.Description != "" {
		payload["description"] = in.Description
	}
	if in.Labels != "" {
		payload["labels"] = in.Labels
	}
	if len(payload) == 0 {
		return mcputil.ErrResult("at least one field (title, description, labels) is required"), nil, nil
	}
	return glPut(ctx, fmt.Sprintf("/projects/%s/merge_requests/%d", encode(in.Project), in.IID), payload), nil, nil
}

func handleListIssues(ctx context.Context, _ *mcp.CallToolRequest, in listIssuesInput) (*mcp.CallToolResult, any, error) {
	state := or(in.State, "opened")
	u := fmt.Sprintf("/projects/%s/issues?state=%s&per_page=30", encode(in.Project), state)
	if in.Labels != "" {
		u += "&labels=" + in.Labels
	}
	return glGet(ctx, "%s", u), nil, nil
}

func handleGetIssue(ctx context.Context, _ *mcp.CallToolRequest, in issueInput) (*mcp.CallToolResult, any, error) {
	return glGet(ctx, "/projects/%s/issues/%d", encode(in.Project), in.IID), nil, nil
}

func handleAddIssueNote(ctx context.Context, _ *mcp.CallToolRequest, in issueNoteInput) (*mcp.CallToolResult, any, error) {
	return glPost(ctx, fmt.Sprintf("/projects/%s/issues/%d/notes", encode(in.Project), in.IID),
		map[string]any{"body": in.Body}), nil, nil
}

func handleGetPipeline(ctx context.Context, _ *mcp.CallToolRequest, in pipelineInput) (*mcp.CallToolResult, any, error) {
	return glGet(ctx, "/projects/%s/pipelines/%d", encode(in.Project), in.PipelineID), nil, nil
}

// ── HTTP helpers ──

func glGet(ctx context.Context, pathFmt string, args ...any) *mcp.CallToolResult {
	u := apiBase + fmt.Sprintf(pathFmt, args...)
	body, status, err := mcputil.TracedHTTP(ctx, "GET", u,
		mcputil.WithHeader("PRIVATE-TOKEN", token),
	)
	if err != nil {
		log.ErrorContext(ctx, "gitlab API error", "url", u, "error", err)
		return mcputil.ErrResult("request failed: %v", err)
	}
	if status >= 400 {
		log.WarnContext(ctx, "gitlab API non-200", "url", u, "status", status)
		return mcputil.ErrResult("HTTP %d: %s", status, string(body))
	}
	return prettyJSON(body)
}

func glPost(ctx context.Context, path string, payload map[string]any) *mcp.CallToolResult {
	return glMutate(ctx, "POST", path, payload)
}

func glPut(ctx context.Context, path string, payload map[string]any) *mcp.CallToolResult {
	return glMutate(ctx, "PUT", path, payload)
}

func glMutate(ctx context.Context, method, path string, payload map[string]any) *mcp.CallToolResult {
	data, _ := json.Marshal(payload)
	u := apiBase + path
	body, status, err := mcputil.TracedHTTP(ctx, method, u,
		mcputil.WithHeader("PRIVATE-TOKEN", token),
		mcputil.WithHeader("Content-Type", "application/json"),
		mcputil.WithBody(strings.NewReader(string(data))),
	)
	if err != nil {
		log.ErrorContext(ctx, "gitlab API error", "url", u, "error", err)
		return mcputil.ErrResult("request failed: %v", err)
	}
	if status >= 400 {
		log.WarnContext(ctx, "gitlab API non-200", "url", u, "status", status)
		return mcputil.ErrResult("HTTP %d: %s", status, string(body))
	}
	return prettyJSON(body)
}

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

func encode(project string) string {
	return url.PathEscape(project)
}

func or(val, def string) string {
	if val == "" {
		return def
	}
	return val
}

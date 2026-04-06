/*
MCP Tool: Git

An MCP stdio server providing Git operations.
Shells out to the git CLI for maximum compatibility with auth,
SSH keys, credential helpers, GPG signing, etc.

Requires: git in PATH.

Tools: git_status, git_diff, git_log, git_add, git_commit, git_push,
       git_pull, git_branch, git_branch_list, git_show, git_clone, git_clone_or_pull
*/
package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
)

var workspace string

func main() {
	workspace = os.Getenv("WORKSPACE")
	if workspace == "" {
		workspace = "/workspace"
	}

	server := mcp.NewServer(
		&mcp.Implementation{Name: "git-tools", Version: "0.1.0"},
		nil,
	)

	add(server, "git_status", "Show the working tree status (modified, staged, untracked files).", handleStatus)
	add(server, "git_diff", "Show changes between commits, commit and working tree, etc.", handleDiff)
	add(server, "git_log", "Show commit logs. Returns recent commits with hash, author, date, and message.", handleLog)
	add(server, "git_add", "Stage files for commit. Use '.' to stage all changes.", handleAdd)
	add(server, "git_commit", "Create a new commit with staged changes.", handleCommit)
	add(server, "git_push", "Push commits to the remote repository.", handlePush)
	add(server, "git_pull", "Pull changes from the remote repository.", handlePull)
	add(server, "git_branch", "Create or switch branches.", handleBranch)
	add(server, "git_branch_list", "List all local and remote branches.", handleBranchList)
	add(server, "git_show", "Show the contents of a commit (diff + message).", handleShow)
	add(server, "git_clone", "Clone a repository into the workspace.", handleClone)
	add(server, "git_clone_or_pull", "Clone a repo if it doesn't exist locally or pull if it does.", handleCloneOrPull)

	if err := server.Run(context.Background(), &mcp.StdioTransport{}); err != nil {
		log.Fatal(err)
	}
}

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
	Cwd    string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Count  int    `json:"count,omitempty" jsonschema_description:"Number of commits to show (default: 20)"`
	Oneline bool  `json:"oneline,omitempty" jsonschema_description:"One-line format"`
}

type addInput struct {
	Cwd   string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Files string `json:"files" jsonschema_description:"Files to stage (space-separated or '.' for all)"`
}

type commitInput struct {
	Cwd     string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Message string `json:"message" jsonschema_description:"Commit message"`
}

type pushInput struct {
	Cwd    string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Remote string `json:"remote,omitempty" jsonschema_description:"Remote name (default: origin)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch to push (default: current branch)"`
	Force  bool   `json:"force,omitempty" jsonschema_description:"Force push (--force-with-lease)"`
}

type pullInput struct {
	Cwd    string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Remote string `json:"remote,omitempty" jsonschema_description:"Remote name (default: origin)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch to pull"`
}

type branchInput struct {
	Cwd    string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Name   string `json:"name" jsonschema_description:"Branch name to create or switch to"`
	Create bool   `json:"create,omitempty" jsonschema_description:"Create the branch if it doesn't exist"`
}

type branchListInput struct {
	Cwd string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	All bool   `json:"all,omitempty" jsonschema_description:"Show remote branches too (-a)"`
}

type showInput struct {
	Cwd string `json:"cwd,omitempty" jsonschema_description:"Working directory"`
	Ref string `json:"ref,omitempty" jsonschema_description:"Commit ref to show (default: HEAD)"`
}

type cloneInput struct {
	URL    string `json:"url" jsonschema_description:"Repository URL to clone"`
	Dir    string `json:"dir,omitempty" jsonschema_description:"Target directory name (defaults to repo name)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch to clone"`
	Depth  int    `json:"depth,omitempty" jsonschema_description:"Shallow clone depth (0 = full)"`
}

type cloneOrPullInput struct {
	URL    string `json:"url" jsonschema_description:"Repository URL"`
	Dir    string `json:"dir,omitempty" jsonschema_description:"Target directory name (defaults to repo name)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch"`
}

// ── Handlers ──

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

func handleAdd(_ context.Context, _ *mcp.CallToolRequest, in addInput) (*mcp.CallToolResult, any, error) {
	files := strings.Fields(in.Files)
	if len(files) == 0 {
		return errResult("files is required"), nil, nil
	}
	args := append([]string{"add"}, files...)
	return git(in.Cwd, args...), nil, nil
}

func handleCommit(_ context.Context, _ *mcp.CallToolRequest, in commitInput) (*mcp.CallToolResult, any, error) {
	if in.Message == "" {
		return errResult("message is required"), nil, nil
	}
	return git(in.Cwd, "commit", "-m", in.Message), nil, nil
}

func handlePush(_ context.Context, _ *mcp.CallToolRequest, in pushInput) (*mcp.CallToolResult, any, error) {
	remote := or(in.Remote, "origin")
	args := []string{"push", remote}
	if in.Branch != "" {
		args = append(args, in.Branch)
	}
	if in.Force {
		args = append(args, "--force-with-lease")
	}
	return git(in.Cwd, args...), nil, nil
}

func handlePull(_ context.Context, _ *mcp.CallToolRequest, in pullInput) (*mcp.CallToolResult, any, error) {
	remote := or(in.Remote, "origin")
	args := []string{"pull", remote}
	if in.Branch != "" {
		args = append(args, in.Branch)
	}
	return git(in.Cwd, args...), nil, nil
}

func handleBranch(_ context.Context, _ *mcp.CallToolRequest, in branchInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return errResult("name is required"), nil, nil
	}
	if in.Create {
		return git(in.Cwd, "checkout", "-b", in.Name), nil, nil
	}
	return git(in.Cwd, "checkout", in.Name), nil, nil
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

func handleClone(_ context.Context, _ *mcp.CallToolRequest, in cloneInput) (*mcp.CallToolResult, any, error) {
	if in.URL == "" {
		return errResult("url is required"), nil, nil
	}
	dir := in.Dir
	if dir == "" {
		// Extract repo name from URL
		parts := strings.Split(strings.TrimSuffix(in.URL, ".git"), "/")
		dir = parts[len(parts)-1]
	}
	target := resolveCwd(dir)

	args := []string{"clone"}
	if in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}
	if in.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", in.Depth))
	}
	args = append(args, in.URL, target)

	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult("clone failed: %s\n%s", err, string(out)), nil, nil
	}
	return textResult(fmt.Sprintf("Cloned %s to %s\n%s", in.URL, target, string(out))), nil, nil
}

func handleCloneOrPull(_ context.Context, _ *mcp.CallToolRequest, in cloneOrPullInput) (*mcp.CallToolResult, any, error) {
	if in.URL == "" {
		return errResult("url is required"), nil, nil
	}
	dir := in.Dir
	if dir == "" {
		parts := strings.Split(strings.TrimSuffix(in.URL, ".git"), "/")
		dir = parts[len(parts)-1]
	}
	target := resolveCwd(dir)

	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		// Repo exists — pull
		args := []string{"pull", "origin"}
		if in.Branch != "" {
			args = append(args, in.Branch)
		}
		cmd := exec.Command("git", args...)
		cmd.Dir = target
		cmd.Env = os.Environ()
		out, err := cmd.CombinedOutput()
		if err != nil {
			return errResult("pull failed: %s\n%s", err, string(out)), nil, nil
		}
		return textResult(fmt.Sprintf("Pulled in %s\n%s", target, string(out))), nil, nil
	}

	// Clone
	args := []string{"clone"}
	if in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}
	args = append(args, in.URL, target)
	cmd := exec.Command("git", args...)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return errResult("clone failed: %s\n%s", err, string(out)), nil, nil
	}
	return textResult(fmt.Sprintf("Cloned %s to %s\n%s", in.URL, target, string(out))), nil, nil
}

// ── Helpers ──

func add[In any](s *mcp.Server, name, desc string, h mcp.ToolHandlerFor[In, any]) {
	mcp.AddTool(s, &mcp.Tool{Name: name, Description: desc}, h)
}

func resolveCwd(cwd string) string {
	if cwd == "" {
		return workspace
	}
	if filepath.IsAbs(cwd) {
		return cwd
	}
	return filepath.Join(workspace, cwd)
}

func git(cwd string, args ...string) *mcp.CallToolResult {
	cmd := exec.Command("git", args...)
	cmd.Dir = resolveCwd(cwd)
	cmd.Env = os.Environ()
	out, err := cmd.CombinedOutput()
	if err != nil {
		return &mcp.CallToolResult{
			Content: []mcp.Content{&mcp.TextContent{Text: fmt.Sprintf("git %s failed: %s\n%s", strings.Join(args, " "), err, string(out))}},
			IsError: true,
		}
	}
	text := strings.TrimSpace(string(out))
	if text == "" {
		text = "(no output)"
	}
	return &mcp.CallToolResult{
		Content: []mcp.Content{&mcp.TextContent{Text: text}},
	}
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

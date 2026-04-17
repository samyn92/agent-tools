package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/modelcontextprotocol/go-sdk/mcp"
	"github.com/samyn92/agent-tools/servers/pkg/mcputil"
)

// ── Input types ──

type addFilesInput struct {
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
	Force  bool   `json:"force,omitempty" jsonschema_description:"Force push with lease (--force-with-lease)"`
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

type cloneInput struct {
	URL    string `json:"url" jsonschema_description:"Repository URL to clone"`
	Dir    string `json:"dir,omitempty" jsonschema_description:"Target directory name (defaults to repo name)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch to clone"`
	Depth  int    `json:"depth,omitempty" jsonschema_description:"Shallow clone depth (0 = full clone)"`
}

type cloneOrPullInput struct {
	URL    string `json:"url" jsonschema_description:"Repository URL"`
	Dir    string `json:"dir,omitempty" jsonschema_description:"Target directory name (defaults to repo name)"`
	Branch string `json:"branch,omitempty" jsonschema_description:"Branch to clone or pull"`
}

// ── Registration ──

func registerReadwriteTools(s *mcputil.Server) {
	mcputil.AddToolTo(s, "git_add", "Stage files for commit. Use '.' to stage all changes.", handleAdd)
	mcputil.AddToolTo(s, "git_commit", "Create a new commit with staged changes.", handleCommit, mcputil.WithInputOutput())
	mcputil.AddToolTo(s, "git_push", "Push commits to the remote repository.", handlePush, mcputil.WithInputOutput())
	mcputil.AddToolTo(s, "git_pull", "Pull changes from the remote repository.", handlePull)
	mcputil.AddToolTo(s, "git_branch", "Create or switch branches.", handleBranch, mcputil.WithInputOutput())
	mcputil.AddToolTo(s, "git_clone", "Clone a repository into the workspace.", handleClone, mcputil.WithInputOutput())
	mcputil.AddToolTo(s, "git_clone_or_pull", "Clone a repo if it doesn't exist locally, or pull latest if it does. Idempotent.", handleCloneOrPull, mcputil.WithInputOutput())
}

// ── Handlers ──

func handleAdd(ctx context.Context, _ *mcp.CallToolRequest, in addFilesInput) (*mcp.CallToolResult, any, error) {
	files := strings.Fields(in.Files)
	if len(files) == 0 {
		return mcputil.ErrResult("files is required"), nil, nil
	}
	args := append([]string{"add"}, files...)
	return git(ctx, in.Cwd, args...), nil, nil
}

func handleCommit(ctx context.Context, _ *mcp.CallToolRequest, in commitInput) (*mcp.CallToolResult, any, error) {
	if in.Message == "" {
		return mcputil.ErrResult("message is required"), nil, nil
	}
	return git(ctx, in.Cwd, "commit", "-m", in.Message), nil, nil
}

func handlePush(ctx context.Context, _ *mcp.CallToolRequest, in pushInput) (*mcp.CallToolResult, any, error) {
	remote := or(in.Remote, "origin")
	args := []string{"push", remote}
	if in.Branch != "" {
		args = append(args, in.Branch)
	}
	if in.Force {
		args = append(args, "--force-with-lease")
	}
	return gitNetwork(ctx, in.Cwd, args...), nil, nil
}

func handlePull(ctx context.Context, _ *mcp.CallToolRequest, in pullInput) (*mcp.CallToolResult, any, error) {
	remote := or(in.Remote, "origin")
	args := []string{"pull", remote}
	if in.Branch != "" {
		args = append(args, in.Branch)
	}
	return gitNetwork(ctx, in.Cwd, args...), nil, nil
}

func handleBranch(ctx context.Context, _ *mcp.CallToolRequest, in branchInput) (*mcp.CallToolResult, any, error) {
	if in.Name == "" {
		return mcputil.ErrResult("name is required"), nil, nil
	}
	if in.Create {
		return git(ctx, in.Cwd, "checkout", "-b", in.Name), nil, nil
	}
	return git(ctx, in.Cwd, "checkout", in.Name), nil, nil
}

func handleClone(ctx context.Context, _ *mcp.CallToolRequest, in cloneInput) (*mcp.CallToolResult, any, error) {
	if in.URL == "" {
		return mcputil.ErrResult("url is required"), nil, nil
	}
	target, err := resolveCloneTarget(in.URL, in.Dir)
	if err != nil {
		return mcputil.ErrResult("blocked: %s", err), nil, nil
	}

	args := []string{"clone"}
	if in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}
	if in.Depth > 0 {
		args = append(args, "--depth", fmt.Sprintf("%d", in.Depth))
	}
	args = append(args, in.URL, target)

	return gitWithTimeout(ctx, networkTimeout, "", args...), nil, nil
}

func handleCloneOrPull(ctx context.Context, _ *mcp.CallToolRequest, in cloneOrPullInput) (*mcp.CallToolResult, any, error) {
	if in.URL == "" {
		return mcputil.ErrResult("url is required"), nil, nil
	}
	target, err := resolveCloneTarget(in.URL, in.Dir)
	if err != nil {
		return mcputil.ErrResult("blocked: %s", err), nil, nil
	}

	if _, err := os.Stat(filepath.Join(target, ".git")); err == nil {
		// Repo exists — pull
		remote := "origin"
		args := []string{"pull", remote}
		if in.Branch != "" {
			args = append(args, in.Branch)
		}
		return gitNetwork(ctx, target, args...), nil, nil
	}

	// Clone
	args := []string{"clone"}
	if in.Branch != "" {
		args = append(args, "-b", in.Branch)
	}
	args = append(args, in.URL, target)

	return gitWithTimeout(ctx, networkTimeout, "", args...), nil, nil
}

// resolveCloneTarget determines the target directory for a clone.
func resolveCloneTarget(url, dir string) (string, error) {
	if dir == "" {
		parts := strings.Split(strings.TrimSuffix(url, ".git"), "/")
		dir = parts[len(parts)-1]
	}
	return resolveCwd(dir)
}

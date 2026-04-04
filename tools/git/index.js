/**
 * Git tool package for Pi Agents.
 *
 * Provides git operations (status, diff, log, add, commit, push, etc.) as AgentTool[]
 * that can be referenced via toolRefs in a PiAgent CRD.
 *
 * This module is self-contained — no external dependencies. It shells out to
 * the `git` CLI which must be available in the agent's container image.
 *
 * All tools accept an optional `cwd` parameter that specifies the working
 * directory for the git command. It can be:
 *   - A relative path (resolved against WORKSPACE, e.g. "myrepo")
 *   - An absolute path (used as-is, e.g. "/workspace/myrepo")
 * If omitted, defaults to the WORKSPACE env var (typically "/workspace").
 *
 * @module agent-tools/git
 */

import { execFile } from "node:child_process";
import { existsSync } from "node:fs";
import { resolve } from "node:path";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const WORKSPACE = process.env.WORKSPACE || "/workspace";

/**
 * Resolve a cwd parameter to an absolute path.
 * - If cwd starts with "/", use as-is.
 * - Otherwise, resolve relative to WORKSPACE.
 * - If cwd is falsy, return WORKSPACE.
 */
function resolveCwd(cwd) {
  if (!cwd) return WORKSPACE;
  if (cwd.startsWith("/")) return cwd;
  return resolve(WORKSPACE, cwd);
}

/** Run a git command and return stdout. Rejects on non-zero exit. */
function git(args, cwd = WORKSPACE) {
  return new Promise((resolve, reject) => {
    execFile("git", args, { cwd, maxBuffer: 10 * 1024 * 1024 }, (err, stdout, stderr) => {
      if (err) {
        reject(new Error(`git ${args[0]} failed: ${stderr || err.message}`));
      } else {
        resolve(stdout);
      }
    });
  });
}

/** Wrap a string result into AgentToolResult format. */
function textResult(text) {
  return {
    content: [{ type: "text", text: text || "(empty)" }],
    details: {},
  };
}

// ---------------------------------------------------------------------------
// Shared parameter fragments
// ---------------------------------------------------------------------------

const cwdParam = {
  cwd: {
    type: "string",
    description:
      "Working directory for the git command. Relative paths are resolved against " +
      "WORKSPACE (e.g. 'myrepo' → /workspace/myrepo). Absolute paths are used as-is. " +
      "Defaults to WORKSPACE if omitted.",
  },
};

// ---------------------------------------------------------------------------
// Parameter schemas (plain JSON Schema — no TypeBox dependency needed)
// ---------------------------------------------------------------------------

const cwdOnlyParams = {
  type: "object",
  properties: { ...cwdParam },
  required: [],
};

const pathParam = {
  type: "object",
  required: ["path"],
  properties: {
    path: { type: "string", description: "File or directory path (relative to repo root)" },
    ...cwdParam,
  },
};

const commitParams = {
  type: "object",
  required: ["message"],
  properties: {
    message: { type: "string", description: "Commit message" },
    ...cwdParam,
  },
};

const logParams = {
  type: "object",
  properties: {
    count: { type: "number", description: "Number of commits to show (default: 20)" },
    oneline: { type: "boolean", description: "Use --oneline format (default: true)" },
    ...cwdParam,
  },
  required: [],
};

const branchParams = {
  type: "object",
  properties: {
    name: { type: "string", description: "Branch name to create or switch to" },
    create: { type: "boolean", description: "Create the branch if it doesn't exist" },
    ...cwdParam,
  },
  required: ["name"],
};

const diffParams = {
  type: "object",
  properties: {
    staged: { type: "boolean", description: "Show only staged changes (--cached)" },
    path: { type: "string", description: "Limit diff to a specific path" },
    ...cwdParam,
  },
  required: [],
};

const pushParams = {
  type: "object",
  properties: {
    remote: { type: "string", description: "Remote name (default: origin)" },
    branch: { type: "string", description: "Branch to push (default: current)" },
    setUpstream: { type: "boolean", description: "Set upstream tracking (-u)" },
    ...cwdParam,
  },
  required: [],
};

const pullParams = {
  type: "object",
  properties: {
    remote: { type: "string", description: "Remote name (default: origin)" },
    branch: { type: "string", description: "Branch to pull (default: current)" },
    ...cwdParam,
  },
  required: [],
};

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

export const tools = [
  {
    name: "git_status",
    description: "Show the working tree status (modified, staged, untracked files).",
    label: "Git Status",
    parameters: cwdOnlyParams,
    execute: async (_id, params) => textResult(await git(["status"], resolveCwd(params?.cwd))),
  },

  {
    name: "git_diff",
    description: "Show changes in the working tree or staging area.",
    label: "Git Diff",
    parameters: diffParams,
    execute: async (_id, params) => {
      const args = ["diff"];
      if (params?.staged) args.push("--cached");
      if (params?.path) args.push("--", params.path);
      return textResult(await git(args, resolveCwd(params?.cwd)));
    },
  },

  {
    name: "git_log",
    description: "Show recent commit history.",
    label: "Git Log",
    parameters: logParams,
    execute: async (_id, params) => {
      const count = params?.count ?? 20;
      const args = ["log", `-${count}`];
      if (params?.oneline !== false) args.push("--oneline");
      return textResult(await git(args, resolveCwd(params?.cwd)));
    },
  },

  {
    name: "git_add",
    description: "Stage a file or directory for the next commit. Use '.' for all changes.",
    label: "Git Add",
    parameters: pathParam,
    execute: async (_id, params) => {
      await git(["add", params.path], resolveCwd(params?.cwd));
      return textResult(`Staged: ${params.path}`);
    },
  },

  {
    name: "git_commit",
    description: "Create a new commit with staged changes.",
    label: "Git Commit",
    parameters: commitParams,
    execute: async (_id, params) =>
      textResult(await git(["commit", "-m", params.message], resolveCwd(params?.cwd))),
  },

  {
    name: "git_push",
    description: "Push commits to a remote repository.",
    label: "Git Push",
    parameters: pushParams,
    execute: async (_id, params) => {
      const args = ["push"];
      if (params?.setUpstream) args.push("-u");
      if (params?.remote) args.push(params.remote);
      if (params?.branch) args.push(params.branch);
      return textResult(await git(args, resolveCwd(params?.cwd)));
    },
  },

  {
    name: "git_pull",
    description: "Pull changes from a remote repository.",
    label: "Git Pull",
    parameters: pullParams,
    execute: async (_id, params) => {
      const args = ["pull"];
      if (params?.remote) args.push(params.remote);
      if (params?.branch) args.push(params.branch);
      return textResult(await git(args, resolveCwd(params?.cwd)));
    },
  },

  {
    name: "git_branch",
    description: "List branches or create/switch to a branch.",
    label: "Git Branch",
    parameters: branchParams,
    execute: async (_id, params) => {
      const dir = resolveCwd(params?.cwd);
      if (params?.create) {
        await git(["checkout", "-b", params.name], dir);
        return textResult(`Created and switched to branch: ${params.name}`);
      }
      await git(["checkout", params.name], dir);
      return textResult(`Switched to branch: ${params.name}`);
    },
  },

  {
    name: "git_branch_list",
    description: "List all local and remote branches.",
    label: "Git Branch List",
    parameters: cwdOnlyParams,
    execute: async (_id, params) =>
      textResult(await git(["branch", "-a"], resolveCwd(params?.cwd))),
  },

  {
    name: "git_show",
    description: "Show the contents of a specific commit.",
    label: "Git Show",
    parameters: {
      type: "object",
      properties: {
        ref: { type: "string", description: "Commit hash, branch, or tag (default: HEAD)" },
        ...cwdParam,
      },
      required: [],
    },
    execute: async (_id, params) =>
      textResult(await git(["show", params?.ref || "HEAD", "--stat"], resolveCwd(params?.cwd))),
  },

  {
    name: "git_clone",
    description:
      "Clone a repository into the workspace. The repo is placed in a subdirectory " +
      "named after the repository (e.g. 'myrepo'). Use the returned directory name " +
      "as the `cwd` parameter for subsequent git commands.",
    label: "Git Clone",
    parameters: {
      type: "object",
      required: ["url"],
      properties: {
        url: { type: "string", description: "Repository URL to clone" },
        directory: { type: "string", description: "Target directory name (optional — derived from URL)" },
      },
    },
    execute: async (_id, params) => {
      const dirName =
        params.directory ||
        params.url
          .replace(/\.git$/, "")
          .split("/")
          .pop();
      const targetPath = resolve(WORKSPACE, dirName);
      const args = ["clone", params.url, targetPath];
      await git(args, WORKSPACE);
      return textResult(
        `Cloned ${params.url} into ${targetPath}.\n` +
        `Use cwd: "${dirName}" in subsequent git commands to operate on this repo.`
      );
    },
  },

  {
    name: "git_clone_or_pull",
    description:
      "Clone a repository if it doesn't exist locally, or pull latest changes if it does. " +
      "Use this instead of git_clone when the repo may already be present from a previous session. " +
      "Returns the directory name to use as `cwd` in subsequent git commands.",
    label: "Git Clone or Pull",
    parameters: {
      type: "object",
      required: ["url"],
      properties: {
        url: { type: "string", description: "Repository URL to clone" },
        directory: {
          type: "string",
          description:
            "Target directory name (optional — derived from URL if omitted)",
        },
        branch: {
          type: "string",
          description: "Branch to checkout/pull (optional — uses default branch if omitted)",
        },
      },
    },
    execute: async (_id, params) => {
      // Derive directory name from URL if not provided
      // e.g. https://gitlab.com/samyn92/homecluster.git → homecluster
      const dirName =
        params.directory ||
        params.url
          .replace(/\.git$/, "")
          .split("/")
          .pop();
      const targetPath = resolve(WORKSPACE, dirName);

      // Check if the directory already exists and contains a git repo
      if (existsSync(`${targetPath}/.git`)) {
        // Repo exists — pull latest changes
        const pullArgs = ["pull"];
        if (params.branch) {
          pullArgs.push("origin", params.branch);
        }
        const out = await git(pullArgs, targetPath);
        return textResult(
          `Repository already exists at ${targetPath}. Pulled latest changes.\n${out}\n` +
          `Use cwd: "${dirName}" in subsequent git commands to operate on this repo.`
        );
      }

      // Repo does not exist — clone it
      const cloneArgs = ["clone", params.url, targetPath];
      if (params.branch) {
        cloneArgs.splice(1, 0, "--branch", params.branch);
      }
      const out = await git(cloneArgs, WORKSPACE);
      return textResult(
        `Cloned ${params.url} into ${targetPath}.\n${out}\n` +
        `Use cwd: "${dirName}" in subsequent git commands to operate on this repo.`
      );
    },
  },
];

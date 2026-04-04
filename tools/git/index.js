/**
 * Git tool package for Pi Agents.
 *
 * Provides git operations (status, diff, log, add, commit, push, etc.) as AgentTool[]
 * that can be referenced via toolRefs in a PiAgent CRD.
 *
 * This module is self-contained — no external dependencies. It shells out to
 * the `git` CLI which must be available in the agent's container image.
 *
 * @module agent-tools/git
 */

import { execFile } from "node:child_process";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

/** Run a git command and return stdout. Rejects on non-zero exit. */
function git(args, cwd = process.env.WORKSPACE || "/workspace") {
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
// Parameter schemas (plain JSON Schema — no TypeBox dependency needed)
// ---------------------------------------------------------------------------

const noParams = { type: "object", properties: {}, required: [] };

const pathParam = {
  type: "object",
  required: ["path"],
  properties: {
    path: { type: "string", description: "File or directory path (relative to workspace root)" },
  },
};

const commitParams = {
  type: "object",
  required: ["message"],
  properties: {
    message: { type: "string", description: "Commit message" },
  },
};

const logParams = {
  type: "object",
  properties: {
    count: { type: "number", description: "Number of commits to show (default: 20)" },
    oneline: { type: "boolean", description: "Use --oneline format (default: true)" },
  },
  required: [],
};

const branchParams = {
  type: "object",
  properties: {
    name: { type: "string", description: "Branch name to create or switch to" },
    create: { type: "boolean", description: "Create the branch if it doesn't exist" },
  },
  required: ["name"],
};

const diffParams = {
  type: "object",
  properties: {
    staged: { type: "boolean", description: "Show only staged changes (--cached)" },
    path: { type: "string", description: "Limit diff to a specific path" },
  },
  required: [],
};

const pushParams = {
  type: "object",
  properties: {
    remote: { type: "string", description: "Remote name (default: origin)" },
    branch: { type: "string", description: "Branch to push (default: current)" },
    setUpstream: { type: "boolean", description: "Set upstream tracking (-u)" },
  },
  required: [],
};

const pullParams = {
  type: "object",
  properties: {
    remote: { type: "string", description: "Remote name (default: origin)" },
    branch: { type: "string", description: "Branch to pull (default: current)" },
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
    parameters: noParams,
    execute: async () => textResult(await git(["status"])),
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
      return textResult(await git(args));
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
      return textResult(await git(args));
    },
  },

  {
    name: "git_add",
    description: "Stage a file or directory for the next commit. Use '.' for all changes.",
    label: "Git Add",
    parameters: pathParam,
    execute: async (_id, params) => {
      await git(["add", params.path]);
      return textResult(`Staged: ${params.path}`);
    },
  },

  {
    name: "git_commit",
    description: "Create a new commit with staged changes.",
    label: "Git Commit",
    parameters: commitParams,
    execute: async (_id, params) => textResult(await git(["commit", "-m", params.message])),
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
      return textResult(await git(args));
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
      return textResult(await git(args));
    },
  },

  {
    name: "git_branch",
    description: "List branches or create/switch to a branch.",
    label: "Git Branch",
    parameters: branchParams,
    execute: async (_id, params) => {
      if (params?.create) {
        await git(["checkout", "-b", params.name]);
        return textResult(`Created and switched to branch: ${params.name}`);
      }
      await git(["checkout", params.name]);
      return textResult(`Switched to branch: ${params.name}`);
    },
  },

  {
    name: "git_branch_list",
    description: "List all local and remote branches.",
    label: "Git Branch List",
    parameters: noParams,
    execute: async () => textResult(await git(["branch", "-a"])),
  },

  {
    name: "git_show",
    description: "Show the contents of a specific commit.",
    label: "Git Show",
    parameters: {
      type: "object",
      properties: {
        ref: { type: "string", description: "Commit hash, branch, or tag (default: HEAD)" },
      },
      required: [],
    },
    execute: async (_id, params) => textResult(await git(["show", params?.ref || "HEAD", "--stat"])),
  },

  {
    name: "git_clone",
    description: "Clone a repository into the workspace.",
    label: "Git Clone",
    parameters: {
      type: "object",
      required: ["url"],
      properties: {
        url: { type: "string", description: "Repository URL to clone" },
        directory: { type: "string", description: "Target directory (optional)" },
      },
    },
    execute: async (_id, params) => {
      const args = ["clone", params.url];
      if (params.directory) args.push(params.directory);
      return textResult(await git(args, "/workspace"));
    },
  },
];

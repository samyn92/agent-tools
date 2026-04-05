/**
 * GitHub tool package for Pi Agents and OpenCode agents (via tool-bridge).
 *
 * Provides GitHub API operations (PRs, issues, comments, check runs, workflows)
 * as AgentTool[] that can be referenced via toolRefs in a PiAgent CRD or loaded
 * by the tool-bridge MCP server for OpenCode agents.
 *
 * Self-contained — no external dependencies. Uses Node.js built-in `fetch`
 * (available since Node 18) to call the GitHub REST API v3.
 *
 * Required environment variables:
 *   GITHUB_TOKEN  — Personal access token or fine-grained token with appropriate scopes
 *
 * Optional environment variables:
 *   GITHUB_API_URL — GitHub API base URL (default: https://api.github.com)
 *
 * @module agent-tools/github
 */

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const API_BASE = (process.env.GITHUB_API_URL || "https://api.github.com").replace(/\/+$/, "");

function getToken() {
  const token = process.env.GITHUB_TOKEN || process.env.GH_TOKEN;
  if (!token) throw new Error("GITHUB_TOKEN environment variable is required");
  return token;
}

/**
 * Make an authenticated GitHub API request.
 * @param {string} path  API path (e.g., "/repos/owner/repo/pulls")
 * @param {object} opts  Fetch options (method, body, etc.)
 * @returns {Promise<any>} Parsed JSON response
 */
async function github(path, opts = {}) {
  const url = `${API_BASE}${path}`;
  const headers = {
    Authorization: `Bearer ${getToken()}`,
    Accept: "application/vnd.github+json",
    "X-GitHub-Api-Version": "2022-11-28",
    "Content-Type": "application/json",
    ...(opts.headers || {}),
  };

  const res = await fetch(url, { ...opts, headers });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`GitHub API ${opts.method || "GET"} ${path} -> ${res.status}: ${body}`);
  }

  // Handle 204 No Content
  if (res.status === 204) return null;
  return res.json();
}

/** Wrap a string result into AgentToolResult format. */
function textResult(text) {
  return {
    content: [{ type: "text", text: text || "(empty)" }],
    details: {},
  };
}

/** Format a pull request for display. */
function formatPR(pr) {
  return [
    `PR #${pr.number}: ${pr.title}`,
    `  State: ${pr.state}${pr.merged ? " (merged)" : ""} | Author: ${pr.user?.login || "unknown"}`,
    `  Base: ${pr.base?.ref} <- Head: ${pr.head?.ref}`,
    `  URL: ${pr.html_url}`,
    pr.body ? `  Description: ${pr.body.substring(0, 200)}${pr.body.length > 200 ? "..." : ""}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

/** Format an issue for display. */
function formatIssue(issue) {
  return [
    `#${issue.number}: ${issue.title}`,
    `  State: ${issue.state} | Author: ${issue.user?.login || "unknown"}`,
    `  Labels: ${issue.labels?.map((l) => l.name).join(", ") || "none"}`,
    `  URL: ${issue.html_url}`,
  ].join("\n");
}

/** Format a check run for display. */
function formatCheckRun(check) {
  return [
    `  ${check.name}: ${check.status}${check.conclusion ? ` (${check.conclusion})` : ""}`,
    check.html_url ? `    URL: ${check.html_url}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

// ---------------------------------------------------------------------------
// Parameter schemas (plain JSON Schema)
// ---------------------------------------------------------------------------

const createPRParams = {
  type: "object",
  required: ["owner", "repo", "head", "base", "title"],
  properties: {
    owner: { type: "string", description: "Repository owner (user or org)" },
    repo: { type: "string", description: "Repository name" },
    head: { type: "string", description: "Branch containing changes (e.g., 'feat/my-feature')" },
    base: { type: "string", description: "Branch to merge into (e.g., 'main')" },
    title: { type: "string", description: "Pull request title" },
    body: { type: "string", description: "Pull request description (Markdown)" },
    draft: { type: "boolean", description: "Create as draft PR (default: false)" },
  },
};

const getPRParams = {
  type: "object",
  required: ["owner", "repo", "pullNumber"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    pullNumber: { type: "number", description: "Pull request number" },
  },
};

const getPRDiffParams = {
  type: "object",
  required: ["owner", "repo", "pullNumber"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    pullNumber: { type: "number", description: "Pull request number" },
  },
};

const listPRsParams = {
  type: "object",
  required: ["owner", "repo"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    state: { type: "string", description: "Filter by state: open, closed, all (default: open)" },
    perPage: { type: "number", description: "Results per page (default: 30, max: 100)" },
  },
};

const addPRCommentParams = {
  type: "object",
  required: ["owner", "repo", "pullNumber", "body"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    pullNumber: { type: "number", description: "Pull request number" },
    body: { type: "string", description: "Comment body (Markdown supported)" },
  },
};

const listIssuesParams = {
  type: "object",
  required: ["owner", "repo"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    state: { type: "string", description: "Filter by state: open, closed, all (default: open)" },
    labels: { type: "string", description: "Comma-separated label names to filter by" },
    perPage: { type: "number", description: "Results per page (default: 30, max: 100)" },
  },
};

const getIssueParams = {
  type: "object",
  required: ["owner", "repo", "issueNumber"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    issueNumber: { type: "number", description: "Issue number" },
  },
};

const addIssueCommentParams = {
  type: "object",
  required: ["owner", "repo", "issueNumber", "body"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    issueNumber: { type: "number", description: "Issue number" },
    body: { type: "string", description: "Comment body (Markdown supported)" },
  },
};

const getCheckRunsParams = {
  type: "object",
  required: ["owner", "repo", "ref"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    ref: { type: "string", description: "Git ref (SHA, branch, or tag)" },
  },
};

const getWorkflowRunsParams = {
  type: "object",
  required: ["owner", "repo"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    branch: { type: "string", description: "Filter by branch name" },
    status: { type: "string", description: "Filter: queued, in_progress, completed, action_required, etc." },
    perPage: { type: "number", description: "Results per page (default: 10, max: 100)" },
  },
};

const getRepoParams = {
  type: "object",
  required: ["owner", "repo"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
  },
};

const listBranchesParams = {
  type: "object",
  required: ["owner", "repo"],
  properties: {
    owner: { type: "string", description: "Repository owner" },
    repo: { type: "string", description: "Repository name" },
    perPage: { type: "number", description: "Results per page (default: 30, max: 100)" },
  },
};

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

export const tools = [
  {
    name: "github_create_pr",
    description:
      "Create a new pull request in a GitHub repository. Returns the PR URL and number on success.",
    label: "Create PR",
    parameters: createPRParams,
    execute: async (_id, params) => {
      const pr = await github(`/repos/${params.owner}/${params.repo}/pulls`, {
        method: "POST",
        body: JSON.stringify({
          head: params.head,
          base: params.base,
          title: params.title,
          body: params.body || "",
          draft: params.draft || false,
        }),
      });
      return textResult(`Created PR #${pr.number}: ${pr.title}\nURL: ${pr.html_url}`);
    },
  },

  {
    name: "github_get_pr",
    description: "Get details of a specific pull request.",
    label: "Get PR",
    parameters: getPRParams,
    execute: async (_id, params) => {
      const pr = await github(`/repos/${params.owner}/${params.repo}/pulls/${params.pullNumber}`);
      return textResult(formatPR(pr));
    },
  },

  {
    name: "github_get_pr_diff",
    description:
      "Get the full diff of a pull request. Returns the unified diff for all changed files.",
    label: "Get PR Diff",
    parameters: getPRDiffParams,
    execute: async (_id, params) => {
      const diff = await fetch(
        `${API_BASE}/repos/${params.owner}/${params.repo}/pulls/${params.pullNumber}`,
        {
          headers: {
            Authorization: `Bearer ${getToken()}`,
            Accept: "application/vnd.github.v3.diff",
            "X-GitHub-Api-Version": "2022-11-28",
          },
        }
      );

      if (!diff.ok) {
        const body = await diff.text();
        throw new Error(`GitHub API GET PR diff -> ${diff.status}: ${body}`);
      }

      const diffText = await diff.text();
      if (!diffText.trim()) return textResult("No changes in this pull request.");
      return textResult(diffText);
    },
  },

  {
    name: "github_list_prs",
    description: "List pull requests in a repository, filtered by state.",
    label: "List PRs",
    parameters: listPRsParams,
    execute: async (_id, params) => {
      const state = params.state || "open";
      const perPage = Math.min(params.perPage || 30, 100);
      const prs = await github(
        `/repos/${params.owner}/${params.repo}/pulls?state=${state}&per_page=${perPage}`
      );

      if (!prs.length) return textResult(`No ${state} pull requests.`);
      return textResult(prs.map(formatPR).join("\n\n"));
    },
  },

  {
    name: "github_add_pr_comment",
    description: "Add a comment to a pull request (uses the issue comment endpoint).",
    label: "Add PR Comment",
    parameters: addPRCommentParams,
    execute: async (_id, params) => {
      const comment = await github(
        `/repos/${params.owner}/${params.repo}/issues/${params.pullNumber}/comments`,
        {
          method: "POST",
          body: JSON.stringify({ body: params.body }),
        }
      );
      return textResult(`Added comment to PR #${params.pullNumber} (comment ID: ${comment.id})`);
    },
  },

  {
    name: "github_list_issues",
    description: "List issues in a repository, filtered by state and/or labels.",
    label: "List Issues",
    parameters: listIssuesParams,
    execute: async (_id, params) => {
      const state = params.state || "open";
      const perPage = Math.min(params.perPage || 30, 100);
      let path = `/repos/${params.owner}/${params.repo}/issues?state=${state}&per_page=${perPage}`;
      if (params.labels) path += `&labels=${encodeURIComponent(params.labels)}`;

      const issues = await github(path);
      // Filter out pull requests (GitHub API returns PRs in the issues endpoint)
      const realIssues = issues.filter((i) => !i.pull_request);
      if (!realIssues.length) return textResult(`No ${state} issues.`);
      return textResult(realIssues.map(formatIssue).join("\n\n"));
    },
  },

  {
    name: "github_get_issue",
    description: "Get details of a specific issue, including description.",
    label: "Get Issue",
    parameters: getIssueParams,
    execute: async (_id, params) => {
      const issue = await github(
        `/repos/${params.owner}/${params.repo}/issues/${params.issueNumber}`
      );
      return textResult(
        [formatIssue(issue), "", "Description:", issue.body || "(no description)"].join("\n")
      );
    },
  },

  {
    name: "github_add_issue_comment",
    description: "Add a comment to an issue.",
    label: "Add Issue Comment",
    parameters: addIssueCommentParams,
    execute: async (_id, params) => {
      const comment = await github(
        `/repos/${params.owner}/${params.repo}/issues/${params.issueNumber}/comments`,
        {
          method: "POST",
          body: JSON.stringify({ body: params.body }),
        }
      );
      return textResult(
        `Added comment to issue #${params.issueNumber} (comment ID: ${comment.id})`
      );
    },
  },

  {
    name: "github_get_check_runs",
    description:
      "Get CI/CD check runs for a specific git ref (commit SHA, branch, or tag).",
    label: "Get Check Runs",
    parameters: getCheckRunsParams,
    execute: async (_id, params) => {
      const result = await github(
        `/repos/${params.owner}/${params.repo}/commits/${params.ref}/check-runs`
      );

      if (!result.check_runs?.length) return textResult("No check runs found for this ref.");

      const summary = [
        `${result.total_count} check runs for ref: ${params.ref}`,
        "",
        ...result.check_runs.map(formatCheckRun),
      ];

      return textResult(summary.join("\n"));
    },
  },

  {
    name: "github_get_workflow_runs",
    description:
      "Get recent GitHub Actions workflow runs for a repository, optionally filtered by branch or status.",
    label: "Get Workflow Runs",
    parameters: getWorkflowRunsParams,
    execute: async (_id, params) => {
      const perPage = Math.min(params.perPage || 10, 100);
      let path = `/repos/${params.owner}/${params.repo}/actions/runs?per_page=${perPage}`;
      if (params.branch) path += `&branch=${encodeURIComponent(params.branch)}`;
      if (params.status) path += `&status=${encodeURIComponent(params.status)}`;

      const result = await github(path);

      if (!result.workflow_runs?.length) return textResult("No workflow runs found.");

      const runs = result.workflow_runs.map((run) =>
        [
          `Run #${run.run_number}: ${run.name}`,
          `  Status: ${run.status}${run.conclusion ? ` (${run.conclusion})` : ""}`,
          `  Branch: ${run.head_branch} | Event: ${run.event}`,
          `  SHA: ${run.head_sha?.substring(0, 8)}`,
          `  URL: ${run.html_url}`,
        ].join("\n")
      );

      return textResult(runs.join("\n\n"));
    },
  },

  {
    name: "github_get_repo",
    description: "Get repository metadata (description, default branch, stars, language, etc.).",
    label: "Get Repo",
    parameters: getRepoParams,
    execute: async (_id, params) => {
      const repo = await github(`/repos/${params.owner}/${params.repo}`);
      return textResult(
        [
          `${repo.full_name}`,
          `  Description: ${repo.description || "(none)"}`,
          `  Default branch: ${repo.default_branch}`,
          `  Language: ${repo.language || "unknown"}`,
          `  Stars: ${repo.stargazers_count} | Forks: ${repo.forks_count}`,
          `  Private: ${repo.private}`,
          `  URL: ${repo.html_url}`,
        ].join("\n")
      );
    },
  },

  {
    name: "github_list_branches",
    description: "List branches in a repository.",
    label: "List Branches",
    parameters: listBranchesParams,
    execute: async (_id, params) => {
      const perPage = Math.min(params.perPage || 30, 100);
      const branches = await github(
        `/repos/${params.owner}/${params.repo}/branches?per_page=${perPage}`
      );

      if (!branches.length) return textResult("No branches found.");

      const list = branches.map(
        (b) => `  ${b.name}${b.protected ? " (protected)" : ""} @ ${b.commit?.sha?.substring(0, 8)}`
      );

      return textResult([`${branches.length} branches:`, ...list].join("\n"));
    },
  },
];

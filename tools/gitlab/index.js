/**
 * GitLab tool package for Pi Agents.
 *
 * Provides GitLab API operations (MR creation, notes, issues, pipelines) as
 * AgentTool[] that can be referenced via toolRefs in a PiAgent CRD.
 *
 * Self-contained — no external dependencies. Uses Node.js built-in `fetch`
 * (available since Node 18) to call the GitLab REST API v4.
 *
 * Required environment variables:
 *   GITLAB_TOKEN  — Personal access token or project token with api scope
 *   GITLAB_URL    — GitLab instance URL (default: https://gitlab.com)
 *
 * @module agent-tools/gitlab
 */

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const GITLAB_URL = (process.env.GITLAB_URL || "https://gitlab.com").replace(/\/+$/, "");
const API_BASE = `${GITLAB_URL}/api/v4`;

function getToken() {
  const token = process.env.GITLAB_TOKEN;
  if (!token) throw new Error("GITLAB_TOKEN environment variable is required");
  return token;
}

/**
 * Make an authenticated GitLab API request.
 * @param {string} path  API path (e.g., "/projects/123/merge_requests")
 * @param {object} opts  Fetch options (method, body, etc.)
 * @returns {Promise<any>} Parsed JSON response
 */
async function gitlab(path, opts = {}) {
  const url = `${API_BASE}${path}`;
  const headers = {
    "PRIVATE-TOKEN": getToken(),
    "Content-Type": "application/json",
    ...(opts.headers || {}),
  };

  const res = await fetch(url, { ...opts, headers });

  if (!res.ok) {
    const body = await res.text();
    throw new Error(`GitLab API ${opts.method || "GET"} ${path} → ${res.status}: ${body}`);
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

/** URL-encode a project path for GitLab API (e.g., "group/project" → "group%2Fproject"). */
function encodeProject(project) {
  // If it's a numeric ID, return as-is
  if (/^\d+$/.test(project)) return project;
  return encodeURIComponent(project);
}

/** Format a merge request for display. */
function formatMR(mr) {
  return [
    `MR !${mr.iid}: ${mr.title}`,
    `  State: ${mr.state} | Author: ${mr.author?.username || "unknown"}`,
    `  Source: ${mr.source_branch} → Target: ${mr.target_branch}`,
    `  URL: ${mr.web_url}`,
    mr.description ? `  Description: ${mr.description.substring(0, 200)}${mr.description.length > 200 ? "..." : ""}` : "",
  ]
    .filter(Boolean)
    .join("\n");
}

/** Format an issue for display. */
function formatIssue(issue) {
  return [
    `#${issue.iid}: ${issue.title}`,
    `  State: ${issue.state} | Author: ${issue.author?.username || "unknown"}`,
    `  Labels: ${issue.labels?.join(", ") || "none"}`,
    `  URL: ${issue.web_url}`,
  ].join("\n");
}

// ---------------------------------------------------------------------------
// Parameter schemas (plain JSON Schema)
// ---------------------------------------------------------------------------

const createMRParams = {
  type: "object",
  required: ["project", "sourceBranch", "targetBranch", "title"],
  properties: {
    project: { type: "string", description: "Project ID or path (e.g., 'mygroup/myproject' or '42')" },
    sourceBranch: { type: "string", description: "Source branch name" },
    targetBranch: { type: "string", description: "Target branch name (e.g., 'main')" },
    title: { type: "string", description: "Merge request title" },
    description: { type: "string", description: "Merge request description (Markdown)" },
    labels: { type: "string", description: "Comma-separated labels" },
    removeSourceBranch: { type: "boolean", description: "Delete source branch after merge (default: true)" },
  },
};

const getMRParams = {
  type: "object",
  required: ["project", "mrIid"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    mrIid: { type: "number", description: "Merge request IID (the ! number)" },
    includeChanges: { type: "boolean", description: "Include file changes/diff (default: false)" },
  },
};

const addMRNoteParams = {
  type: "object",
  required: ["project", "mrIid", "body"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    mrIid: { type: "number", description: "Merge request IID" },
    body: { type: "string", description: "Note body (Markdown supported)" },
  },
};

const listMRsParams = {
  type: "object",
  required: ["project"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    state: { type: "string", description: "Filter by state: opened, closed, merged, all (default: opened)" },
    perPage: { type: "number", description: "Results per page (default: 20, max: 100)" },
  },
};

const listIssuesParams = {
  type: "object",
  required: ["project"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    state: { type: "string", description: "Filter by state: opened, closed, all (default: opened)" },
    labels: { type: "string", description: "Comma-separated label names to filter by" },
    perPage: { type: "number", description: "Results per page (default: 20, max: 100)" },
  },
};

const getIssueParams = {
  type: "object",
  required: ["project", "issueIid"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    issueIid: { type: "number", description: "Issue IID (the # number)" },
  },
};

const addIssueNoteParams = {
  type: "object",
  required: ["project", "issueIid", "body"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    issueIid: { type: "number", description: "Issue IID" },
    body: { type: "string", description: "Comment body (Markdown supported)" },
  },
};

const getPipelineParams = {
  type: "object",
  required: ["project"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    ref: { type: "string", description: "Branch or tag name to get pipeline for (default: latest)" },
  },
};

const getMRDiffParams = {
  type: "object",
  required: ["project", "mrIid"],
  properties: {
    project: { type: "string", description: "Project ID or path" },
    mrIid: { type: "number", description: "Merge request IID" },
  },
};

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

export const tools = [
  {
    name: "gitlab_create_mr",
    description:
      "Create a new merge request in a GitLab project. Returns the MR URL and IID on success.",
    label: "Create MR",
    parameters: createMRParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const mr = await gitlab(`/projects/${proj}/merge_requests`, {
        method: "POST",
        body: JSON.stringify({
          source_branch: params.sourceBranch,
          target_branch: params.targetBranch,
          title: params.title,
          description: params.description || "",
          labels: params.labels || "",
          remove_source_branch: params.removeSourceBranch !== false,
        }),
      });
      return textResult(`Created MR !${mr.iid}: ${mr.title}\nURL: ${mr.web_url}`);
    },
  },

  {
    name: "gitlab_get_mr",
    description: "Get details of a specific merge request, optionally including file changes.",
    label: "Get MR",
    parameters: getMRParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const mr = await gitlab(`/projects/${proj}/merge_requests/${params.mrIid}`);
      let result = formatMR(mr);

      if (params.includeChanges) {
        const changes = await gitlab(`/projects/${proj}/merge_requests/${params.mrIid}/changes`);
        if (changes.changes?.length) {
          result += "\n\nChanged files:\n";
          result += changes.changes
            .map((c) => `  ${c.new_path} (+${c.diff?.split("\n+").length - 1 || 0}/-${c.diff?.split("\n-").length - 1 || 0})`)
            .join("\n");
        }
      }

      return textResult(result);
    },
  },

  {
    name: "gitlab_get_mr_diff",
    description: "Get the full diff of a merge request. Returns the unified diff for all changed files.",
    label: "Get MR Diff",
    parameters: getMRDiffParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const changes = await gitlab(`/projects/${proj}/merge_requests/${params.mrIid}/changes`);

      if (!changes.changes?.length) {
        return textResult("No changes in this merge request.");
      }

      const diffs = changes.changes.map((c) => {
        const header = `--- a/${c.old_path}\n+++ b/${c.new_path}`;
        return `${header}\n${c.diff || "(binary or empty)"}`;
      });

      return textResult(diffs.join("\n\n"));
    },
  },

  {
    name: "gitlab_add_mr_note",
    description: "Add a comment (note) to a merge request.",
    label: "Add MR Note",
    parameters: addMRNoteParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const note = await gitlab(`/projects/${proj}/merge_requests/${params.mrIid}/notes`, {
        method: "POST",
        body: JSON.stringify({ body: params.body }),
      });
      return textResult(`Added note to MR !${params.mrIid} (note ID: ${note.id})`);
    },
  },

  {
    name: "gitlab_list_mrs",
    description: "List merge requests in a project, filtered by state.",
    label: "List MRs",
    parameters: listMRsParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const state = params.state || "opened";
      const perPage = Math.min(params.perPage || 20, 100);
      const mrs = await gitlab(`/projects/${proj}/merge_requests?state=${state}&per_page=${perPage}`);

      if (!mrs.length) return textResult(`No ${state} merge requests.`);
      return textResult(mrs.map(formatMR).join("\n\n"));
    },
  },

  {
    name: "gitlab_list_issues",
    description: "List issues in a project, filtered by state and/or labels.",
    label: "List Issues",
    parameters: listIssuesParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const state = params.state || "opened";
      const perPage = Math.min(params.perPage || 20, 100);
      let path = `/projects/${proj}/issues?state=${state}&per_page=${perPage}`;
      if (params.labels) path += `&labels=${encodeURIComponent(params.labels)}`;

      const issues = await gitlab(path);
      if (!issues.length) return textResult(`No ${state} issues.`);
      return textResult(issues.map(formatIssue).join("\n\n"));
    },
  },

  {
    name: "gitlab_get_issue",
    description: "Get details of a specific issue.",
    label: "Get Issue",
    parameters: getIssueParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const issue = await gitlab(`/projects/${proj}/issues/${params.issueIid}`);
      return textResult(
        [
          formatIssue(issue),
          "",
          "Description:",
          issue.description || "(no description)",
        ].join("\n")
      );
    },
  },

  {
    name: "gitlab_add_issue_note",
    description: "Add a comment to an issue.",
    label: "Add Issue Note",
    parameters: addIssueNoteParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      const note = await gitlab(`/projects/${proj}/issues/${params.issueIid}/notes`, {
        method: "POST",
        body: JSON.stringify({ body: params.body }),
      });
      return textResult(`Added note to issue #${params.issueIid} (note ID: ${note.id})`);
    },
  },

  {
    name: "gitlab_get_pipeline",
    description:
      "Get the latest pipeline status for a project or a specific branch/tag.",
    label: "Get Pipeline",
    parameters: getPipelineParams,
    execute: async (_id, params) => {
      const proj = encodeProject(params.project);
      let path = `/projects/${proj}/pipelines?per_page=1`;
      if (params.ref) path += `&ref=${encodeURIComponent(params.ref)}`;

      const pipelines = await gitlab(path);
      if (!pipelines.length) return textResult("No pipelines found.");

      const p = pipelines[0];
      return textResult(
        [
          `Pipeline #${p.id}`,
          `  Status: ${p.status}`,
          `  Ref: ${p.ref}`,
          `  SHA: ${p.sha?.substring(0, 8)}`,
          `  Created: ${p.created_at}`,
          `  URL: ${p.web_url}`,
        ].join("\n")
      );
    },
  },
];

/**
 * File tool package for Pi Agents.
 *
 * Provides filesystem operations (read, write, list, search, mkdir) as AgentTool[]
 * that can be referenced via toolRefs in a PiAgent CRD.
 *
 * Self-contained — no external dependencies. Uses Node.js built-in `fs` and
 * `child_process` modules.
 *
 * All paths are resolved relative to WORKSPACE (default: /workspace).
 *
 * @module agent-tools/file
 */

import { readFile, writeFile, mkdir, readdir, stat } from "node:fs/promises";
import { execFile } from "node:child_process";
import { join, resolve, relative } from "node:path";

// ---------------------------------------------------------------------------
// Helpers
// ---------------------------------------------------------------------------

const WORKSPACE = process.env.WORKSPACE || "/workspace";

/** Resolve a path safely within the workspace. Prevents path traversal. */
function safePath(userPath) {
  const resolved = resolve(WORKSPACE, userPath);
  if (!resolved.startsWith(WORKSPACE)) {
    throw new Error(`Path traversal rejected: ${userPath} resolves outside workspace`);
  }
  return resolved;
}

/** Wrap a string result into AgentToolResult format. */
function textResult(text) {
  return {
    content: [{ type: "text", text: text || "(empty)" }],
    details: {},
  };
}

/**
 * Run `grep` via child_process. Returns stdout or a message when no matches.
 */
function grepFiles(pattern, searchPath, options = []) {
  return new Promise((resolve, reject) => {
    const args = ["-r", "-n", "--include=*", ...options, pattern, searchPath];
    execFile("grep", args, { maxBuffer: 10 * 1024 * 1024 }, (err, stdout, stderr) => {
      // grep exits 1 when no matches — that's not an error
      if (err && err.code !== 1) {
        reject(new Error(`grep failed: ${stderr || err.message}`));
      } else {
        resolve(stdout || "No matches found.");
      }
    });
  });
}

// ---------------------------------------------------------------------------
// Parameter schemas (plain JSON Schema)
// ---------------------------------------------------------------------------

const readFileParams = {
  type: "object",
  required: ["path"],
  properties: {
    path: { type: "string", description: "File path relative to workspace root" },
    offset: { type: "number", description: "Line number to start reading from (1-indexed, default: 1)" },
    limit: { type: "number", description: "Maximum number of lines to return (default: 500)" },
  },
};

const writeFileParams = {
  type: "object",
  required: ["path", "content"],
  properties: {
    path: { type: "string", description: "File path relative to workspace root" },
    content: { type: "string", description: "Content to write to the file" },
    createDirectories: { type: "boolean", description: "Create parent directories if they don't exist (default: true)" },
  },
};

const editFileParams = {
  type: "object",
  required: ["path", "oldString", "newString"],
  properties: {
    path: { type: "string", description: "File path relative to workspace root" },
    oldString: { type: "string", description: "Exact string to find and replace" },
    newString: { type: "string", description: "Replacement string" },
    replaceAll: { type: "boolean", description: "Replace all occurrences (default: false — replaces first only)" },
  },
};

const listFilesParams = {
  type: "object",
  properties: {
    path: { type: "string", description: "Directory path relative to workspace root (default: '.')" },
    recursive: { type: "boolean", description: "List files recursively (default: false)" },
    maxDepth: { type: "number", description: "Maximum recursion depth (default: 3)" },
  },
  required: [],
};

const searchFilesParams = {
  type: "object",
  required: ["pattern"],
  properties: {
    pattern: { type: "string", description: "Search pattern (regex supported by grep)" },
    path: { type: "string", description: "Directory to search in, relative to workspace (default: '.')" },
    include: { type: "string", description: "File glob filter, e.g. '*.go' or '*.ts'" },
    ignoreCase: { type: "boolean", description: "Case-insensitive search (default: false)" },
  },
};

const mkdirParams = {
  type: "object",
  required: ["path"],
  properties: {
    path: { type: "string", description: "Directory path to create, relative to workspace root" },
  },
};

// ---------------------------------------------------------------------------
// Recursive directory listing helper
// ---------------------------------------------------------------------------

async function listDir(dirPath, depth, maxDepth) {
  const entries = await readdir(dirPath, { withFileTypes: true });
  const results = [];

  for (const entry of entries) {
    // Skip hidden files and common noise
    if (entry.name.startsWith(".") || entry.name === "node_modules") continue;

    const fullPath = join(dirPath, entry.name);
    const relPath = relative(WORKSPACE, fullPath);

    if (entry.isDirectory()) {
      results.push(relPath + "/");
      if (depth < maxDepth) {
        results.push(...(await listDir(fullPath, depth + 1, maxDepth)));
      }
    } else {
      results.push(relPath);
    }
  }

  return results;
}

// ---------------------------------------------------------------------------
// Tools
// ---------------------------------------------------------------------------

export const tools = [
  {
    name: "read_file",
    description:
      "Read a file's contents. Supports offset/limit for reading specific sections of large files. Returns line-numbered output.",
    label: "Read File",
    parameters: readFileParams,
    execute: async (_id, params) => {
      const filePath = safePath(params.path);
      const raw = await readFile(filePath, "utf-8");
      const lines = raw.split("\n");

      const offset = Math.max(1, params?.offset || 1);
      const limit = params?.limit || 500;
      const slice = lines.slice(offset - 1, offset - 1 + limit);

      const numbered = slice.map((line, i) => `${offset + i}: ${line}`).join("\n");
      const total = lines.length;
      const shown = slice.length;

      let header = `${params.path} (${total} lines)`;
      if (shown < total) {
        header += ` — showing lines ${offset}-${offset + shown - 1}`;
      }

      return textResult(`${header}\n\n${numbered}`);
    },
  },

  {
    name: "write_file",
    description:
      "Write content to a file, creating it if it doesn't exist. Creates parent directories by default.",
    label: "Write File",
    parameters: writeFileParams,
    execute: async (_id, params) => {
      const filePath = safePath(params.path);
      const createDirs = params.createDirectories !== false;

      if (createDirs) {
        const dir = filePath.substring(0, filePath.lastIndexOf("/"));
        await mkdir(dir, { recursive: true });
      }

      await writeFile(filePath, params.content, "utf-8");
      return textResult(`Written: ${params.path} (${params.content.length} bytes)`);
    },
  },

  {
    name: "edit_file",
    description:
      "Find and replace a specific string in a file. Use for surgical edits without rewriting the entire file.",
    label: "Edit File",
    parameters: editFileParams,
    execute: async (_id, params) => {
      const filePath = safePath(params.path);
      const content = await readFile(filePath, "utf-8");

      if (!content.includes(params.oldString)) {
        throw new Error(
          `oldString not found in ${params.path}. Make sure it matches exactly (including whitespace and indentation).`
        );
      }

      let newContent;
      if (params.replaceAll) {
        newContent = content.replaceAll(params.oldString, params.newString);
      } else {
        newContent = content.replace(params.oldString, params.newString);
      }

      await writeFile(filePath, newContent, "utf-8");

      const occurrences = content.split(params.oldString).length - 1;
      const replaced = params.replaceAll ? occurrences : 1;
      return textResult(`Edited: ${params.path} (${replaced} replacement${replaced > 1 ? "s" : ""})`);
    },
  },

  {
    name: "list_files",
    description:
      "List files and directories. Supports recursive listing with configurable depth. Skips hidden files and node_modules.",
    label: "List Files",
    parameters: listFilesParams,
    execute: async (_id, params) => {
      const dirPath = safePath(params?.path || ".");
      const recursive = params?.recursive || false;
      const maxDepth = params?.maxDepth || 3;

      if (!recursive) {
        const entries = await readdir(dirPath, { withFileTypes: true });
        const items = entries
          .filter((e) => !e.name.startsWith(".") && e.name !== "node_modules")
          .map((e) => (e.isDirectory() ? e.name + "/" : e.name));
        return textResult(items.join("\n") || "(empty directory)");
      }

      const items = await listDir(dirPath, 1, maxDepth);
      return textResult(items.join("\n") || "(empty directory)");
    },
  },

  {
    name: "search_files",
    description:
      "Search file contents using grep with regex support. Returns matching lines with file paths and line numbers.",
    label: "Search Files",
    parameters: searchFilesParams,
    execute: async (_id, params) => {
      const searchPath = safePath(params?.path || ".");
      const options = [];

      if (params?.ignoreCase) options.push("-i");
      if (params?.include) options.push(`--include=${params.include}`);

      // Exclude common noise
      options.push("--exclude-dir=node_modules", "--exclude-dir=.git", "--exclude-dir=vendor");

      const result = await grepFiles(params.pattern, searchPath, options);

      // Make paths relative to workspace for readability
      const cleaned = result.replace(new RegExp(WORKSPACE + "/", "g"), "");
      return textResult(cleaned);
    },
  },

  {
    name: "create_directory",
    description: "Create a directory (and all parent directories if needed).",
    label: "Create Directory",
    parameters: mkdirParams,
    execute: async (_id, params) => {
      const dirPath = safePath(params.path);
      await mkdir(dirPath, { recursive: true });
      return textResult(`Created: ${params.path}`);
    },
  },
];

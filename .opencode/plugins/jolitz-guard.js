// jolitz guard for OpenCode.
//
// Claude Code runs .claude/hooks/guard-taboos.sh as a PreToolUse
// hook. OpenCode has no such hook, but every tool call passes a
// plugin's "tool.execute.before" first, and an error thrown there
// stops the call. This plugin hands each shell command and each
// file write to the same guard script, in the JSON Claude Code
// would send, and throws when the guard denies. One guard for
// both tools; this file holds no rules of its own.
//
// It fails closed like the hook registration in
// .claude/settings.json ("onFailure": "block"): when the script
// cannot start, times out, exits non-zero or answers with
// something that is not the guard's JSON, the call is stopped.
//
// Mapped (OpenCode 1.19 tool ids and arguments):
//   bash         command, workdir   -> Bash mode
//   edit, write  filePath           -> file mode
//   apply_patch  patchText          -> file mode, once per path
//                                      in its *** Add/Update/
//                                      Delete File and Move to
//                                      lines
// Reads, searches, web and MCP tools pass, as under Claude Code,
// where the hook is registered for Bash and the edit tools only.
// A tool OpenCode renames later is still caught by its arguments:
// any tool with a command string is checked as a shell command,
// any with patchText as a patch, any with a filePath as a write
// unless it is one of the known readers.
//
// Loaded automatically when OpenCode starts inside this checkout,
// unless OPENCODE_DISABLE_PROJECT_CONFIG is set. The guard denies
// the agent any write under .opencode/, to opencode.json(c) and
// under ~/.config/opencode/, so it can neither rewrite this file
// nor add a plugin that changes a call after it. MCP tools pass,
// as under Claude Code. The guard is a backstop against
// mistakes, not a sandbox.

import { spawnSync } from "node:child_process"
import { fileURLToPath } from "node:url"
import path from "node:path"

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..", "..")
const GUARD = path.join(ROOT, ".claude", "hooks", "guard-taboos.sh")
// The same limit as the hook registration in .claude/settings.json.
const TIMEOUT_MS = 15000

function guard(payload, mode) {
  const argv = mode === "file" ? [GUARD, "file"] : [GUARD]
  const run = spawnSync("bash", argv, {
    input: JSON.stringify(payload),
    encoding: "utf8",
    timeout: TIMEOUT_MS,
    env: process.env,
  })
  if (run.error || run.status !== 0 || run.signal) {
    const why = run.error ? run.error.message : `exit ${run.status ?? run.signal}`
    throw new Error(`jolitz guard: the guard could not run (${why}); the call is blocked`)
  }
  const out = (run.stdout || "").trim()
  if (out === "") return
  let decision
  try {
    decision = JSON.parse(out).hookSpecificOutput
  } catch {
    throw new Error("jolitz guard: unreadable answer from the guard; the call is blocked")
  }
  if (!decision || typeof decision !== "object") {
    throw new Error("jolitz guard: unreadable answer from the guard; the call is blocked")
  }
  if (decision.permissionDecision === "deny") {
    throw new Error(decision.permissionDecisionReason || "jolitz guard: denied")
  }
}

function patchPaths(text) {
  const marks = ["*** Add File:", "*** Update File:", "*** Delete File:", "*** Move to:"]
  const found = []
  for (const raw of String(text).split(/\r\n|\r|\n/)) {
    const line = raw.trimStart()
    for (const m of marks) {
      if (line.startsWith(m)) found.push(line.slice(m.length).trim())
    }
  }
  return found
}

const READERS = new Set(["read", "grep", "glob", "list", "lsp"])

export const JolitzGuard = async ({ directory }) => {
  const base = directory || process.cwd()
  const abs = (p) => path.resolve(base, String(p ?? ""))
  return {
    "tool.execute.before": async (input, output) => {
      const tool = input && input.tool
      const args = (output && output.args) || {}
      if (tool === "bash" || typeof args.command === "string") {
        guard(
          {
            tool_name: "Bash",
            cwd: args.workdir ? abs(args.workdir) : base,
            tool_input: { command: String(args.command ?? "") },
          },
          "bash",
        )
      } else if (tool === "apply_patch" || typeof args.patchText === "string") {
        const paths = patchPaths(args.patchText ?? "")
        if (paths.length === 0 && String(args.patchText ?? "").trim() !== "") {
          throw new Error("jolitz guard: no file path found in the patch; the call is blocked")
        }
        // The whole patch rides along as content, so the guard's
        // scan for an override assignment sees every byte written.
        for (const p of paths) {
          guard(
            {
              tool_name: "Write",
              cwd: base,
              tool_input: { file_path: abs(p), content: String(args.patchText) },
            },
            "file",
          )
        }
      } else if (tool === "edit" || tool === "write"
        || (typeof args.filePath === "string" && !READERS.has(tool))) {
        guard(
          {
            tool_name: tool === "edit" ? "Edit" : "Write",
            cwd: base,
            tool_input: {
              file_path: abs(args.filePath),
              // An unknown writer may carry its text in any field.
              content:
                tool === "edit" || tool === "write"
                  ? args.content
                  : JSON.stringify(args),
              new_string: args.newString,
            },
          },
          "file",
        )
      }
    },
  }
}

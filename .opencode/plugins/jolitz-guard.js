// jolitz guard for OpenCode.
//
// Claude Code runs .claude/hooks/guard-taboos.sh as a PreToolUse
// hook. OpenCode has no such hook, but every tool call passes a
// plugin's "before" hook first, and an error thrown there stops
// the call. This plugin hands each shell command and each file
// write to the same guard script, in the JSON Claude Code would
// send, and throws when the guard denies. One guard for both
// tools; this file holds no rules of its own.
//
// It fails closed like the hook registration in
// .claude/settings.json ("onFailure": "block"): when the script
// cannot start, times out, exits non-zero or answers with
// something that is not the guard's JSON, the call is stopped.
//
// Two plugin APIs, one default export (OpenCode 1.15 or newer;
// older 1.x call every export as a function and would not load
// this one):
//   OpenCode 1.x reads default.server(input) and calls the
//     "tool.execute.before"(input, output) hook it returns; the
//     arguments are output.args.
//   OpenCode 2.x reads default.setup(ctx) and registers
//     ctx.tool.hook("execute.before", event); the arguments are
//     event.input. Each version ignores the other's key.
//
// Mapped tool ids and arguments (1.19 / 2.2):
//   bash / shell        command, workdir  -> Bash mode
//   edit, write         filePath / path   -> file mode
//   apply_patch / patch patchText         -> file mode, once per
//                                            path in its *** Add/
//                                            Update/Delete File
//                                            and Move to lines
// Reads, searches, web and MCP tools pass, as under Claude Code,
// where the hook is registered for Bash and the edit tools only.
// A tool OpenCode renames later is still caught by its arguments:
// any tool with a command string is checked as a shell command,
// any with patchText as a patch, any with a path argument as a
// write unless it is one of the known readers, every path
// argument on its own. A known tool missing its argument is
// denied.
//
// Loaded automatically when OpenCode starts inside this checkout
// (.opencode/plugins/), unless OPENCODE_DISABLE_PROJECT_CONFIG is
// set. The guard denies the agent any write under .opencode/, to
// opencode.json(c) and under ~/.config/opencode/, so it can
// neither rewrite this file nor add a plugin that changes a call
// after it; the operator edits those by hand. A plugin hook that
// runs after this one can still change the arguments, and the
// operator can change the config outside the agent. The guard is
// a backstop against mistakes, not a sandbox.

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

// Tools known to only read. Anything else that carries a path is
// checked as a write; the guard passes ordinary paths anyway.
const READERS = new Set(["read", "grep", "glob", "list", "lsp"])
// Every argument that can name a file. A call that names two must
// not slip one past the guard behind the other: each is checked.
const PATH_KEYS = ["filePath", "path", "file_path", "file", "target", "destination"]

// One tool call, either API: the tool id, its arguments, and the
// directory relative paths start from. A known shell, patch or
// file tool without the argument it needs is denied, not passed.
function check(tool, args, base) {
  args = args && typeof args === "object" ? args : {}
  const abs = (p) => path.resolve(base, String(p ?? ""))
  const files = PATH_KEYS.filter((k) => typeof args[k] === "string").map((k) => args[k])
  const shell = tool === "bash" || tool === "shell"
  const patcher = tool === "apply_patch" || tool === "patch"
  const writer = tool === "edit" || tool === "write"
  if (shell && typeof args.command !== "string") {
    throw new Error("jolitz guard: a shell call without a command string; the call is blocked")
  }
  if (patcher && (typeof args.patchText !== "string" || args.patchText.trim() === "")) {
    throw new Error("jolitz guard: a patch call without patch text; the call is blocked")
  }
  if (writer && files.length === 0) {
    throw new Error("jolitz guard: a file edit without a file path; the call is blocked")
  }
  if (shell || typeof args.command === "string") {
    guard(
      {
        tool_name: "Bash",
        cwd: args.workdir ? abs(args.workdir) : base,
        tool_input: { command: String(args.command ?? "") },
      },
      "bash",
    )
  }
  if (patcher || typeof args.patchText === "string") {
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
  }
  if (writer || (files.length > 0 && !READERS.has(tool))) {
    for (const f of files) {
      guard(
        {
          tool_name: tool === "edit" ? "Edit" : "Write",
          cwd: base,
          tool_input: {
            file_path: abs(f),
            // An unknown writer may carry its text in any field.
            content: writer ? args.content : JSON.stringify(args),
            new_string: args.newString,
          },
        },
        "file",
      )
    }
  }
}

// OpenCode 1.x.
export const JolitzGuard = async ({ directory }) => {
  const base = directory || process.cwd()
  return {
    "tool.execute.before": async (input, output) => {
      check(input && input.tool, output && output.args, base)
    },
  }
}

export default {
  id: "jolitz-guard",
  server: JolitzGuard,
  // OpenCode 2.x.
  setup: async (ctx) => {
    const base = (ctx && ctx.location && ctx.location.directory) || process.cwd()
    await ctx.tool.hook("execute.before", async (event) => {
      check(event && event.tool, event && event.input, base)
    })
  },
}

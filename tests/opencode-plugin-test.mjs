// opencode-plugin-test.mjs — drives .opencode/plugins/jolitz-guard.js
// the way OpenCode does: load the module, call the plugin, feed
// "tool.execute.before" one tool call at a time. A deny is a thrown
// error, a pass is a normal return. Needs node, bash and jq.
//   node tests/opencode-plugin-test.mjs

import path from "node:path"
import { fileURLToPath, pathToFileURL } from "node:url"

const ROOT = path.resolve(path.dirname(fileURLToPath(import.meta.url)), "..")
const PLUGIN = path.join(ROOT, ".opencode", "plugins", "jolitz-guard.js")
const DIR = "/home/alice/jolitz"

const mod = await import(pathToFileURL(PLUGIN).href)
let pass = 0
let fail = 0

// OpenCode calls every export as a plugin; anything else throws.
const exports = Object.values(mod)
if (exports.length === 1 && typeof exports[0] === "function") pass++
else {
  fail++
  console.log("FAIL: the plugin module must export exactly one function")
}

const hooks = await mod.JolitzGuard({ directory: DIR, worktree: DIR })
const before = hooks["tool.execute.before"]

async function check(expect, tool, args, label, match) {
  let got = "pass"
  let msg = ""
  try {
    await before({ tool, sessionID: "s", callID: "c" }, { args })
  } catch (e) {
    got = "deny"
    msg = String(e && e.message)
  }
  const ok = got === expect && (!match || msg.includes(match))
  if (ok) pass++
  else {
    fail++
    console.log(`FAIL [${expect}, got ${got}]: ${label}${msg ? ` (${msg.slice(0, 120)})` : ""}`)
  }
}

// bash
await check("deny", "bash", { command: "halt" }, "halt", "guard:")
await check("pass", "bash", { command: "ls -la /tmp" }, "ls")
await check("deny", "bash", { command: 'ssh root@h "mkfs.ext4 /dev/sda1"' }, "mkfs over ssh")
await check("deny", "bash", { command: "echo x > .opencode/plugins/jolitz-guard.js" }, "rewrite the plugin")
await check("deny", "bash", { command: "rm -f .claude/hooks/guard-taboos.sh" }, "remove the guard")
await check("deny", "bash", { command: "rm -rf plugins", workdir: ".opencode" }, "workdir inside .opencode")
await check("pass", "bash", { command: "ls plugins", workdir: ".opencode" }, "read inside .opencode")
await check("pass", "bash", { command: 'ssh root@h "shutdown -r now"' }, "reboot over ssh")
await check("deny", "bash", { command: "export JOLITZ_GUARD_DISABLE=1" }, "set the override")

// edit and write
await check("deny", "write", { filePath: "/etc/ssh/sshd_config", content: "x" }, "write sshd_config")
await check("deny", "edit", { filePath: "/etc/ssh/sshd_config", oldString: "a", newString: "b" }, "edit sshd_config")
await check("deny", "write", { filePath: ".opencode/plugins/jolitz-guard.js", content: "" }, "relative write into .opencode")
await check("deny", "write", { filePath: "/tmp/x.sh", content: "JOLITZ_GUARD_DISABLE=1\n" }, "override in written text")
await check("pass", "edit", { filePath: "/tmp/notes.md", oldString: "a", newString: "b" }, "edit an ordinary file")
await check("pass", "write", { filePath: "memory/servers/web1/memory.md", content: "- OS: Debian 13\n" }, "write memory")

// apply_patch: every path in the patch is checked
const patch = (lines) => ["*** Begin Patch", ...lines, "*** End Patch"].join("\n")
await check("pass", "apply_patch", { patchText: patch(["*** Add File: /tmp/a.txt", "+hi"]) }, "patch adds a file")
await check("deny", "apply_patch", { patchText: patch(["*** Add File: /tmp/a.txt", "+hi", "*** Update File: /etc/ssh/sshd_config", "@@", "-a", "+b"]) }, "patch touches sshd_config second")
await check("deny", "apply_patch", { patchText: patch(["*** Update File: /tmp/a.txt", "*** Move to: .claude/hooks/x.sh", "@@", "-a", "+b"]) }, "patch moves into .claude/hooks")
await check("deny", "apply_patch", { patchText: patch(["*** Delete File: /root/.ssh/authorized_keys"]) }, "patch deletes authorized_keys")
await check("deny", "apply_patch", { patchText: "garbage without headers" }, "patch without a path")

// the override assignment is found in any written text
await check("deny", "apply_patch", { patchText: patch(["*** Add File: /tmp/run.sh", "+export JOLITZ_GUARD_DISABLE=1"]) }, "override written by a patch")
await check("deny", "multiedit", { filePath: "/tmp/run.sh", edits: [{ newString: "JOLITZ_GUARD_DISABLE=1" }] }, "override written by an unknown writer")
await check("deny", "write", { filePath: "opencode.json", content: "{}" }, "write opencode.json")
await check("deny", "write", { filePath: "/home/alice/.config/opencode/plugins/a.js", content: "" }, "add a global plugin")

// tools the hook is not registered for under Claude Code either
await check("pass", "read", { filePath: "/etc/ssh/sshd_config" }, "read sshd_config")
await check("pass", "grep", { pattern: "PermitRootLogin", path: "/etc/ssh" }, "grep /etc/ssh")

// a tool OpenCode might rename later is caught by its arguments
await check("deny", "shell", { command: "halt" }, "renamed shell tool")
await check("deny", "multiedit", { filePath: "/etc/ssh/sshd_config", edits: [] }, "unknown writer with filePath")
await check("deny", "patch", { patchText: patch(["*** Delete File: /etc/sudoers"]) }, "renamed patch tool")
await check("pass", "glob", { pattern: "**/*.md", path: "/etc" }, "glob passes")
await check("pass", "lsp", { filePath: "/etc/ssh/sshd_config", line: 1 }, "lsp is a reader")

// the operator override, inherited from the environment
process.env.JOLITZ_GUARD_DISABLE = "1"
await check("pass", "bash", { command: "mkfs.ext4 /dev/sdb1" }, "override lifts mkfs")
await check("deny", "bash", { command: "hashcat -m 0 hashes.txt words.txt" }, "override keeps attack tools blocked")
delete process.env.JOLITZ_GUARD_DISABLE

// fails closed: no bash on PATH
const savedPath = process.env.PATH
process.env.PATH = "/nonexistent"
await check("deny", "bash", { command: "ls" }, "guard cannot start", "could not run")
process.env.PATH = savedPath

console.log(`opencode-plugin tests: ${pass} passed, ${fail} failed`)
process.exit(fail === 0 ? 0 : 1)

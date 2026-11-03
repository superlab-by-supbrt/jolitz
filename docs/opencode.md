# OpenCode and local models

Back to the [README](../README.md).

Claude Code is what jolitz is developed with. OpenCode reads
the same `CLAUDE.md` and `.claude/skills/` while
`OPENCODE_DISABLE_CLAUDE_CODE` is unset. Other tools get the
rule layer; the skills (housekeeping, security, email, fleet
audit, incident, updates, docker, macOS cleanup) need Skills
support. `memory/opencode.json.example` carries a
`permission` block that denies in OpenCode what the guard
denies in Claude Code, as far as its glob patterns reach.

With a local model (Ollama or another provider on your own
hardware) every host is read-only by default: smaller models
keep long rule sets worse. The line
`Local model changes: allowed` in `memory/user.md` lifts it
(`rules/local-model.md`).

## The guard in OpenCode

`.opencode/plugins/jolitz-guard.js` runs
`.claude/hooks/guard-taboos.sh`, the same script Claude Code
runs, before every tool call that can change something. It
hands over the call in the JSON Claude Code would send and
stops it on a deny. It holds no rules of its own.

What it checks, by OpenCode tool (1.x / 2.x):

| Tool | Checked as |
|---|---|
| `bash` / `shell` | the command, in its `workdir` |
| `edit`, `write` (`filePath` / `path`) | the target file |
| `apply_patch` / `patch` | every file the patch adds, updates, deletes or moves to |

Reads, searches, web and MCP tools pass, as under Claude
Code. A tool a later OpenCode renames is still caught by its
arguments (`command`, `filePath` or `path`, `patchText`).

One file serves both plugin APIs. OpenCode 1.x reads
`default.server()` and its `tool.execute.before` hook;
OpenCode 2.x reads `default.setup()` and registers
`tool.hook("execute.before")`. Each ignores the other's key.
Needs OpenCode 1.3.4 or newer: older versions call every
export as a function and do not load the default object, so
no guard runs there (`bin/jolitz-doctor` warns). Written
against OpenCode 1.19.5 and the 2.2.5 source; the tests drive
both APIs with stand-ins, not a running OpenCode.

Each argument that can name a file (`filePath`, `path`,
`file_path`, `file`, `target`, `destination`) is checked on
its own, and only `read`, `grep`, `glob`, `list` and `lsp`
count as readers. A known shell, patch or file tool missing
its argument is denied. Before 3.3.3-jolitz.22 the plugin had no default
export, so OpenCode 2 did not load it and nothing was
guarded there.

### What you set up

Nothing to install. OpenCode loads every `*.js` in
`.opencode/plugins/` of each `.opencode` directory between
the directory it starts in and the repository root.

- Start `opencode` in the jolitz checkout or below it. Started
  elsewhere, OpenCode does not see the plugin.
- Leave `OPENCODE_DISABLE_PROJECT_CONFIG` unset. With it,
  OpenCode loads no project plugin, and no guard runs.
- `bash` and `jq` on `PATH`, as for Claude Code
  (`bin/jolitz-doctor` checks `jq`).
- The operator override works as under Claude Code:
  `JOLITZ_GUARD_DISABLE=1 opencode`, for that session only.
  `JOLITZ_GUARD_LOG=1` writes the denial log.

### Check that it runs

Ask OpenCode to write a file into `.opencode/`:

```
❯ Write the word test into .opencode/probe.txt
```

The answer must be a refusal that starts with `jolitz guard:`.
If the file appears, the plugin is not loaded: check the
start directory and `OPENCODE_DISABLE_PROJECT_CONFIG`, then
delete the file. Without OpenCode:
`node tests/opencode-plugin-test.mjs`.

### Fails closed

When the script cannot start, takes longer than 15 seconds,
exits with an error or answers with something that is not the
guard's JSON, the call is blocked, like
`"onFailure": "block"` under Claude Code. Without `bash` on
`PATH` every shell command and file write is blocked.

### What differs from Claude Code

- The guard denies any write under `.opencode/`, to
  `opencode.json` or `opencode.jsonc` (also
  `memory/opencode.json`) and under `~/.config/opencode/`, as
  under `.claude/hooks/`: the agent can neither rewrite the
  plugin nor add one that changes a call after it, nor loosen
  its own permission block. You edit those files by hand, in
  an editor or a shell outside the agent; asking jolitz to do
  it is denied on purpose. `JOLITZ_GUARD_DISABLE=1` lifts this
  with every other taboo for the session, so prefer the
  editor.
- A plugin hook that runs after this one can still change a
  call's arguments, and anyone with the config can remove
  the plugin. The guard protects against the agent, not
  against the operator.
- MCP tools pass unchecked, as under Claude Code.
- The SessionStart hooks (update check, start screen, `jq`
  check, SSH socket directory) and the output marker
  (`.claude/hooks/flag-output.sh`) do not run. Create the
  socket directory once: `mkdir -p -m 700 ~/.cache/jolitz`.
  Text in server output that addresses the model meets only
  the rule ([safety](safety.md#injected-text-in-server-output)).
- The `permission` block from `memory/opencode.json.example`
  stays useful as a second layer.

The guard is a backstop against mistakes, not a sandbox.

## A local model

With [Ollama](https://ollama.com) and OpenCode jolitz runs on
your own hardware:

```bash
ollama pull qwen3.5:9b
ollama run qwen3.5:9b
>>> /set parameter num_ctx 16384
>>> /save qwen3.5:9b-16k
>>> /bye
cp memory/opencode.json.example memory/opencode.json
opencode
```

Ollama's default context of 4096 tokens is too small for tool
use. Set `baseURL` and the model in `memory/opencode.json` and
pick it with `/models`. 14B and up call tools more reliably.
See the [OpenCode provider docs](https://opencode.ai/docs/providers/).

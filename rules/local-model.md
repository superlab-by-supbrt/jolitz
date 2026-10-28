# Local Model

jolitz runs under OpenCode with a local model when
`memory/opencode.json` names a provider on the
operator's own hardware: an `ollama` section, or a
`baseURL` on localhost or a private address
(README → A local model). Then every host is
read-only by default.

## Why

Smaller models keep long rule sets worse: they skip
a check, misread a taboo, invent a flag. Under
OpenCode the guard hook does not run; the written
rules are the only safety layer there. Reading
breaks nothing, so reading is the default until the
operator says otherwise.

## What applies

Read-only mode exactly as `rules/access-control.md`
→ Read-Only Servers: the same allowed list, the same
blocked list, the same deferred modification report
at session end. It applies to every host, local mode
included, whether or not the host is listed in
`memory/readonly.md`.

Say so once per session, before the first command:
"Local model: every host is read-only
(`rules/local-model.md`). The line
`Local model changes: allowed` in `memory/user.md`
lifts it."

## Lifting it

One line in `memory/user.md`:

```markdown
Local model changes: allowed
```

Then the normal rules apply, with one difference:
the reload auto-proceed of `rules/service-reload.md`
stays off. A local model asks before every change,
reloads included, with the `Way back:` line.
`memory/readonly.md`, `/etc/jolitz.conf` and the
blacklist are not touched by the line.

Only the operator writes that line, by hand. jolitz
never adds it and never proposes it for the task at
hand.

## How to check

At session start, with the preflight
(`CLAUDE.md` → Session Start Preflight): does
`memory/opencode.json` exist, and does it hold a
local provider? Read the file; do not guess from the
model name the session shows. No file, or only
hosted providers: this rule does not apply.

## OpenCode permissions

`memory/opencode.json.example` carries a
`permission` block that denies in OpenCode what
`.claude/settings.json` denies in Claude Code, and
the guard's taboos as far as glob patterns reach:
halt, poweroff, shutdown without `-r`, mkfs and its
relatives, the account and sudo editors, boot
loaders, stopping sshd, a second agent session, the
server lock. Verified against
https://opencode.ai/docs/permissions on 2026-10-28:
`permission.bash` maps glob patterns to `allow`,
`ask` or `deny`; `*` matches any characters; the
last matching rule wins, so `"*": "ask"` comes
first. The published schema
(https://opencode.ai/config.json) marks `permission`
deprecated without naming a successor; the
documentation still describes it. Keep the block
when you copy the example. It is a backstop like the
guard, not a sandbox: a command inside `ssh host "…"`
or built at run time is outside what a pattern sees.

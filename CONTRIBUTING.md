# Contributing

Bug reports, ideas and pull requests are welcome.

- jolitz is MIT, like heinzel, which it forks. A
  contribution is published under the same license.
- A fix that is not specific to this fork (a rule,
  a probe, a guard pattern that upstream has too)
  belongs in [heinzel](https://github.com/wintermeyer/heinzel)
  as well. Send it there first when you can.
- Everything is named jolitz: skills, scripts,
  variables, paths, the journal tag. "heinzel"
  appears only where a text points to upstream.
- Rule files wrap at 80 characters, except code
  blocks. A code block that needs the guard lifted
  is fenced ` ```sh guard-off ` and its file names
  `JOLITZ_GUARD_DISABLE`.
- A change to `.claude/hooks/guard-taboos.sh` needs
  lines in `guard-taboos-test.sh`: one per effect
  blocked, one for the read-only form that stays
  allowed, one through a language runtime, one inside
  `ssh host "…"`, and one with the override set when
  the rule is meant to hold even then. CI runs the
  matrix.
- A rule names the effect it guards against, never a
  list of binaries. See the head of the hook for why.
- `rules/ethics.md` is not up for change in this
  fork: no pull request that weakens it is merged.

## Working on the code with an AI assistant

`CLAUDE.md` in this repository is the product: the
system prompt of the admin agent. It is not a guide
for developing jolitz, and its guard hook gets in
the way of editing the hook itself. Work on the code
from a workspace one level up, with a CLAUDE.md of
its own and this repository as a subdirectory, so
the product's hooks do not run and its CLAUDE.md is
read as content.

## Layout

```
CLAUDE.md        instructions for the AI tool (the product)
rules/           rule files per platform and topic
.claude/skills/  housekeeping, security, email, fleet audit,
                 incident, updates, docker, macOS cleanup, hub
.claude/hooks/   update check, hub sync and the guard
bin/             jolitz-update, -backup, -migrate, -overview,
                 -memory-lint, -doctor, -log, -guard-report,
                 -sync
lib/             the renderer behind jolitz-overview
hub/             the team hub (Go), optional
tests/           lint, overview, bin tools, sync client
docs/            the long forms the README links to
memory/          the operator's state (gitignored)
assets/          screenshots, demo data, the trailer script,
                 the welcome and the start banner
```

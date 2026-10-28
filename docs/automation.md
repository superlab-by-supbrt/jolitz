# Fewer prompts, scheduled runs, updates

Back to the [README](../README.md).

## Fewer prompts

Claude Code asks before every tool call. For batch work,
**auto mode** lets routine commands through and still stops
on risky ones (Shift+Tab, or `--permission-mode auto`). It
trusts this checkout, not your servers: changes over SSH need
the hosts under `autoMode` in `~/.claude/settings.json`.
`/auto-mode-setup` drafts the block; `rules/service-reload.md`
explains it.

```bash
claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com"
```

For CI the strictest setup is `--permission-mode dontAsk`
with an allowlist. `--dangerously-skip-permissions` drops all
review, including the defence against malicious server
output: disposable environments only. In OpenCode, `-p` is
`opencode run "…"`. See the
[permission modes docs](https://code.claude.com/docs/en/permission-modes).

A nightly check from cron:

```
17 6 * * * cd /path/to/jolitz && flock -n \
  ~/.cache/jolitz/cron-server1.lock timeout 30m \
  env JOLITZ_NO_UPDATE=1 \
  /abs/path/to/claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report" >> ~/jolitz-cron.log 2>&1
```

Run the prompt once interactively first, so its one-time
questions are answered. `JOLITZ_NO_UPDATE=1` keeps an
unattended run from pulling and running new code from
`main` with nobody reading the changelog; update by hand
or pin a release. A systemd timer variant is in
`rules/scheduled-housekeeping.md`.

## Updates

Claude Code pulls `main` at session start, unless the
checkout is pinned, on another branch, or
`JOLITZ_NO_UPDATE=1` is set. The version is in `VERSION`, the
changes in `CHANGELOG.md`. Every pull that brings commits
prints them, with or without a version bump, because what
arrives is the agent's instructions, its hooks and the
scripts the hook runs next. For a checkout that works on
production servers, pin a release and move it yourself:

```bash
bin/jolitz-update                       # pull latest
bin/jolitz-update --check               # check only
bin/jolitz-update --pin v3.3.3-jolitz.1
bin/jolitz-update --unpin
```

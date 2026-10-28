# Scheduled Housekeeping

How to run jolitz housekeeping on a schedule —
nightly or weekly health reports delivered by email
without a human at the keyboard. Read this when the
user asks to "schedule housekeeping", "run a nightly
check", or "email me a weekly report automatically".

## Where the Scheduler Lives

On the workstation or ops box that has the jolitz
repo, the `claude` CLI, and the SSH keys — **never
on the managed server itself**. The managed server
needs no jolitz installation.

## The Command

```sh operator
cd /path/to/jolitz && claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report"
```

- Verify the flags with `claude --help` on the
  installed CLI before writing the crontab — flag
  names have changed across releases.
- The operator installs the crontab line. jolitz
  never starts a second `claude -p` itself: the
  guard denies it, because a session started from
  another directory runs without this hook.
- The `-p` prompt counts as the explicit user
  request that the `jolitz-email` skill requires;
  "never run automatically" means never
  self-initiated, not never scheduled by the user.

## Prerequisite: One Interactive Dry Run

Run the exact prompt **interactively once** before
scheduling it. The first run answers questions an
unattended run cannot:

- the email skill's pickers (recipient, local vs
  remote source, MTA choice) — persisted as
  `Alert email:` / `Email source:` lines in
  `memory/servers/<host>/memory.md`;
- the backup-presence question from housekeeping;
- any first-connection onboarding for a new host.

Once memory holds the answers, unattended runs have
nothing left to ask.

## Safety

- **`--permission-mode auto` only.** Auto mode keeps
  a safety check on every action.
- **Never `--dangerously-skip-permissions` in
  cron.** Unattended plus unguarded plus production
  SSH keys is the worst possible combination.
- Stricter alternative: `--permission-mode dontAsk`
  with an explicit allowlist in
  `.claude/settings.json` — only pre-approved
  commands run.
- Wrap the run in `timeout` (see template). In auto
  mode a run that hits a question it cannot answer
  idles; the timeout ends it instead of letting it
  hang until the next run.

## Cron Environment

- cron's default `PATH` is minimal — `claude` is
  often a mise or npm shim that won't resolve. Use
  the absolute path from `command -v claude`, or set
  `PATH=` at the top of the crontab.
- Schedule in the crontab of the user who owns
  `~/.claude` (CLI credentials) and the SSH keys —
  not root's.
- The SSH key must work without an interactive
  agent or passphrase prompt (jolitz's standard
  BatchMode requirement).

## Crontab Template

```sh operator
17 6 * * * cd /path/to/jolitz && flock -n \
  ~/.cache/jolitz/cron-server1.lock timeout 30m \
  env JOLITZ_NO_UPDATE=1 \
  /abs/path/to/claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report" >> ~/jolitz-cron.log 2>&1
```

- `flock -n` skips the run if the previous one is
  still going — never two jolitz sessions on the
  same host at once. (`flock` is Linux; on macOS,
  prefer the launchd alternative below, which
  serializes runs by itself.) The lock lives in
  `~/.cache/jolitz` (mode 0700), not in `/tmp`,
  where any local user could plant the file.
- `JOLITZ_NO_UPDATE=1`: an unattended run must not
  pull new rules, hooks and scripts from `main` and
  run them with nobody reading the changelog.
  Update interactively, or pin a release
  (`bin/jolitz-update --pin`).
- `timeout 30m` caps a stuck run.
- Pick an off-peak minute that is not :00.
- `~/jolitz-cron.log` grows without bound and holds
  host names and report text. Rotate it (logrotate,
  newsyslog) or truncate it from the same crontab.

A weekly summary of all servers needs no SSH at
all: it is built from `memory/` by
`bin/jolitz-overview --text` and mailed from the
workstation (`jolitz-email` → Fleet summary):

```sh operator
23 7 * * 1 cd /path/to/jolitz && flock -n \
  ~/.cache/jolitz/cron-fleet.lock timeout 10m \
  env JOLITZ_NO_UPDATE=1 \
  /abs/path/to/claude --permission-mode auto \
  -p "Email me the weekly fleet summary" \
  >> ~/jolitz-cron.log 2>&1
```

With a team hub (`rules/hub.md`) the weekly summary
needs no cron line and no workstation at all: the
hub's scheduler builds the same text from its
database every week (`[summary]` in the hub's
`hub.toml`, weekday and hour) and pushes it through
the channels of `rules/notify.md` configured there.
The crontab above stays the way for teams without a
hub and for email, which the hub does not send.

## systemd Timer Alternative (Linux)

A `jolitz-housekeeping.service` (Type=oneshot,
`ExecStart` = the command above) plus a timer:

```
[Timer]
OnCalendar=daily
RandomizedDelaySec=30m
Persistent=true
```

Timers never start the service while a previous run
is active, so no flock is needed. On macOS, launchd
is the native equivalent; cron also works.

## Where Output Goes

- **Delivery:** the report arrives by email via the
  `jolitz-email` skill (that is what the prompt
  asks for).
- **Debug trail:** `claude -p` stdout lands in the
  log file from the template (or cron's `MAILTO`).
- **Audit trail:** as in any session —
  `journalctl -t jolitz` on the server and
  `memory/servers/<host>/changelog.log` locally.

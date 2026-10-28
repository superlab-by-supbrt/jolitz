# Scheduled Housekeeping

How to run heinzel housekeeping on a schedule —
nightly or weekly health reports delivered by email
without a human at the keyboard. Read this when the
user asks to "schedule housekeeping", "run a nightly
check", or "email me a weekly report automatically".

## Where the Scheduler Lives

On the workstation or ops box that has the heinzel
repo, the `claude` CLI, and the SSH keys — **never
on the managed server itself**. The managed server
needs no heinzel installation.

## The Command

```
cd /path/to/heinzel && claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report"
```

- Verify the flags with `claude --help` on the
  installed CLI before writing the crontab — flag
  names have changed across releases.
- The `-p` prompt counts as the explicit user
  request that the `heinzel-email` skill requires;
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
  agent or passphrase prompt (heinzel's standard
  BatchMode requirement).

## Crontab Template

```
17 6 * * * cd /path/to/heinzel && flock -n \
  /tmp/heinzel-cron-server1.lock timeout 30m \
  /abs/path/to/claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report" >> ~/heinzel-cron.log 2>&1
```

- `flock -n` skips the run if the previous one is
  still going — never two heinzel sessions on the
  same host at once. (`flock` is Linux; on macOS,
  prefer the launchd alternative below, which
  serializes runs by itself.)
- `timeout 30m` caps a stuck run.
- Pick an off-peak minute that is not :00.

## systemd Timer Alternative (Linux)

A `heinzel-housekeeping.service` (Type=oneshot,
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
  `heinzel-email` skill (that is what the prompt
  asks for).
- **Debug trail:** `claude -p` stdout lands in the
  log file from the template (or cron's `MAILTO`).
- **Audit trail:** as in any session —
  `journalctl -t heinzel` on the server and
  `memory/servers/<host>/changelog.log` locally.

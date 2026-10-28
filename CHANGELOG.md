# Changelog

## 3.3.3-jolitz.7 — 2026-10-28

- **Three new skills.** `jolitz-incident` (a read-only
  timeline, what changed on the host, one finding with
  evidence and a proposed fix), `jolitz-updates` (the dry
  run of every host's package manager side by side, a
  window proposed, applied only after the OK and never with
  a reboot chained on), `jolitz-docker` (containers and
  compose projects, published ports against the firewall,
  image age, restart policies, disk use).
- **Fleet audit:** certificate expiry as a category,
  FreeBSD and macOS probes, and every drift entry appended
  to the host's `todo.md` so it reaches overview and
  weekly mail.
- **Housekeeping:** FreeBSD and NetBSD baselines; the
  backup check can restore one sample and compare its
  checksum before it says OK.
- **Around the tool.** `bin/jolitz-doctor` checks the
  workstation (Claude Code version, guard registration,
  jq, curl, socket directory, git state, memory, logs, ssh
  agent) with one line per check and exit 2 on a FAIL;
  `bin/jolitz-log` searches every changelog by host, tag,
  date and pattern; `bin/jolitz-guard-report` turns the
  guard log into an issue template.
- **More views from memory.** `- Owner:` and
  `- Services:` lines in `memory.md` give the Servers
  table an owner column and the overview a services
  matrix; `- Link:` lines in `memory/network.md` are drawn
  as a graph; `timeline.html` puts every change on one
  axis; `--json` and `--prom` write `overview.json` and a
  Prometheus textfile.
- **Rules.** `rules/guest-software.md` and `rules/sap.md`:
  jolitz as a guest under an application it does not
  administer, with the application's own taboos.
  `rules/local-model.md`: read-only by default under a
  local model. `rules/notify.md`: push to ntfy, Matrix or
  a Slack-compatible webhook, secrets only in files.
  `rules/backups.md`: the diff and the config test before
  a config file goes live. `memory/opencode.json.example`
  carries a `permission` block for OpenCode.
- **CI** runs the guard matrix and every test under
  Alpine busybox as well; the matrix no longer uses a
  GNU-only `grep` option.

## 3.3.3-jolitz.6 — 2026-10-28

- **The guard sees more spellings of a write.** All
  three POSIX output redirections, every spelling of an
  in-place edit (`-i` alone, at the end of a flag
  cluster, with a suffix, `--in-place`), an output flag
  at the end of a `curl`/`wget` cluster, `ex` next to
  the other editors, and a write by relative name after
  a `cd` into `/etc/ssh`, `sshd_config.d`, `.ssh`,
  `/etc/sudoers.d` or `/conf/sshd`, or a `find -delete`
  there. Each form has its test, for the server lock
  also with the override set.
- **New taboos, named by effect.** The machine goes to
  sleep (systemd suspend/hibernate, `acpiconf -s`,
  `pmset sleepnow`, `rtcwake`, `/sys/power/state`); the
  boot default changes (`grub-set-default`,
  `grub-reboot`, `grub-editenv set`, `nvram`,
  `systemsetup -setstartupdisk`); a device is formatted
  by a tool outside the mkfs family (`cryptsetup
  luksFormat`, `zpool labelclear`, `mdadm
  --zero-superblock`); partition attributes change
  (`sgdisk -A`, `gpart unset`); firmware is written to a
  drive (`hdparm --fwdownload`, `nvme fw-*`); sshd is
  stopped, disabled or masked; a second agent session
  is started (`claude -p`, `--settings`,
  `--permission-mode`, `--dangerously-skip-permissions`,
  `opencode run`), because it would run without this
  hook. Read-only forms pass. The crontab templates in
  `rules/scheduled-housekeeping.md` are marked
  `operator`: the operator installs them, jolitz never
  runs them. Every disk tool now has a test inside
  `ssh host "…"`.
- **A hook that fails or times out now denies.** Both
  guard registrations in `.claude/settings.json` carry
  `"onFailure": "block"` and a 15-second timeout. Claude
  Code lets a timed-out PreToolUse hook through by
  default; this needs Claude Code 2.1.295 or newer. The
  guard itself runs in well under a second. The matrix
  checks the registration.
- **Every pull names its commits.** The session-start
  hook and `bin/jolitz-update` print the new commits
  whenever HEAD moved, with or without a `VERSION`
  change; before, a pull that left `VERSION` alone was
  silent although it brought new rules, hooks and the
  migration script that ran next. The cron templates set
  `JOLITZ_NO_UPDATE=1` and lock under `~/.cache/jolitz`
  instead of `/tmp`; the README says when to pin a
  release.
- **Memory lint warns about a bare word after
  `Password:`.** A location (`in /root/.my.cnf`) and the
  usual states (`none`, `rotated …`) pass; a word that
  looks like the value itself is a WARN. `refusals.log`
  and the other `.log` files in `memory/` are scanned
  like the rest.
- **The overview skips a host directory with an odd
  name** (characters outside `A-Za-z0-9._-`) with a
  warning instead of writing it into awk variables and
  file names.
- **Mail recipients and policies from shared memory ask
  first.** An `Alert email:` outside the operator's own
  domain is confirmed once per session, an `always`
  policy nobody at this keyboard chose is treated as
  missing; in team mode those lines arrive through git.
- **Docs.** README: `jq` as a requirement, what leaves
  the machine and what stays for how long, the guard's
  limits, a warranty sentence up front, "limits a
  lockout" instead of "cannot lock you out for long",
  the fork's origin in neutral terms and "no plan for a
  paid edition" instead of a promise. `CLAUDE.md`: a user
  request does not lift a taboo, sleep, boot default,
  firmware and other formatters named as taboos, the
  guard denies stopping sshd. `rules/deployment.md` and
  `rules/os-replacement.md` name the override where
  their steps need it; `rules/rollback-timer.md` no
  longer points to a section that left with the fork.
  OpenSSH 10.4 began mixed-case `sshd -T` output, 10.5
  completed it. CI runs with read-only token rights and
  pins `actions/checkout` to a commit.

## 3.3.3-jolitz.5 — 2026-10-28

- **The README opens with everyday work.** A table of
  realistic requests, what jolitz does and whether it
  changes anything, and the fleet overview screenshot. OS
  replacement, dual boot and deploy users moved to "Heavy
  work, with the guard lifted", which says when they are
  realistic: with a console, a rescue system, a VM snapshot
  or a volume manager, and with downtime planned.
- **"Which servers run out of support?"** runs
  `bin/jolitz-overview --eol --text` and answers from it.

## 3.3.3-jolitz.4 — 2026-10-28

- **A fleet overview from memory.** `bin/jolitz-overview`
  writes `memory/overview/index.html`: servers with OS, end
  of life, last connection, tags, open to-dos and last
  change; a "needs attention" list; recent changes with
  their way back; the latest fleet audit. Each server gets
  a handover sheet with its standing constraints, own rules
  and full changelog. `--text` prints a summary, `--tag`
  narrows it. No server is contacted. Demo data and
  screenshots are in `assets/overview/`.
- **End-of-life dates from a live source.** `--eol` fetches
  them from endoflife.date for the products in memory,
  never host names, and caches them in `memory/eol/`. The
  page names source and fetch date.
- **Memory lint.** `bin/jolitz-memory-lint` flags values
  that look like credentials (by line, never by value) and
  shape problems: missing OS or last-connected line, bad
  dates, tags, oversized memory, a todo.md with nothing
  open. A secret stops the overview from being built.
  jolitz runs it after each memory update.
- **Tags.** `- Tags: prod, web` in `memory.md`. The fleet
  audit takes `tag:<name>`, the overview `--tag`.
- **The fleet audit keeps its history.** Each run saves a
  snapshot in `memory/audits/` and reports what changed
  since the last one, and which of those changes no
  changelog entry explains.
- **Fleet summary by mail.** "Email me the weekly fleet
  summary" sends the text summary from the workstation;
  `rules/scheduled-housekeeping.md` has the cron line.
- **Opt-in guard log.** With `JOLITZ_GUARD_LOG=1` the
  guard appends each denial to `~/.cache/jolitz/guard.log`
  (0600): time, reason and the first 200 characters of the
  command, or only the path of a file edit. Writing it
  never changes a decision; an unwritable or symlinked log
  still denies. The overview counts denials by reason.

## 3.3.3-jolitz.3 — 2026-10-28

- **Everything is named jolitz.** Skills
  (`jolitz-housekeeping`, …), scripts
  (`bin/jolitz-update`, `bin/jolitz-backup`,
  `bin/jolitz-migrate`), the variables
  `JOLITZ_GUARD_DISABLE` and `JOLITZ_NO_UPDATE`,
  the server lock `/etc/jolitz.conf`, the backup
  directory `/var/backups/jolitz/`, the socket
  directory `~/.cache/jolitz` and the journal tag
  `jolitz`. The guard reports as "jolitz guard".
- **Action needed.** On a server locked with the
  old lock file, its owner moves it:
  `mv /etc/heinzel.conf /etc/jolitz.conf`. Until
  then jolitz treats the server as unlocked.
  Export `JOLITZ_GUARD_DISABLE` instead of the old
  variable; the old one lifts nothing. Backups and
  journal lines written under the old names stay
  where they are; jolitz no longer reads them.
- `bin/jolitz-migrate` renames skill overrides in
  `memory/custom-rules/` to the new skill names on
  the next session start. An existing file under
  the new name wins; the old one stays to merge by
  hand.
- **A README of its own**, for admins: what
  jolitz does, with everyday examples, how it stays
  out of trouble, and the promise that every
  feature is free and stays free.

## 3.3.3-jolitz.2 — 2026-10-28

- **The guard's own files hold against more
  spellings.** Case (`.CLAUDE/Settings.json`), `..`
  and `/./`, a non-ASCII byte next to `.claude`, a
  session cwd anywhere in a `.claude` tree, and the
  writers rsync, scp, dd `of=`, `curl -o`, tar,
  unzip and git checkout/restore now count. The
  write onto a spelled-out path, lost in
  3.3.3-jolitz.1, is back. `.claude/skills/` stays
  usable, for the macOS cleanup scanner.
- **Next to `.claude` only reads pass.** The guard
  no longer lists the ways to write there; it
  allows cat, grep, ls, head, find without `-exec`
  and `-delete`, and git log, show and diff without
  `--output`, `-O`, `--ext-diff` or `--textconv`.
  The scanner keeps its two forms, on one line, and
  without a redirect when the session runs inside
  `.claude`; over ssh only with the `-o` options
  CLAUDE.md prescribes. Quotes, backslashes, globs
  and braces that the shell turns into `.claude`
  put a command in scope too.

## 3.3.3-jolitz.1 — 2026-10-28

- **jolitz forks heinzel 3.3.3.** The work heinzel
  3.0 moved into a paid edition is back: OS replacement,
  EFI and dual boot, partition staging, cloud
  images, accounts, SSH keys, sudo and deploy users,
  secret rotation, the fleet audit skill and
  scheduled housekeeping. Every request for it asks
  first, as any other change does.
- **The guard keeps every 3.x block and gets its
  override back.** The operator exports the
  override variable set to 1 before the session to
  lift the taboos. Attack tools, the
  server lock and an assignment of the variable by
  the agent stay blocked even then. With the guard
  on, `.claude/hooks/` and `.claude/settings*.json`
  are protected, also through a relative path or a
  session cwd inside `.claude`.
- The ethics rule is unchanged.

## Before the fork

jolitz forked heinzel 3.3.3. Its releases up to then
are in
[heinzel's changelog](https://github.com/wintermeyer/heinzel/blob/main/CHANGELOG.md).

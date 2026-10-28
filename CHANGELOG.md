# Changelog

## 3.3.3-jolitz.6 — 2026-10-28

- **The guard reads Windows paths.** A file edit on
  `C:\Users\x\.claude\settings.json`, on a key under
  `C:\Users\x\.ssh\`, or a session whose directory is
  `C:\…\.claude` passed, because every pattern expects
  `/`. Paths from the file tools, the session directory
  and backslash paths in a command are now read with
  slashes.
- **Without jq the guard no longer blocks everything.** The
  hook input always carries `transcript_path` under
  `~/.claude`; scanned raw, it put every command next to the
  guard's own files, even `ls`. The fallback now leaves that
  one field out. Taboos stay blocked.
- **DNS aliases on Windows.** Git Bash copies a directory
  where `ln -s` should link it, so an alias got its own
  memory that drifted from the server's. On Windows an
  alias is now a file holding the canonical name
  (`rules/dns-aliases.md`); `bin/jolitz-overview` and
  `bin/jolitz-memory-lint` read both forms, also a symlink
  that git checked out as a file.
- **The README says what a workstation needs:** `jq`, and on
  Windows Git for Windows or WSL 2.
- **CI on Windows.** Every pull request runs a guard smoke
  test with Windows hook input in Git Bash (`tests/windows-smoke.sh`),
  the memory tool tests, and a check that no shell script is
  checked out with CRLF. The full guard matrix, which takes
  longer than 45 minutes under Git Bash, runs on Windows for
  `main` and on request.

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

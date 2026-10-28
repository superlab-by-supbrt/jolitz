# Changelog

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
- **The README speaks of jolitz.** Technical names
  (skills, `bin/heinzel-*`, `HEINZEL_*`, the journal
  tag, the server lock file) keep the upstream
  spelling.

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

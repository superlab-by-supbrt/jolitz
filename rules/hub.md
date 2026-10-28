# Team Hub

The hub is an optional service of the team: one binary
(`jolitz-hub`), an HTTP API, a web UI and a SQLite file on a
host of the team, behind a reverse proxy with TLS. Every
workstation that has `~/.config/jolitz/hub.conf` mirrors its
`memory/` into it with `bin/jolitz-sync` and pulls the others'
files back. Without that file nothing of this rule applies:
no sync, no session, no line in `bin/jolitz-doctor`. Set up and
joined through the `jolitz-hub` skill
(`.claude/skills/jolitz-hub/`), never by hand.

`memory/` stays the working copy and the source. Every rule and
skill reads `memory/`; the hub shows the same files to the whole
team, live, with search and presence. The team mode over git
(`CLAUDE.md` → Team Usage) is the alternative for a team without
a hub; both can run side by side.

## What the hub holds

The synced set, byte-identical to the files in `memory/`:

- `servers/<host>/memory.md`, `changelog.log`, `todo.md`,
  `rules.md`;
- `servers/<alias>`: an alias as a file holding the canonical
  name (the Windows form of `rules/dns-aliases.md`; `pull`
  recreates the symlink on Unix);
- `network.md`;
- `audits/<YYYY-MM-DD-HHMM>.json`.

The hub parses them for its pages (hosts, changes, to-dos,
links, audit values). It rewrites a file only for the edits its
UI allows (to-dos, owner, tags, links), each as a new version
by the UI user, through the same lint as a push. Every write of
a file is a version: `GET files/<path>/history` lists them with
who and when, `GET files/<path>?version=N` returns an old body,
`bin/jolitz-sync history <path>` prints the list. Besides the
files the hub knows what only a service can know:

- **Sessions**: who is on which host from which workstation,
  since when, last heartbeat. Opened by the SessionStart hook,
  closed by the Stop hook; without a heartbeat for 10 minutes
  a session shows as stale, after 60 minutes it is closed.
- **Claims**: "alice holds db1 until 20:00, migration".
  `bin/jolitz-sync claim <host> [note]` and `release <host>`.
- **Guard reasons**: time, person and the reason of a guard
  denial, when a client sends them. Never the command text; the
  hub rejects a body that carries one.

Hostnames, IPs, operator names, changelogs: what lies on every
workstation today, now in one place. The hub host is backed up
and protected accordingly (`jolitz-hub` skill →
`references/operate.md` → Backup).

## What never leaves the workstation

- `memory/user.md`, `blacklist.md`, `readonly.md`,
  `protected-paths.md`, `opencode.json`, `overview/`, `eol/`:
  not synced in either direction, not even with `--prune`.
- The hub token: one line `Authorization: Bearer …` in a file
  with mode 600 under `~/.config/jolitz/`, named by `hub.conf`.
  Never in a command jolitz runs, never in `memory/`, never in
  a changelog, never shown again after `user add` printed it
  (`rules/notify.md` → Secrets only in files, `rules/secrets.md`).
- Secrets in memory: `bin/jolitz-memory-lint` runs before every
  push and stops on `ERROR`; the hub checks the same patterns
  and refuses the file with 422 and a line number, storing
  nothing. A credential's place in memory is its location, not
  its value (`rules/server-memory.md`).
- Commands. The hub runs none, holds no SSH key and reaches no
  server. Every command keeps running from the workstation of
  the person responsible, with their key, their guard and
  their provider. The UI writes claims, to-dos, the `Owner:`
  and `Tags:` lines of `memory.md` and the `Link:` lines of
  `network.md`, nothing else; the rest of memory and every
  changelog entry are written by jolitz.

## Pull at session start

With `hub.conf` the SessionStart hook pulls before anything
else; the session works on the team's current files. When the
hub does not answer, the hook says so once and the session
continues on the local copy. `bin/jolitz-sync status` then
prints `stale` (long form: `pulled: <time>, stale (pull failed
at <time>; run jolitz-sync pull)`), and keeps printing it until
a pull reaches the hub; a workstation that never pulled is
stale too. Step 6 of `rules/first-connection.md` reads that
line: stale means `bin/jolitz-sync pull` before the host is
touched, because memory, to-dos and claims may lag the team.
Where hooks do not run (OpenCode), the pull is the user's first
command.

## Versions and conflicts

Every file on the hub carries a version number next to its
checksum; every push, and every edit in the UI, is a new
version, and the old bodies stay in the history. A push names
the version it builds on (`If-Match: <sha256>`); once a file
exists the hub takes nothing without it (428).

Changelog and audits are append-only; there are no conflicts
there. `memory.md`, `todo.md`, `rules.md` and `network.md` can
be edited on two workstations at once, or on one and in the UI.

- `pull` never overwrites a local file that is newer than the
  hub's and different. It keeps the local file, writes the
  hub's version next to it as `<file>.hub`, lists every such
  file at the end with a line count (`+3 -1 lines against
  yours`) and exits 1.
- `push` sends each file with the checksum it was based on; a
  409 means someone pushed first. The answer carries both
  checksums, the hub's version number and a unified diff of
  the hub's body against the pushed one. Same handling: the
  hub's version lands as `<file>.hub`, the local file stays,
  the line count is printed.
- The user decides per file. jolitz shows `diff -u <file>
  <file>.hub` and asks; it picks no side on its own. Then:
  `bin/jolitz-sync resolve <file> --mine` pushes the local
  file over the hub's current version, `--theirs` replaces the
  local file with the hub's. Both remove the `.hub` file and
  record the new state. A merge by hand is `--mine` after the
  edit. If the hub moved again in between, `--mine` reports a
  new conflict and writes a fresh `.hub`.
- `bin/jolitz-sync history <file>` lists the versions with who
  and when; an old body is `GET files/<path>?version=N`. The
  way back from a wrong resolution is to fetch that body and
  push it as `--mine`.

Presence exists to make this rare: two people on one host see
each other before they edit.

## Claims and handover

A claim says "I am working on this host; ask before you do".
Before working on a host, `bin/jolitz-sync status` shows the
open claims, and step 6 of `rules/first-connection.md` reads
them. Another person's claim on the host: say so, name the
person and the note, and ask before continuing. Working anyway
is the user's decision, not jolitz's. A claim is etiquette, not
a lock: the hub refuses nothing because of it.

"übernimm db1" or "take over db1" is
`bin/jolitz-sync claim db1 "<note>"`: the claim is set and the
handover printed. A claim made in a session is released by the
Stop hook when the session ends; `claim --keep <host>` keeps
it standing for work that spans sessions ("migration until
20:00"), `release <host>` ends it by hand. Another person's
claim is released only by them or an admin; the UI's "claim"
and "release" buttons do the same.

The handover is what the next person reads first:
`bin/jolitz-sync handover <host>` prints the claim, the
sessions on the host, the open to-dos, the running jobs (to-do
lines that name `~/.jolitz/jobs/`, `rules/ssh-connections.md`)
and the last three changes with their `Rollback:` line, from
`GET hosts/<name>/handover`. The host page of the UI starts
with the same block. It is part of the activity report
(`rules/activity-check.md`) and the answer to "what is going on
on db1" when a hub is configured; without one, the local
`todo.md` and `bin/jolitz-log` are.

## Retention

The hub of this version deletes nothing on its own. Files keep
every version; sessions and claims stay as history until an
operator removes them through the API. Keep the hub as you keep
`memory/`: the admin's backup (`sqlite3 … ".backup"`) and the
export are the copies.

## The way back

`GET /api/v1/export` returns one tar whose content is exactly
the synced set, as `memory/` holds it. Unpacked over `memory/`
it is the state of the team without a hub. `leave` in the
`jolitz-hub` skill pulls, exports, then removes `hub.conf`;
nothing in `memory/` changes. Uninstalling the hub keeps
`/var/lib/jolitz-hub`.

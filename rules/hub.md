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
links, audit values) but never rewrites them. Besides the
files it knows what only a service can know:

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
  their provider. The UI of this version writes claims and
  nothing else; memory and changelog are written by jolitz.

## Conflicts

Changelog and audits are append-only; there are no conflicts
there. `memory.md`, `todo.md`, `rules.md` and `network.md` can
be edited on two workstations at once.

- `pull` never overwrites a local file that is newer than the
  hub's and different. It keeps the local file, writes the
  hub's version next to it as `<file>.hub`, lists every such
  file at the end and exits 1.
- `push` sends each file with the checksum it was based on; a
  412 means someone pushed first. Same handling: the hub's
  version lands as `<file>.hub`, the local file stays.
- The user decides per file after `diff -u <file> <file>.hub`:
  keep local and push again, take the hub's (`mv` the `.hub`
  file over it), or merge by hand. jolitz shows the diff and
  asks; it picks no side on its own. A `.hub` file is removed
  once the decision is made; `pull` lists the ones still there.

Presence exists to make this rare: two people on one host see
each other before they edit.

## Claims

A claim says "I am working on this host; ask before you do".
Before working on a host, `bin/jolitz-sync status` shows the
open claims. Another person's claim on the host: say so, name
the person and the note, and ask before continuing. Working
anyway is the user's decision, not jolitz's. Set a claim when
the work spans more than one session or must not be disturbed
(`claim db1 "migration until 20:00"`); release it when done.
A claim is etiquette, not a lock: the hub refuses nothing
because of it.

## Retention

The hub of this version deletes nothing on its own. Files keep
their latest version; sessions and claims stay as history until
an operator removes them through the API. Keep the hub as you keep
`memory/`: the admin's backup (`sqlite3 … ".backup"`) and the
export are the copies.

## The way back

`GET /api/v1/export` returns one tar whose content is exactly
the synced set, as `memory/` holds it. Unpacked over `memory/`
it is the state of the team without a hub. `leave` in the
`jolitz-hub` skill pulls, exports, then removes `hub.conf`;
nothing in `memory/` changes. Uninstalling the hub keeps
`/var/lib/jolitz-hub`.

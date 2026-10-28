# Operate the hub

Users, update, backup, status, leave, uninstall. Commands on the
hub host run over SSH as in `references/install.md`; the hub's
CLI is run as the service user so that `hub.db` keeps its owner.
Linux: `runuser -u jolitz-hub -- <command>`; FreeBSD:
`su -m jolitz-hub -c '<command>'`.

## Users

Roles: `viewer` reads (GET), `operator` also writes files,
sessions, claims and guard reasons, `admin` also manages users.
A person who pushes memory needs `operator`; a manager who
reads the fleet page needs `viewer`. Ask when the user named no
role; propose `operator`.

```
runuser -u jolitz-hub -- /usr/local/bin/jolitz-hub user add bob --role operator
```

The token is the command's output, printed once. Show it to
the operator as it came and never again: not in the summary,
not in memory, not in the changelog (`rules/secrets.md`). The
operator passes it on; the colleague's jolitz takes it with
`references/join.md`. The hub keeps only a SHA-256 of it.

```
runuser -u jolitz-hub -- /usr/local/bin/jolitz-hub user list
runuser -u jolitz-hub -- /usr/local/bin/jolitz-hub user rotate bob
runuser -u jolitz-hub -- /usr/local/bin/jolitz-hub user rm bob
```

`rotate` prints a new token and invalidates the old one at
once; the colleague's next `bin/jolitz-sync` fails with 401
until their token file holds the new one. `rm` ends their
access; their pushed files stay, attributed to the name. Both
are changes on the hub: ask first, `Way back:` for `rotate` is
none (the old token is gone), for `rm` it is `user add` with a
new token. One journal line on the hub host per user change,
without the token (`rules/changelog.md`):

```
logger -t jolitz "[alice as root] jolitz hub: added user bob (operator) — because <the user's reason>"
```

## Update

A hub and its clients run the same jolitz version. After
`bin/jolitz-update` moved the workstation to a new `VERSION`,
`bin/jolitz-sync status` prints the hub's version in its first
line; compare it with `cat VERSION`. The update is steps 4 and
5 of `references/install.md` with the new version, then a
restart:

```
install -o root -g root -m 755 jolitz-hub_linux_amd64 /usr/local/bin/jolitz-hub
systemctl restart jolitz-hub
curl -fsS http://127.0.0.1:7331/api/v1/health
```

The restart interrupts pushes for a second; `systemctl
restart` asks per `rules/service-reload.md` unless
`memory/service-policy.md` lists `jolitz-hub` under
`restart-auto`. `Way back:` the previous binary, kept as
`/var/backups/jolitz/jolitz-hub.<old version>` before the
`install` line; `hub.db` is backed up first (below) because a
new version may migrate the schema.

## Backup

The whole hub is `/var/lib/jolitz-hub/hub.db`. Two copies, both
asked for once and then recorded in the host's `memory.md` as
`- Backup: …` (`jolitz-housekeeping` → backup-presence):

1. On the host, SQLite's online backup, consistent while the
   service runs; needs the `sqlite3` CLI (package `sqlite3` on
   Debian, `sqlite3` on FreeBSD):

   ```
   mkdir -p -m 750 /var/backups/jolitz-hub
   sqlite3 /var/lib/jolitz-hub/hub.db ".backup /var/backups/jolitz-hub/hub-20261028.db"
   find /var/backups/jolitz-hub -type f -mtime +30 -delete
   ```

   Offered as a daily cron line or timer under root, with the
   date computed by the job; the job's command runs once by
   hand first (`rules/way-back.md` → Failures that show
   later). The directory is root's: it holds every host name,
   IP and operator name of the team.
2. From a workstation, the export, a tar of the files exactly
   as `memory/` holds them, the way back to "files only":

   ```
   curl -sS --fail-with-body -H @"$HOME/.config/jolitz/hub-token" \
     -o ~/jolitz-hub-export-20261028.tar \
     https://hub.example.com/api/v1/export
   tar -tf ~/jolitz-hub-export-20261028.tar | head
   ```

Restore of the database: stop the service, copy the `.db` over
`hub.db` with owner `jolitz-hub`, start, health check. Sessions
and claims from after the copy are gone; files come back from
the next push of each workstation.

## Status

`bin/jolitz-sync status` on the workstation: URL and hub
version, `token: accepted (<user>)`, workstation, files to push
and to pull, conflicts, claims, sessions (`--short` is one
line). On the host:
`systemctl --no-pager status jolitz-hub`, `journalctl -u
jolitz-hub --since today --no-pager`, and the UI's `/presence`
page. `bin/jolitz-doctor` prints one `hub:` line when
`hub.conf` exists; nothing otherwise.

A 401 (exit 2) means the token file does not match a user: the
file was changed, or the admin rotated or removed the user. A
403 on `push` means role `viewer`. 409 is a conflict: another
workstation pushed first; the hub's version lands as the
`.hub` file and the line count is printed, `resolve` ends it
(`rules/hub.md` → Versions and conflicts). 428 is a `PUT`
without `If-Match` on a file the hub already has; `jolitz-sync`
never sends one, a hand-written `curl` must add the header.
422 is the lint: the line number in the answer names the value
that looks like a credential, and `push` counts the file as
refused (exit 1). `stale` in the first lines of `status` means
the last pull did not reach the hub: `bin/jolitz-sync pull`
before working on a host (`rules/hub.md` → Pull at session
start).

## Claims and handover

A claim is etiquette: "I am on this host; ask before you do".
The hub refuses nothing because of it (`rules/hub.md` → Claims
and handover). From the workstation:

```
bin/jolitz-sync claim db1.example.com "migration until 20:00"
bin/jolitz-sync claim --keep db1.example.com "migration until 20:00"
bin/jolitz-sync release db1.example.com
bin/jolitz-sync handover db1.example.com
bin/jolitz-sync history servers/db1.example.com/todo.md
```

`claim` sets the claim and prints the handover: claim, sessions
on the host, open to-dos, running jobs (to-do lines naming
`~/.jolitz/jobs/`), the last three changes with their
`Rollback:` line. Without `--keep` the claim ends with the
session: the Stop hook's `session end` releases every claim
the session made (and the next `session start` releases what
a crashed session left). `--keep` leaves it standing until
`release`. Another person's claim is released only by them or
by an admin, who may also take it over; `claim` then exits 1
and names the holder. `handover` prints the same block without
claiming; the UI shows it at the top of the host page, with
the claim and release buttons.

`history` lists the versions of one file with who and when; an
old body is `GET /api/v1/files/<path>?version=N`. The UI's edits
(to-dos, owner, tags, links) appear there as versions by the UI
user and arrive on every workstation with the next pull.

A conflict (`push` or `pull` exit 1, a `.hub` file next to the
local one) ends with one of:

```
bin/jolitz-sync resolve servers/db1.example.com/todo.md --mine
bin/jolitz-sync resolve servers/db1.example.com/todo.md --theirs
```

`--mine` pushes the local file over the hub's current version
(after the memory lint); `--theirs` replaces the local file
with the hub's. Both remove the `.hub` file. The user chooses
after seeing `diff -u <file> <file>.hub`; jolitz never picks a
side on its own.

## Leave

Order matters: first everything the hub has, then the
configuration.

```
bin/jolitz-sync pull
curl -sS --fail-with-body -H @"$HOME/.config/jolitz/hub-token" \
  -o ~/jolitz-hub-export-20261028.tar \
  https://hub.example.com/api/v1/export
tar -tf ~/jolitz-hub-export-20261028.tar | wc -l
```

Then, after the user's OK (`Way back: join again with the same
token file`):

```
rm ~/.config/jolitz/hub.conf
```

Remove the `Hub:` line from `memory/user.md`. `memory/` stays
as it is; jolitz works on files again, solo or with git
(`CLAUDE.md` → Team Usage). The token file stays until the
user deletes it (`rm ~/.config/jolitz/hub-token`, their call)
or the admin runs `user rm`. The hub keeps the person's files
and sessions; ask the admin for `user rm` when the person
leaves the team.

## Uninstall the hub

`references/install.md` → Way back, in full. Before it: the
export or a `.backup` copy, confirmed readable (`tar -tf`,
`sqlite3 … "PRAGMA integrity_check"`), because the data
directory is what the team worked on. Every member runs `leave`
first; otherwise `bin/jolitz-sync` reports the hub as
unreachable (exit 3) at each of their session starts until
`hub.conf` is gone. Changelog and journal line on the host;
`memory.md` loses the `Hub:`, `jolitz-hub:` and `Services`
entries.

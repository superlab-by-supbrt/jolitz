---
name: jolitz-hub
argument-hint: "install <host> | join | user add <name> [role] | status | leave"
description: Install the optional jolitz team hub on a server, join
  a workstation to it, add team members, show the connection or
  leave. Use when the user asks to "set up the jolitz hub on
  <host>", "join the hub", "connect me to the team hub", "add
  <name> to the hub", "hub status" or "leave the hub". Everything
  happens in conversation, like any other install jolitz does;
  the operator provides server access and pastes a token once
  into a file in their own terminal. Nothing is active without
  `~/.config/jolitz/hub.conf`. **Never run automatically** — only
  on explicit user request.
---

# jolitz-hub

The hub is an optional team service: one Go binary with an HTTP
API, a web UI and a SQLite file, on a host of the team. Every
workstation mirrors its `memory/` into it with `bin/jolitz-sync`
and pulls the others' back. `rules/hub.md` says what the hub
holds, what never leaves the workstation and how a conflict is
handled. The hub never runs a command: servers are reached from
the workstations over SSH, as before.

Five subcommands. The user asks for one in their own words; the
skill does the rest and asks only the questions listed here, in
this order, each with its `Way back:` line (`rules/way-back.md`).

A hub and its clients run the same jolitz version: the skill
reads the local `VERSION` file and installs that release of
`jolitz-hub`; after `bin/jolitz-update` the hub is updated with
`references/operate.md` → Update.

## install <host>

`references/install.md` in full. Summary:

1. **Pipeline.** `rules/first-connection.md` for `<host>`, all
   ten steps. The hub host is a server like any other.
2. **Facts before questions.** Local `VERSION`; the host's OS
   and architecture; `rules/service-class-check.md` (a web
   server present?) and `rules/port-check.md` for 7331.
3. **Questions**, in order:
   1. The URL the team will use (`https://hub.example.com`).
      It becomes `--base-url` and the proxy's site name. The
      hostname must resolve to this host before TLS can work.
   2. The first admin's name (the operator's own, usually).
   3. Reverse proxy: new Caddy (automatic TLS), the web server
      already on the host (the block is shown first), or none
      (reachable on `127.0.0.1:7331` only, for a VPN or SSH
      tunnel).
   4. Firewall: 443 (and 80 for Caddy's certificate) open to
      all, restricted to the team's network, or no change. Port
      7331 is never opened.
   5. Database: SQLite on the host (recommended), or the
      team's existing PostgreSQL; then the operator writes the
      DSN into a mode-600 file in their own terminal.
   6. Browser login through the team's identity provider
      (OIDC), optional: issuer, client id, the username claim;
      the client secret goes into a file the same way. The
      first login binds each hub user to the provider's
      identity.
4. **One plan, one OK.** Download with checksum verification,
   user, directories, binary, unit or `rc.d` script, proxy,
   firewall, start, health check. Shown as a list with the way
   back, then executed step by step; each config write follows
   `rules/backups.md` (backup, `diff -u`, config test).
5. **First admin.** `jolitz-hub user add <name> --role admin`
   on the host, as the service user. The command prints the
   token once; it is the command's own output and is never
   repeated, stored or logged.
6. **Record.** `memory.md` of the host, the changelog, the
   journal headline. Then tell the user how the team joins.

## join

`references/join.md` in full. Summary:

1. **Questions**, in order:
   1. The hub URL.
   2. The token: the user pastes it into a file in their own
      terminal (the ` ```sh operator ` block in the reference,
      `install -m 600` then `printf`), or names a file that
      already holds it. jolitz never puts the token into a
      command, a file it writes, memory or the changelog.
   3. The workstation name shown to the team; default
      `hostname -s`, confirmed in one line.
2. **Write** `~/.config/jolitz/hub.conf` (url, token_file,
   workstation), mode 600, after `install -m 600 /dev/null`.
3. **Run** `bin/jolitz-sync status`, then `bin/jolitz-sync
   pull`. List the `.hub` files it wrote, if any
   (`rules/hub.md` → Conflicts).
4. **Record** one line in `memory/user.md`: `Hub: <url>`.

## user add <name> [role]

`references/operate.md` → Users. Over SSH on the hub host, as
the service user: `jolitz-hub user add <name> --role <role>`
(`viewer`, `operator`, `admin`; ask when the user named none,
default `operator`). The token is the command's output, shown
once to the operator, who hands it to the colleague over a
channel of their choice. Nothing is written anywhere.

## status

`bin/jolitz-sync status` on the workstation: URL and hub
version, whether the token is accepted and as whom, the
workstation name, files to push and to pull, conflicts, claims,
sessions. Without `hub.conf` the answer is one line: no hub
configured.

## leave

`references/operate.md` → Leave. In this order: `bin/jolitz-sync
pull`, then `GET /api/v1/export` into a tar file the user keeps,
then remove `hub.conf` and the `Hub:` line from `memory/user.md`.
`memory/` stays as it is; jolitz works on files again, solo or
with git. The token file stays until the user deletes it, or the
admin removes the user with `jolitz-hub user rm <name>`.

## Consent gates

- Every change on the hub host asks first, with `Way back:`.
  The install is one plan with one OK; a step the plan did not
  name asks on its own.
- The firewall change asks separately, as `CLAUDE.md` →
  Firewall Awareness requires, and arms a rollback timer
  (`rules/rollback-timer.md`).
- `leave` asks once and names what it deletes (`hub.conf`) and
  what it keeps (`memory/`, the export).
- No question is skipped for "just set it up"; "do what you
  think is best" is answered with the proposed choice and one
  more confirmation.

## Memory lines

Host memory (`memory/servers/<host>/memory.md`), written by
`install`:

```markdown
- Services: jolitz-hub 3.3.3-jolitz.8
- Hub: https://hub.example.com
- jolitz-hub: port 7331 (tcp, 127.0.0.1)
- Web server: caddy
```

The `Services` value joins the existing list; the version is
the one `jolitz-hub version` printed. `Web server` only when
the skill installed one. Workstation (`memory/user.md`), written
by `join`:

```markdown
Hub: https://hub.example.com
```

Never the token, never the path of the token file in memory;
the path lives in `hub.conf` only.

## References (read on demand)

- `references/install.md` — the install flow with every
  command: download and checksum, user, systemd unit or
  FreeBSD `rc.d` script, Caddy and nginx, firewall, first
  admin, memory and changelog, the way back.
- `references/join.md` — the join flow: token file, `hub.conf`,
  first pull, conflicts, `user.md`.
- `references/operate.md` — users, update, backup, status,
  leave, uninstall.
- `rules/hub.md` — what the hub holds, what stays local, the
  conflict rule, claims, retention, the export.
- `rules/first-connection.md`, `rules/service-class-check.md`,
  `rules/port-check.md`, `rules/backups.md`,
  `rules/way-back.md`, `rules/rollback-timer.md`,
  `rules/notify.md` (secrets only in files),
  `rules/secrets.md`, `rules/changelog.md`.

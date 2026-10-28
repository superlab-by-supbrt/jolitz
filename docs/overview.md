# Fleet overview and the tools around it

Everything here works from `memory/` on your workstation, without
connecting to a server. Back to the [README](../README.md).

## The overview pages

Everything jolitz learns about your servers is in `memory/`.
`bin/jolitz-overview` turns it into pages, without touching a
server:

```bash
bin/jolitz-overview            # memory/overview/index.html
bin/jolitz-overview --eol      # fetch end-of-life dates first
bin/jolitz-overview --tag prod # only servers tagged prod
bin/jolitz-overview --text     # plain text, last 7 days
bin/jolitz-overview --json --prom   # overview.json, jolitz.prom
```

![Fleet overview: servers, end of life, to-dos, changes, audit drift](../assets/overview/overview.png)

- **Needs attention:** releases past or within 180 days of
  their end of life, servers not connected for 90 days,
  settings that differ between servers or changed since the
  last fleet audit, memory files with a problem.
- **Servers:** OS, end of life, IP, SSH port, aliases, tags,
  own rules, last connection (amber after 30 days, red after
  90), open to-dos, last change.
- **Open to-dos, changes** of the last 30 days with detail
  and way back, the **fleet audit** table and, if enabled,
  the **guard denials**.

Tag buttons filter the page. Each server gets a handover
sheet, `memory/overview/hosts/<host>.html`: facts, open
to-dos, standing constraints (the `Flags:` of its
changelog), its `rules.md`, every change with detail and way
back, its audit values and the full `memory.md`. It prints
cleanly.

![Handover sheet of one server](../assets/overview/host.png)

On the demo data in the repository:

```bash
bin/jolitz-overview -d assets/overview/demo -o /tmp/fleet-demo
open /tmp/fleet-demo/index.html   # Linux: xdg-open, Windows: start
```

**Tags.** Tell jolitz what a server is for; it records the
tags in the server's `memory.md` (`- Tags: prod, web`). The
fleet audit, `--tag` and the summary mail take a tag.

**End of life.** `--eol` asks [endoflife.date](https://endoflife.date)
for the releases in your memory and caches the answer in
`memory/eol/`. Only product names such as `debian` leave your
machine, never a host name. Without `--eol` the cache is
used. Known: Debian, Ubuntu, RHEL, Rocky, AlmaLinux, CentOS
Stream, Fedora, openSUSE Leap, SLES, FreeBSD, NetBSD, macOS,
Alpine, Proxmox VE, OPNsense.

**Plain text and mail.** `--text` on the demo data:

```
$ bin/jolitz-overview -d assets/overview/demo --text --tag prod
jolitz fleet summary, 2026-10-28, tag prod
Hosts: 2, open to-dos: 2, changes in 7 days: 1

Changes, last 7 days
  2026-10-27 web1.example.com: nginx now sends HSTS for app.example.com — because the security audit flagged its absence

Open to-dos
  db1.example.com: Upgrade to Debian 12 (Bookworm)
  db1.example.com: Move PostgreSQL 13 to 17 with pg_upgradecluster

Not connected for 30 days or more
  none

End of life
  db1.example.com: Debian 11 (Bullseye), ended 2026-08-31

Fleet audit 2026-10-27 09:00: 3 settings differ, 2 changed since 2026-09-30 09:00
  db1.example.com firewall.state: active -> inactive
  db1.example.com time.service: chrony -> systemd-timesyncd

Memory lint: 0 warnings
EOL dates: endoflife.date, fetched debian 2026-10-28, macos 2026-10-28, rocky-linux 2026-10-28, ubuntu 2026-10-28
```

Every Monday by cron:

```
23 7 * * 1 cd /path/to/jolitz && flock -n \
  ~/.cache/jolitz/cron-fleet.lock timeout 10m \
  env JOLITZ_NO_UPDATE=1 \
  /abs/path/to/claude --permission-mode auto \
  -p "Email me the weekly fleet summary" \
  >> ~/jolitz-cron.log 2>&1
```

**Audit history.** Each fleet audit saves its values in
`memory/audits/<YYYY-MM-DD-HHMM>.json` and reports what
changed since the previous one. A change no changelog entry
explains was made outside jolitz, and the report says so.
Format: `.claude/skills/jolitz-fleet-audit/references/snapshot.md`.

**Memory lint.** `bin/jolitz-memory-lint` checks the memory
files. jolitz runs it after each memory update, the overview
before it writes a page:

```
$ bin/jolitz-memory-lint
WARN  db1.example.com: memory.md: tag "DB" is not lowercase letters, digits and -
ERROR web1.example.com: servers/web1.example.com/memory.md:14: looks like a secret value; record its location, not the value (rules/secrets.md)
memory lint: 4 hosts, 1 errors, 1 warnings
```

- **ERROR:** something that looks like a credential: a
  private key block, a known token format, a password in a
  URL or in `PASSWORD=`, a password hash. The line number is
  printed, never the value. The overview refuses to build
  until it is gone, so a secret never lands in a page you
  forward. `Database password: in /root/.pgpass (0600)` is a
  location and passes.
- **WARN:** no `OS:` or `Last connected:` line, a date not in
  `YYYY-MM-DD`, a tag not in lowercase, a `memory.md` over 40
  lines, a `todo.md` with nothing open, a changelog entry
  without timestamp. `--strict` fails on warnings too.

The patterns are a backstop; a secret in a format they do
not know passes.

## More views from the same memory

- **Owner and services.** `- Owner: alice` and
  `- Services: nginx 1.26, postgresql 17` in a host's
  `memory.md` put the owner into the Servers table and the
  handover sheet, and build a services matrix: which host
  runs which service in which version, without a connection.
- **Timeline.** `memory/overview/timeline.html`: every change
  of the window on one axis, one lane per host, standing
  constraints (`Flags:`) marked.
- **Network.** `- Link: web1.example.com -> db1.example.com
  (postgresql 5432)` lines in `memory/network.md` are drawn as
  a graph on the overview; prose around them stays prose.
- **Machine-readable.** `--json` writes
  `memory/overview/overview.json` (hosts, to-dos, changes,
  end of life, audit, guard counts), `--prom` writes
  `memory/overview/jolitz.prom` for a Prometheus textfile
  collector: days to end of life, days since the last
  contact, open to-dos per host, guard denials. Grafana or an
  alert rule can then do the nagging.

## Around the tool

```bash
bin/jolitz-doctor                        # is the workstation set up right?
bin/jolitz-log --grep nginx --since 2026-10-01   # what did we change?
bin/jolitz-guard-report                  # denials as an issue template
```

`jolitz-doctor` checks Claude Code version, guard
registration, `jq`, `curl`, the socket directory, git state
(pinned, behind, dirty), `memory/user.md`, memory lint, the
age of the EOL cache, log sizes and the SSH agent; one line
each, `OK`, `WARN` or `FAIL`, exit 2 on a FAIL so cron can
tell. `jolitz-log` searches every host's changelog by host,
tag, date and pattern. `jolitz-guard-report` turns the guard
log into a table by reason, commands only on request.

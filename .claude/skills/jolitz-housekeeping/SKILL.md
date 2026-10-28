---
name: jolitz-housekeeping
argument-hint: "[hostname]"
description: Run a jolitz housekeeping (health) inspection on a
  server — disk, memory, load, pending updates, firewall, SSL
  certs, failed systemd units, logs, kernel reboot status, and
  service-specific checks. Use when the user asks to "run
  housekeeping", "housekeeping report", "run a health check on
  <host>", or "do routine inspection". Do NOT auto-invoke for
  ambiguous requests like "check server <host>" — that's
  reserved for quick queries. Covers Linux (Debian, Ubuntu, RHEL,
  CentOS, Fedora, SUSE), FreeBSD, NetBSD and macOS.
---

# jolitz-housekeeping

Routine health inspection for a server or the local machine.
**Never run automatically** — only on explicit user request. The
whole of the jolitz first-connection onboarding pipeline still
applies before any of this runs.

## Workflow

1. **Load overrides.** Before running any check, apply the full
   jolitz rule-override chain (later wins):
   - `memory/custom-rules/jolitz-housekeeping.md` if present
     (global custom overrides for this skill — `## Add:`,
     `## Replace:`, `## Remove:` prefixes per `CLAUDE.md`).
   - `memory/housekeeping.md` if present (additional cross-
     server custom checks — free-form Markdown, gitignored by
     default).
   - `memory/servers/<hostname>/memory.md` (service list,
     last-known state, per-server quirks).
   - `memory/servers/<hostname>/rules.md` if present (per-server
     rule overrides — same prefixes as above, highest precedence).
   Note: `memory/custom-rules/all.md` is already loaded by the
   CLAUDE.md session-start preflight — do not re-read it.
2. **Select checks.** Run all baseline checks for the detected OS
   plus any service-specific checks triggered by entries in the
   server's `memory.md` (e.g. PostgreSQL, nginx, Docker). The
   backup-presence check from `references/backup-presence.md`
   runs on every host, independent of `memory.md` entries.
   Its restore test (one small file into
   `/root/jolitz-scratch/`, checksum, remove) runs only after
   the user says yes.
3. **Run the version check** procedure from
   `rules/version-check.md` for all Tier 1 software and include the
   "Versions" section in the report.
4. **Run checks in 2–3 parallel batches** for speed — not one
   massive batch. If a single parallel tool call errors, Claude
   Code cancels sibling calls, so grouping limits blast radius.
5. **Emit the report** using the format in
   `references/report-format.md`.
6. **Update `memory.md`** immediately after, if the checks
   revealed changed facts (disk usage shifted significantly, a
   new service appeared, a service was removed).
7. **Log the summary** to the system journal and mirror to the
   local changelog per `rules/changelog.md`:

       logger -t jolitz "Housekeeping: 1 CRITICAL, 2 WARN, \
       all services OK"

## References

Read on demand, only when the relevant section applies:

- `references/report-format.md` — required output format and
  severity rules (CRITICAL / WARN / INFO).
- `references/baseline-linux.md` — disk, memory, load, uptime,
  updates, firewall, NTP, logs, SSL certs, kernel.
- `references/baseline-macos.md` — disk, memory, load, updates,
  Homebrew, Application Firewall, SMART, time sync.
- `references/baseline-freebsd.md` — disk and ZFS, memory,
  load, `pkg` and `freebsd-update`, pf, rc services, time,
  logs, certs, kernel vs userland, boot environments.
- `references/baseline-netbsd.md` — the same shape for
  NetBSD: `pkgin`, `pkg_admin audit`, npf, rc services,
  `/etc/security`, syslog with `grep -a`.
- `references/backup-presence.md` — generic "any backup at
  all?" probe, the provider-snapshot question, the restore
  test (`Backups  OK — verified <file> from <date>`), and
  the `Backup:` acknowledgment line in `memory.md`.
- `references/service-checks.md` — PostgreSQL, backups, nginx,
  Docker, Ollama, node_exporter, NVIDIA GPU, MariaDB/MySQL,
  WireGuard. Only run the ones the server's `memory.md` mentions.
- `references/unprivileged.md` — which checks work without root
  and how to report skipped ones.

## Scope and limits

- Linux (Debian, Ubuntu, RHEL, CentOS, Fedora, SUSE),
  FreeBSD, NetBSD and macOS are covered by the baseline
  references above. On NetBSD every remote command starts
  with the PATH export from `rules/netbsd.md`. Unraid hosts:
  `rules/unraid.md` first.
- The BSD baselines have no `needrestart` equivalent: a
  daemon upgraded by `pkg` or `pkgin` keeps its old binary
  until restarted, and the report says which ones.

## Custom checks

Users add their own checks in `memory/housekeeping.md`
(gitignored, free-form Markdown). The file describes what to
check, what commands to run, and what thresholds to use. Do not
pre-create an empty file — it is created when needed.

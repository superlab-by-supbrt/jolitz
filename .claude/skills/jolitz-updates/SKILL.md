---
name: jolitz-updates
argument-hint: "[hostname ... | tag:<name>]"
description: Plan and run package updates on the fleet, a tag
  or named hosts — per host a dry run (apt-get, dnf, zypper,
  pkg, freebsd-update, pkgin), the reboot-required state, then
  one table and a proposed order. Applies nothing before the
  user's OK; applies as a job per rules/ssh-connections.md,
  never with a reboot on the same line. Use when the user asks
  "what updates are pending", "update the fleet", "patch the
  web servers", "plan the maintenance window". Do NOT
  auto-invoke for one host's pending count — housekeeping
  covers that.
---

# jolitz-updates

Updates for many hosts in one pass: first every host's dry
run, then one table, then a proposed order. Applying waits
for the user's OK and runs per host as a job, so a dropped
connection or the tool's time limit never leaves a package
manager half done. **Never run automatically** — only on
explicit user request.

## Workflow

1. **Scope.** All directories under `memory/servers/` that
   name a real host, or the hosts given as arguments, or
   `tag:<name>` (hosts whose `memory.md` carries `<name>` in
   `- Tags:`). Hosts without an SSH user in `memory/user.md`,
   unreachable hosts and hosts listed in `memory/readonly.md`
   or `memory/blacklist.md` go on a skipped list with the
   reason. Read-only hosts get the dry run and nothing else.
2. **Pipeline per host.** `rules/first-connection.md` in
   full, every host, every time. A host whose `todo.md`
   records a running job (`rules/ssh-connections.md` → 4)
   is read back first, never given a second job.
3. **Dry run per host**, one SSH call each, in parallel:
   the commands of `references/linux.md` or
   `references/freebsd.md` for the detected family. macOS
   hosts: `softwareupdate --list` and `brew outdated`
   (`rules/macos.md`); they appear in the table and are
   applied only by hand, one at a time, with the user.
4. **Reboot and restart state** from the same call: the
   `reboot-required` file, `needrestart -b`, the running
   kernel against the installed one, `freebsd-version -k`
   against `freebsd-version`.
5. **Table and proposed order** in the format of
   `references/report-format.md`. Order: hosts of one
   service class never at the same time (both database
   replicas, both mail relays); hosts that need a reboot
   last; a host with a staged kernel first in its group so
   the reboot question comes early. Propose a start time
   only when `memory/user.md` or a host's `memory.md` names
   a maintenance window; otherwise ask. The question ends
   with the `Way back:` line for the whole run
   (`rules/way-back.md`).
6. **Apply only after OK**, host by host in the agreed
   order, each as a job (the block in the family reference).
   Before the first job on a ZFS host, a snapshot or boot
   environment (`bectl create`, `rules/freebsd.md`); on
   Linux, the package cache keeps the previous versions
   (`/var/cache/apt/archives`, `dnf`'s cache) unless the
   host cleans it. Write the `Rollback:` line of the
   changelog before the job starts.
7. **Read the job back** with one waiting call
   (`rules/ssh-connections.md` → 4), up to the tool's
   limit. `status` holds the exit code. No `status` after
   the wait: say "running", record the directory in
   `todo.md`, come back. Never start a second job on the
   host meanwhile.
8. **Restarts.** `needrestart -b` after the job lists the
   services that run old binaries. A reload auto-proceeds
   under `rules/service-reload.md`; a restart asks, per
   service, with the way back. Deferred services
   (`docker.service`, `dbus`, `systemd-logind`) are listed,
   not restarted.
9. **Reboot is its own step.** After the status is read
   and the user asks for it; one host at a time; a fresh
   login confirms it is back before the next host starts.
   Never on the same line as the upgrade, never inside the
   job. A host that stays down: `rules/ssh-unreachable.md`.
10. **Record.** Per host one journal headline
    (`logger -t jolitz "[<operator> as root] upgraded N
    packages (K security) — because <reason>"`), the
    local changelog with `Detail:` (the package list from
    `out`), `Rollback:` and `Verify:`, `memory.md` for
    changed versions and kernel, `todo.md` for a reboot
    still owed. Hosts that were only dry-run log
    `read-only: pending updates listed`.

## References

Read on demand:

- `references/linux.md` — dry run, security subset, reboot
  and restart state, and the apply job for Debian/Ubuntu,
  RHEL family and SUSE.
- `references/freebsd.md` — the same for FreeBSD (`pkg`,
  `freebsd-update`) and NetBSD (`pkgin`, `sysupgrade`).
- `references/report-format.md` — the table, the proposed
  order, the result lines.

## Scope and limits

- Routine upgrades within a release. A release upgrade
  (`dist-upgrade` to a new codename, `freebsd-update
  upgrade -r`, `sysupgrade`) is a project with its own
  plan, snapshot and reboot; this skill reports that one is
  due and stops.
- Counts come from the dry run of this session, never from
  `memory.md` or the last housekeeping.
- Hosts under a guest application (`rules/guest-software.md`
  where present, `- Guest:` in `memory.md`) are dry-run only
  until the application's admin agreed to the window.
- No reboot, ever, without the user asking for that host
  by name after the result was read.

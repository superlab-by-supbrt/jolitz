# Updates Report Format

One table for the scope, then the proposed order, then — only
after the user's OK and the jobs ran — one result line per
host. In the user's language; facts, one per line.

## Before applying

```
## Updates: tag:web (3 hosts)

Date: 2026-10-28 14:05
Skipped: old1.example.com (unreachable), db2.example.com (read-only: dry run only)

| Host | OS           | Pending | Security  | Reboot  | Restart          |
|------|--------------|---------|-----------|---------|------------------|
| web1 | Debian 13    | 12      | 3         | no      | nginx            |
| web2 | Debian 13    | 12      | 3         | kernel  | nginx, postfix   |
| bsd1 | FreeBSD 14.3 | 4 pkg   | 1 (audit) | no      | postgresql       |

### Proposed order

1. web2: upgrade, reload nginx, restart postfix (asks), then
   the reboot question. web1 waits until web2 answers again.
2. web1: upgrade, reload nginx.
3. bsd1: pkg upgrade; freebsd-update fetch lists nothing.
Window: none recorded in memory — when?
Way back: web1/web2 previous .deb files in /var/cache/apt/archives;
bsd1 boot environment pre-update-20261028 (bectl activate).
```

## After applying

```
### Result

web2  OK    12 upgraded, status 0, nginx reloaded, postfix restarted
web2  reboot owed (kernel 6.12.43 installed, 6.12.41 running)
web1  OK    12 upgraded, status 0, nginx reloaded
bsd1  FAIL  pkg upgrade status 1: lock held by pid 8812 (err tail below)
```

## Rules

- **Pending** is the dry-run count of this session. FreeBSD
  shows `N pkg` and, when `freebsd-update fetch` listed
  files, `+ base`. macOS shows `N os, M brew`.
- **Security** is the distro's own filter: the `-security`
  origins on Debian, `--security` on dnf, `list-patches
  --category security` on zypper, `pkg audit` on FreeBSD,
  `pkg_admin audit` on NetBSD. `n/a` where there is none.
- **Reboot**: `no`, `kernel`, `pending (since <date>)`, `?`.
- **Restart**: the `NEEDRESTART-SVC` names, or the daemons
  in the dry-run list where there is no `needrestart`.
- A host whose dry run failed keeps its row with `?` in
  every cell and one line under the table saying why.
- The proposed order carries no start time unless memory
  names a window; then the `Window:` line is a question.
- The `Way back:` line is mandatory before the OK
  (`rules/way-back.md`); `none` is a valid value for a host
  without snapshot or cache, and is said as such.
- **Result**: one line per host, `OK` or `FAIL` with the exit
  status; a second line when a reboot is owed. A `FAIL` line
  quotes the last `err` line, never more than one.
- Skipped hosts appear in the header and nowhere else.

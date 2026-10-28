# Incident Triage

Read-only probes, grouped by symptom. Every command here
reads; none restarts, deletes or writes. Bound every log
read (`rules/log-queries.md`). Commands come from the
loaded `rules/<family>.md` and the housekeeping baselines;
where a flag is not named there, check `--help` on the host
before relying on it (`CLAUDE.md` → Verify Before Running).

## Common batch (every symptom, Linux)

One SSH call, before any branch:

```bash
uptime
nproc
df -h --output=target,pcent,size,used,avail \
  -x tmpfs -x devtmpfs -x overlay
free -h
systemctl --failed --no-pager --no-legend
journalctl -t jolitz --since "7 days ago" --no-pager -q \
  -n 20
journalctl --since "48 hours ago" -p err --no-pager -q \
  -n 100
```

FreeBSD: `df -h`, `swapinfo`, `uptime`, `sysctl -n hw.ncpu`,
`tail -n 200 /var/log/messages`. NetBSD: the same after
`export PATH=/sbin:/usr/sbin:/usr/pkg/sbin:$PATH;`, and
`grep -a` on `/var/log/messages` (`rules/netbsd.md`).
macOS: `df -h /`, `uptime`, `log show --last 2h --predicate
'messageType == error' --info`.

Read the batch before choosing a branch: a 502 with a full
disk is a disk incident.

## What changed (every symptom)

Locally, without SSH:

```bash
bin/jolitz-log --host HOST --since $(date -v-7d +%F \
  2>/dev/null || date -d '7 days ago' +%F)
grep -n -E '^\s+(Flags|Rollback):' \
  memory/servers/HOST/changelog.log | tail -n 10
cat memory/servers/HOST/todo.md 2>/dev/null
ls -t memory/audits/*.json 2>/dev/null | head -n 1
```

On the host, bounded to the window of the symptom:

```bash
# Debian/Ubuntu package history, newest first
tail -n 60 /var/log/apt/history.log
# Files under /etc changed since the symptom began
find /etc -type f -newermt "YYYY-MM-DD HH:MM" 2>/dev/null \
  | head -n 30
# A job a previous session left running
ls -lt ~/.jolitz/jobs 2>/dev/null | head -n 5
```

RHEL: `dnf history` lists transactions (check `dnf history
--help` for the detail form). SUSE: `/var/log/zypp/history`.
FreeBSD: `/var/log/messages` holds `pkg:` lines;
`freebsd-update` logs to `/var/db/freebsd-update/`.

## Branch: unreachable

The host does not answer SSH. Read
`rules/ssh-unreachable.md` and follow it: a fresh-login
attempt with the fresh-login options from `CLAUDE.md`, the
port from memory, then the distinction between "host down",
"sshd down" and "firewall". Nothing in this skill scans
ports or tries other accounts. Report what the user can
check at the provider (console, power state) as a question,
not as a fact.

## Branch: 5xx, site down, TLS error

```bash
nginx -t 2>&1
systemctl is-active nginx
tail -n 50 /var/log/nginx/error.log
# The upstream nginx proxies to: PORT from the vhost
ss -tlnp 'sport = :PORT'
journalctl -u APP --since "2 hours ago" --no-pager -q -n 100
echo | openssl s_client -connect localhost:443 \
  -servername "$(hostname -f)" 2>/dev/null \
  | openssl x509 -enddate -noout 2>/dev/null
```

Typical findings, in the order to rule them out:

- `connect() failed (111)` in the error log and nothing on
  the upstream port: the application is down. Its journal
  names why (crash, port in use, failed migration).
- `upstream timed out (110)`: the application is up and
  slow. Go to the load branch.
- `no live upstreams` after a deploy: the unit was renamed
  or the port moved. Compare the vhost with
  `systemctl list-units --type=service --no-pager
  --no-legend | grep -i APP`.
- `nginx -t` fails: a config edit without a reload test.
  The backup in `/var/backups/jolitz/` is the way back.
- Certificate expired: a TLS error at the client, not a
  5xx. Then `certbot renew --dry-run` output and the renewal
  timer (`systemctl list-timers --all | grep -i certbot`).
- Apache instead of nginx: `apachectl configtest`,
  `/var/log/apache2/error.log` (Debian) or
  `/var/log/httpd/error_log` (RHEL).

FreeBSD: `/usr/local/etc/nginx/`, `service nginx status`,
`/var/log/nginx/error.log`; ports with `sockstat -4 -6 -l`
(`rules/port-check.md`).

## Branch: load

```bash
uptime
nproc
ps -eo pid,user,pcpu,pmem,etime,comm --sort=-pcpu | head -n 15
vmstat 1 3
journalctl --since "24 hours ago" -k --grep="Out of memory" \
  --no-pager -q | wc -l
```

- Load above the core count with high `wa` in `vmstat`:
  I/O bound. Then `iostat -x 1 3` where sysstat is
  installed, and the disk branch for a filling filesystem.
- One process at 100 % for hours: name it and its start
  time (`etime`). A cron job, a runaway backup and a stuck
  package manager look alike in `uptime` and differ in
  `comm`.
- Many short-lived processes (`ps` shows a new pid each
  call): a crash loop. The unit's journal shows the
  restarts.
- macOS and the BSDs: `top` in batch mode; the flag differs
  per system, check `man top` on the host before using it.

The skill reports the process. It does not stop one: that
is a change, and it asks first.

## Branch: disk full

```bash
df -h --output=target,pcent,size,used,avail \
  -x tmpfs -x devtmpfs -x overlay
df -i
du -xsh /var/log /var/lib/docker /var/cache /var/tmp /tmp \
  /home /root /srv /opt 2>/dev/null | sort -h
journalctl --disk-usage
lsof +L1 2>/dev/null | head -n 20
```

Then one level deeper into the largest directory:

```bash
du -xh -d 1 DIR 2>/dev/null | sort -h | tail -n 10
```

- `df` full but `du` small: a deleted file still open
  (`lsof +L1`). A restart of that process frees it; ask.
- Inodes at 100 % (`df -i`) with space left: millions of
  small files, usually a session or cache directory.
- Docker hosts: `docker system df`
  (`jolitz-docker` → `references/inventory.md`). A prune
  deletes; ask.
- The journal above its limit: `journalctl --disk-usage`
  against `SystemMaxUse` in `/etc/systemd/journald.conf`.
- Never propose a bigger disk at the provider
  (`CLAUDE.md` → Work with the disk the server has). The
  proposed fix names what to delete or rotate, with its
  size, and asks.

FreeBSD: `df -h`, `zfs list -o name,used,avail,refer
-s used`, `zfs list -t snapshot` (snapshots hold space),
`du -xh -d 1`.

## Branch: memory

```bash
free -h
ps -eo pid,user,rss,comm --sort=-rss | head -n 10
journalctl --since "7 days ago" -k --grep="Out of memory" \
  --no-pager -q -n 20
```

- An OOM line names the killed process and the one that
  held the memory. Report both; the killed one is the
  symptom.
- Swap in use with memory free: old pages, not a problem.
  Swap full and climbing: the leak is in the top `rss`
  process.
- A database with a cache sized above the host (PostgreSQL
  `shared_buffers`, MariaDB `innodb_buffer_pool_size`) is
  a config finding, not a hardware one.

## Branch: one service

```bash
systemctl status SVC --no-pager -n 30
journalctl -u SVC --since "2 hours ago" --no-pager -q -n 100
systemctl cat SVC --no-pager | grep -E '^(ExecStart|EnvironmentFile|User)='
```

Then the service's own config test: `nginx -t`,
`apachectl configtest`, `postfix check`, `pg_isready`,
`sshd -T` as root (a read; writing `sshd_config` is a
taboo). A failed unit after a package upgrade: the
`Critical Services` check in `jolitz-housekeeping` →
`references/baseline-linux.md` shows whether the binary
on disk is newer than the running process.

FreeBSD: `service SVC status`, the daemon's log under
`/var/log/`, config under `/usr/local/etc/`. macOS:
`launchctl list | grep -i SVC`, `brew services list`; note
that `brew upgrade` leaves the old process running
(`rules/macos.md`).

A service that is stopped is started only after the user
says yes (`rules/service-reload.md`: start always asks).

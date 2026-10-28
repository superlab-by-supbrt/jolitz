# Baseline Checks — FreeBSD

Run these on every FreeBSD server. Commands come from
`rules/freebsd.md`: no `systemctl`, no `journalctl`, no
`/etc/os-release`; third-party config lives under
`/usr/local/etc/`. A flag not named there is checked with
`--help` or the man page on the host first.

## Backup Presence

Run the generic "any backup at all?" check — see
`references/backup-presence.md` (FreeBSD and NetBSD probes).
It runs on every host, independent of `memory.md` entries.

## Disk Usage

```bash
df -h
zpool list 2>/dev/null
zpool status 2>/dev/null | grep -E '^ *(state|status|errors):'
zfs list -t snapshot -o name,used,creation -s creation \
  2>/dev/null | tail -n 5
```

- **WARN** if any filesystem > 85% used, or a pool's `CAP`
  > 80%
- **CRITICAL** if any filesystem > 95% used, or a pool's
  `state` is not `ONLINE`
- **INFO** when snapshots hold a notable share of the used
  space; freeing it destroys snapshots, which is a question

## Memory and Swap

```bash
sysctl -n hw.physmem
sysctl -n hw.pagesize
sysctl -n vm.stats.vm.v_free_count
sysctl -n vm.stats.vm.v_inactive_count
swapinfo
```

Available memory is (free + inactive pages) × page size.

- **WARN** if available memory < 10% of total
- **WARN** if swap `Used` > 50% of its size

## System Load

```bash
uptime
sysctl -n hw.ncpu
```

- **WARN** if 15-minute load average > core count

## Uptime and Reboot Detection

```bash
uptime
sysctl -n kern.boottime
```

- **INFO** unexpected reboot detected (compare with the
  memory file's last connected date)

## Pending Updates

Two layers (`rules/freebsd.md`): `freebsd-update` patches
the base system, `pkg` the packages. Both are reported.

```bash
freebsd-version
freebsd-version -k
grep -h url /etc/pkg/FreeBSD.conf /usr/local/etc/pkg/repos/*.conf \
  2>/dev/null
pkg update 2>&1 | tail -n 2
pkg upgrade -n 2>&1 | tail -n 40
pkg audit -F 2>&1 | tail -n 20
```

`freebsd-update --not-running-from-cron fetch` lists the
base patches that `install` would apply. It downloads them
into `/var/db/freebsd-update/`: a write, so skip it on a
read-only host and say so in the report.

- **WARN** if `pkg audit` names any package
- **WARN** if base patches are pending
- Report the package count and the branch (`quarterly` or
  `latest` from the `url` line)

## Automatic Security Updates

There is no `unattended-upgrades` equivalent
(`rules/freebsd.md`). Look for the notify-only baseline:

```bash
crontab -l 2>/dev/null \
  | grep -E 'pkg (upgrade|audit)|freebsd-update'
grep -rE 'pkg (upgrade|audit)|freebsd-update' /etc/cron.d \
  /etc/crontab 2>/dev/null
ls /etc/periodic/security/ 2>/dev/null
grep -i pkgaudit /etc/defaults/periodic.conf /etc/periodic.conf \
  2>/dev/null
```

The daily `periodic` security run mails root; the `ls`
shows its scripts and the `grep` whether the pkg audit
among them is switched on.

- **INFO** if no notify job exists: recommend the cron line
  from `rules/freebsd.md`. Applying updates automatically
  is the user's decision, not a recommendation.

## Firewall Status

Needs root. Without it `pfctl` prints a permission error:
report "skipped: needs root", never "disabled".

```bash
sysrc -a 2>/dev/null | grep -E '^pf_enable'
pfctl -s info 2>&1 | head -n 1
pfctl -s rules 2>&1 | head -n 20
```

- **CRITICAL** if `Status: Disabled`, or `pf_enable` is not
  `YES` (the rules are gone at the next boot)
- **WARN** if the rules show no `block` of incoming traffic
  (`rules/freebsd.md`: the default policy blocks incoming)

## Enabled Services Not Running

```bash
for s in $(service -e); do "$s" status 2>/dev/null; done \
  | grep 'is not running'
```

- **WARN** per enabled service that is not running, by
  name. One-shot rc scripts print no status line: not a
  finding.

## NTP / Time Sync

```bash
sysrc -a 2>/dev/null | grep -E '^(ntpd|chronyd|openntpd)_enable'
service ntpd status 2>&1
ntpq -pn 2>/dev/null
```

- **WARN** if no time daemon is enabled, or `ntpq -pn`
  shows no `*` peer. chrony: `chronyc tracking`, after
  `chronyc help` on the host.

## Log Anomalies

Syslog files, bounded (`rules/log-queries.md`): `-m` for
the newest matches, no `zgrep` over rotated files.

```bash
grep -m 20 -iE 'out of swap|swap_pager|I/O error|CAM status' \
  /var/log/messages
grep -c -E 'Failed password|Invalid user' /var/log/auth.log
```

- **WARN** if any out-of-swap or I/O error line
- **INFO** if > 100 failed SSH logins in the current file

## SSL/TLS Certificate Expiry

Only on hosts whose `memory.md` names a web server or a TLS
service. certbot from ports keeps its files under
`/usr/local/etc/letsencrypt/live/`; BSD `date` converts with
`-j -f`.

```bash
for cert in /usr/local/etc/letsencrypt/live/*/cert.pem; do
  [ -f "$cert" ] || continue
  domain=$(basename "$(dirname "$cert")")
  end=$(openssl x509 -enddate -noout -in "$cert" 2>/dev/null \
    | cut -d= -f2)
  days=$(( ($(date -j -f '%b %e %T %Y %Z' "$end" +%s) \
    - $(date +%s)) / 86400 ))
  echo "$domain: ${days}d remaining"
done
```

No certbot files: the listening port, as in the Linux
baseline:

```bash
echo | openssl s_client -connect localhost:443 \
  -servername "$(hostname -f)" 2>/dev/null \
  | openssl x509 -enddate -noout 2>/dev/null
```

- **CRITICAL** if any cert expires in < 7 days
- **WARN** if any cert expires in < 30 days

## Kernel: Running vs Installed

```bash
freebsd-version -k
freebsd-version
bectl list 2>/dev/null
```

- **INFO** if the running kernel (`-k`) is older than the
  userland: a base update is installed and waits for its
  reboot, which asks and is its own step
- **INFO** the boot environments and the active one. A
  boot environment from before the last update is the way
  back (`rules/way-back.md`)

## Services: Running Binary vs Installed Package

No `needrestart` here. After `pkg upgrade`, a daemon keeps
the old binary until it is restarted. `pkg info -f <name>`
prints when the package was installed (check `pkg info
--help`); `ps -axo pid,lstart,comm` shows when the process
started.

- **WARN** for each daemon from `memory.md` whose package
  was installed after its process started. A restart asks
  (`rules/service-reload.md`).

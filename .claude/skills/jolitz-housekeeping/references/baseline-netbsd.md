# Baseline Checks — NetBSD

Run these on every NetBSD host. Every remote command starts
with `export PATH=/sbin:/usr/sbin:/usr/pkg/sbin:$PATH;`
(`rules/netbsd.md`); the blocks below assume it. No
`systemctl`, no `journalctl`, no `sysrc`, no `sudo` in base.
A flag not named in the rule is checked with `-h` or the man
page on the host first.

## Backup Presence

Run the generic "any backup at all?" check — see
`references/backup-presence.md` (FreeBSD and NetBSD probes),
including its note on `/var/backups/`.

## Disk Usage

```bash
df -h
```

- **WARN** if any filesystem > 85% used
- **CRITICAL** if any filesystem > 95% used

## Memory and Swap

```bash
sysctl -n hw.physmem64
vmstat -s | grep -E 'bytes per page|pages free|pages inactive'
swapctl -l
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

```bash
uname -r
head -n 1 /etc/release
pkgin update 2>&1 | tail -n 2
pkgin -n upgrade 2>&1 | tail -n 40
pkg_admin fetch-pkg-vulnerabilities 2>&1 | tail -n 2
pkg_admin audit 2>&1 | tail -n 20
```

No `pkgin` on the host: `pkg_info` lists what is installed,
and installing `pkgin` is ordinary work that asks first
(`rules/netbsd.md`). Base updates to a new point release go
through `sysupgrade`, replace the kernel and need a reboot:
report them as due, never run them here. Whether the release
series is still supported comes from netbsd.org/releases
(`rules/version-check.md`).

- **WARN** if `pkg_admin audit` names any package
- **WARN** if the release series is no longer supported
- Report the `pkgin` count

## Automatic Security Updates

Nothing applies updates on its own (`rules/netbsd.md`). The
daily `/etc/security` run audits installed packages and
mails root; that is the notify-only baseline.

```bash
grep -E 'check_pkg_vulnerabilities' /etc/security.conf \
  /etc/defaults/security.conf 2>/dev/null
```

- **WARN** if `check_pkg_vulnerabilities=NO` in
  `/etc/security.conf`
- **INFO** that applying updates is the user's decision

## Firewall Status

Needs root. Without it `npfctl` fails: report "skipped:
needs root", never "inactive".

```bash
grep -E '^npf=' /etc/rc.conf
npfctl show 2>&1 | head -n 1
npfctl show 2>&1 | grep -E 'block all|pass stateful in' | head -n 5
```

- **CRITICAL** if `npfctl show` does not say
  `filtering: active`, or `npf=YES` is missing from
  `/etc/rc.conf` (nothing filters after the next boot)
- **WARN** if the loaded rules show no `block all` in the
  default group (`rules/netbsd.md`)

## Enabled Services Not Running

```bash
for s in $(service -e); do "$s" status 2>/dev/null; done \
  | grep 'is not running'
```

- **WARN** per enabled service that is not running, by
  name. Scripts without a status line are not a finding.

## NTP / Time Sync

```bash
grep -E '^(ntpd|ntpdate)' /etc/rc.conf
ntpq -pn 2>/dev/null
```

With `ntpd_flags="-4"` the query is `ntpq -4 -pn 127.0.0.1`
(`rules/netbsd.md`).

- **WARN** if `ntpd=YES` is missing or `ntpq` shows no `*`
  peer. An IPv6 route that goes nowhere is the usual cause;
  the rule names the check (`ntpdate -q -4` against `-6`)
  and the fix, which asks.

## Log Anomalies

Syslog only; `grep -a`, because `messages` can hold NUL
bytes after an unclean shutdown (`rules/netbsd.md`). Bound
with `-m` (`rules/log-queries.md`).

```bash
grep -a -m 20 -iE 'panic|out of swap|I/O error' /var/log/messages
grep -a -c -E 'Failed password|Invalid user' /var/log/authlog
```

- **WARN** if any panic, out-of-swap or I/O error line
- **INFO** if > 100 failed SSH logins in the current file

## SSL/TLS Certificate Expiry

Only on hosts whose `memory.md` names a TLS service. pkgsrc
puts third-party config under `/usr/pkg/etc/`; find the
certbot tree instead of assuming it, and let `openssl`
compare dates (`-checkend` takes seconds), since the date
arithmetic differs per OS.

```bash
for cert in $(find /usr/pkg/etc /etc -maxdepth 3 -name cert.pem \
    -path '*letsencrypt/live/*' 2>/dev/null); do
  domain=$(basename "$(dirname "$cert")")
  openssl x509 -enddate -noout -in "$cert" 2>/dev/null
  openssl x509 -checkend 2592000 -noout -in "$cert" >/dev/null 2>&1 \
    && w=ok || w=under-30d
  openssl x509 -checkend 604800 -noout -in "$cert" >/dev/null 2>&1 \
    && c=ok || c=under-7d
  echo "$domain $w $c"
done
```

No certbot files: the listening port, as in the Linux
baseline (`openssl s_client -connect localhost:443`).

- **CRITICAL** if any cert expires in < 7 days
- **WARN** if any cert expires in < 30 days

## Kernel: Running vs Installed

NetBSD has no marker file. `sysupgrade` replaces `/netbsd`;
until the reboot the running kernel is the old one.

```bash
uname -v
ls -l /netbsd
sysctl -n kern.boottime
```

- **INFO** if `/netbsd` is newer than the boot time: a new
  kernel waits for its reboot, which asks and is its own
  step; record it in `todo.md`

## Services: Running Binary vs Installed Package

No `needrestart`. After `pkgin upgrade`, a daemon keeps its
old binary until restarted. `pkg_info -q -B <name>` shows
build information; the install time is the mtime of
`/var/db/pkg/<name>-<version>/+CONTENTS` (check `ls -l`);
`ps -axo pid,lstart,comm` shows the process start.

- **WARN** for each daemon from `memory.md` whose package
  was installed after its process started. A restart asks
  (`rules/service-reload.md`).

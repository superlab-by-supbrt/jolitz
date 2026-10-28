# Fleet Audit Probes

The probe commands run on each audited host. All are
read-only. Group them into a single SSH invocation per host
to minimise round-trips:

```bash
ssh <standard options from CLAUDE.md → SSH Options> USER@HOST '
echo "###ua###"; <ua probe>
echo "###sshd###"; <sshd probe>
echo "###fw###"; <firewall probe>
echo "###mta###"; <mta probe>
echo "###time###"; <time probe>
echo "###reboot###"; <reboot probe>
echo "###certs###"; <certificate probe>
'
```

Then split the output on `###<key>###` markers to fill the
comparison table.

The marker set follows the OS family the pipeline detected
(`rules/os-detection.md`). Linux runs sections 1 to 7.
FreeBSD runs 2, 7 and 8; section 8 fills the `firewall`,
`time` and `reboot` rows with its own commands. macOS runs
2, 7 and 9 the same way. A marker whose section does not
apply emits nothing, and the table cell reads `n/a`.

**Privilege handling.** The sshd and firewall probes need
root. When the SSH user is not root, try `sudo -n` (never an
interactive sudo — BatchMode means no prompts). When neither
root nor passwordless sudo is available, the probe must emit
the sentinel `unknown(needs-root)` instead of a degraded
answer — an active ufw must never be reported as `none` just
because the probe lacked permission to read its state. See
`references/output-format.md` for how the sentinel is
rendered and why it is excluded from drift detection.

## 1. Unattended-upgrades (Debian/Ubuntu)

```bash
apt-config dump 2>/dev/null | grep \
  -e '^APT::Periodic::Update-Package-Lists ' \
  -e '^APT::Periodic::Unattended-Upgrade ' \
  -e '^Unattended-Upgrade::Origins-Pattern::' \
  -e '^Unattended-Upgrade::Mail ' \
  -e '^Unattended-Upgrade::MailReport ' \
  -e '^Unattended-Upgrade::Automatic-Reboot ' \
  -e '^Unattended-Upgrade::Automatic-Reboot-WithUsers ' \
  -e '^Unattended-Upgrade::Automatic-Reboot-Time ' \
  -e '^Unattended-Upgrade::Remove-Unused-Kernel-Packages ' \
  -e '^Unattended-Upgrade::Remove-Unused-Dependencies '
```

Row keys to extract for the table:

- `APT::Periodic::Update-Package-Lists`
- `APT::Periodic::Unattended-Upgrade`
- `Origins-Pattern` count (number of `::` entries)
- `Origins-Pattern` contains `${distro_codename}-security`
  pattern (yes/no)
- `Mail`
- `MailReport` (or legacy `MailOnlyOnError`)
- `Automatic-Reboot`
- `Automatic-Reboot-WithUsers`
- `Automatic-Reboot-Time` (present/absent — absent is the
  preferred fleet policy)
- `Remove-Unused-Kernel-Packages`

## 2. sshd effective config

`sshd -T` needs root (it reads host keys). Probe with the
privilege ladder — direct as root, `sudo -n` otherwise, and
the sentinel when neither works:

```bash
if [ "$(id -u)" = "0" ]; then
  SSHD="sshd"
elif sudo -n true 2>/dev/null; then
  SSHD="sudo -n sshd"
else
  SSHD=""
fi
if [ -z "$SSHD" ]; then
  echo "unknown(needs-root)"
else
  $SSHD -T 2>/dev/null | grep -i \
    -e '^permitrootlogin ' \
    -e '^passwordauthentication ' \
    -e '^pubkeyauthentication ' \
    -e '^kbdinteractiveauthentication ' \
    -e '^challengeresponseauthentication ' \
    -e '^x11forwarding ' \
    -e '^allowtcpforwarding ' \
    -e '^maxauthtries ' \
    -e '^logingracetime ' \
    -e '^usepam ' \
    -e '^port '
fi
```

Row keys: each line is `key value`. Since OpenSSH 10.4
the keys are mixed case (`PermitRootLogin`; every key
from 10.5), so compare them without regard to case.
Compare column-by-column. A host whose sshd column is
`unknown(needs-root)` is reported as such, never as
"defaults".

Highlight as drift:

- Any host with `passwordauthentication yes` while others
  have `no`.
- Any host with `permitrootlogin yes` while others use
  `prohibit-password` or `forced-commands-only`.
- Mismatched `port` values across the fleet.

## 3. Firewall posture

Reading firewall state needs root (`ufw status`,
`firewall-cmd` and `nft list` all refuse for normal users).
Detect the *tools* via `command -v` (no root needed), but
only report their *state* when root or `sudo -n` is
available — otherwise emit the sentinel. Never let a
permission error degrade to `tool=none`: that fabricates
"no firewall" on a host whose firewall is simply unreadable.

Classify the tool in this order, first match wins:

1. `ufw` when `ufw status` says `Status: active`
2. `firewalld` when `firewall-cmd --state` says `running`
3. `nftables` when `nftables.service` is `active`, or an
   input chain in the `--nft` block drops by default
4. `none` — nothing of the above. The `nft` binary alone
   is not a firewall, nor are input chains that fail2ban,
   Docker or kube-proxy add with `policy accept;`.

Default deny for `nftables`, and what `legacy4` and
`legacy6` in the `--legacy` block mean: `jolitz-security` →
`references/firewall-nftables-docker.md`. No count means
no legacy table.

```bash
if [ "$(id -u)" = "0" ]; then
  SUDO=""
elif sudo -n true 2>/dev/null; then
  SUDO="sudo -n"
else
  SUDO="-"
fi
# Prefer ufw on Debian/Ubuntu; firewall-cmd on RHEL family.
TOOLS=""
for t in ufw firewall-cmd nft; do
  command -v "$t" >/dev/null 2>&1 && TOOLS="$TOOLS $t"
done
echo "tools=${TOOLS:- none}"
echo "nftables.service=$(systemctl is-active nftables 2>/dev/null)"
echo "nftables.enabled=$(systemctl is-enabled nftables 2>/dev/null)"
if [ -n "$TOOLS" ] && [ "$SUDO" = "-" ]; then
  echo "state=unknown(needs-root)"
elif [ -n "$TOOLS" ]; then
  for t in $TOOLS; do
    echo "--$t"
    case $t in
      ufw) $SUDO ufw status verbose ;;
      firewall-cmd) $SUDO firewall-cmd --state
                    $SUDO firewall-cmd --list-all ;;
      nft) $SUDO nft list chains \
             | grep -B1 -e ^table -e "hook input" ;;
    esac 2>&1
  done
  echo "--legacy"
  iptables -V 2>/dev/null
  if iptables -V 2>/dev/null | grep -q nf_tables; then
    $SUDO grep -q . /proc/net/ip_tables_names 2>/dev/null &&
      echo "legacy4=$($SUDO iptables-legacy -S | grep -vc ^-P)"
    $SUDO grep -q . /proc/net/ip6_tables_names 2>/dev/null &&
      echo "legacy6=$($SUDO ip6tables-legacy -S | grep -vc ^-P)"
  fi
fi
```

(`$SUDO` is intentionally unquoted so an empty value
disappears; `-` marks "no privilege path".)

Row keys for the table:

- Tool in use (`ufw` / `firewalld` / `nftables` / `none`)
- State — `unknown(needs-root)` when a tool exists but
  its status is unreadable without root
- Legacy iptables rules next to nf_tables (count; > 0 is
  a WARN — `nft` does not show them)
- Default policy (deny incoming required)
- Number of open ports / services
- Whether the SSH port is open (must be yes: 22, or each
  `port` from section 2)
- `nftables.enabled=enabled` next to an active ufw or
  firewalld (WARN: the unit flushes their rules)

Highlight as drift:

- Different firewall tool across the fleet.
- Different default policy.
- Different exposure of admin ports (5432, 27017, 3306,
  9100 to 0.0.0.0).
- Any host with legacy iptables rules while the others
  have none.

## 4. MTA

```bash
# Detect installed MTA package + active unit.
for pkg in postfix sendmail msmtp-mta nullmailer dma \
           opensmtpd exim4; do
  dpkg -l "$pkg" 2>/dev/null \
    | awk -v p=$pkg "/^ii  /{print \"pkg=\" p; exit}"
done
ls -l /usr/sbin/sendmail 2>/dev/null | awk "{print \"sendmail=\" \$NF}"
for unit in postfix opensmtpd exim4; do
  state=$(systemctl is-active "$unit" 2>/dev/null)
  [ "$state" = "active" ] && echo "active=$unit"
done
hostname -f
```

Row keys:

- Installed MTA package
- Sendmail symlink target
- Active SMTP unit
- FQDN (sanity)

Highlight as drift:

- One host with no MTA while others have one.
- Different MTAs in use without a documented reason in
  the per-host `memory.md`.

## 5. Time sync

```bash
timedatectl show \
  --property=NTPSynchronized \
  --property=NTP \
  --property=TimeUSec \
  --property=Timezone 2>/dev/null
systemctl is-active systemd-timesyncd chronyd ntp \
  ntpd openntpd 2>&1 | head -5
```

Row keys:

- `NTPSynchronized` (yes required)
- Active timesync unit name
- Timezone (typically all `Europe/Berlin`)

Highlight as drift:

- `NTPSynchronized=no` on any host.
- Different timesync daemons across the fleet.
- Different timezones.

## 6. Auto-reboot behaviour (cross-check with UA)

```bash
test -f /var/run/reboot-required && echo "pending=yes" \
  || echo "pending=no"
uptime -s
```

Row keys:

- `/var/run/reboot-required` present? (kernel waiting for
  reboot)
- Boot time / uptime

Highlight as drift / warning:

- Any host with `pending=yes` but uptime > 7d — auto-reboot
  has not fired despite a pending kernel.
- Hosts with uptime > 90d — even without a pending reboot,
  worth a heads-up.

## 7. TLS certificates (every OS)

Certificates do not drift between hosts; the row shows how
many days each one has left, and under 30 the host gets a
to-do (`references/output-format.md`). The probe reads the
files certbot keeps — `/etc/letsencrypt/live/` on Linux,
`/usr/local/etc/letsencrypt/live/` on FreeBSD — and, where
there are none, asks the local port 443. The port needs no
root; the `cert.pem` files are usually root-only, so the
privilege ladder of section 2 applies to the loop.

```bash
for cert in /etc/letsencrypt/live/*/cert.pem \
            /usr/local/etc/letsencrypt/live/*/cert.pem; do
  [ -f "$cert" ] || continue
  name=$(basename "$(dirname "$cert")")
  end=$(openssl x509 -enddate -noout -in "$cert" 2>/dev/null \
    | cut -d= -f2)
  echo "cert=$name notAfter=$end"
done
if [ ! -d /etc/letsencrypt/live ] \
   && [ ! -d /usr/local/etc/letsencrypt/live ]; then
  end=$(echo | openssl s_client -connect localhost:443 \
    -servername "$(hostname -f)" 2>/dev/null \
    | openssl x509 -enddate -noout 2>/dev/null | cut -d= -f2)
  [ -n "$end" ] && echo "cert=$(hostname -f) notAfter=$end"
fi
```

Compute the days locally from `notAfter` and the audit date:
`date -d` on Linux and `date -j -f` on the BSDs and macOS
differ, so the host prints the date and the agent does the
arithmetic. `certbot certificates` prints the same lineages
with expiry and renewal state; use it when the to-do needs
that detail, after `certbot --help` on the host. No output
at all means no certificate was found: the row reads `none`,
a finding on a host whose `memory.md` names a web server.

Row keys: `certs.<name>.days`, one row per lineage or FQDN;
the value is the number of days as a string, `expired` when
negative.

Highlight as a to-do (not as drift):

- `days` under 30: WARN. The renewal timer or cron is the
  suspect (`systemctl list-timers --all | grep -i certbot`;
  `certbot renew --dry-run` per `rules/way-back.md`).
- `days` under 7 or `expired`: CRITICAL.

## 8. FreeBSD

`rules/freebsd.md` names every command. `sshd -T` (section
2) works unchanged; `pfctl` needs root, so the ladder of
section 3 applies and the sentinel `unknown(needs-root)`
stands in for the state. No `/etc/os-release`, no
`systemctl`.

```bash
freebsd-version
freebsd-version -k
pkg audit -F 2>&1 | tail -n 1
pkg upgrade -n 2>&1 | tail -n 1
grep -h url /etc/pkg/FreeBSD.conf /usr/local/etc/pkg/repos/*.conf \
  2>/dev/null
sysrc -a 2>/dev/null \
  | grep -E '^(pf|ntpd|chronyd|openntpd|ntpdate)_enable'
pfctl -s info 2>&1 | head -n 1
service ntpd status 2>&1
service chronyd status 2>&1
ntpq -pn 2>/dev/null | grep -c '^\*'
service -e 2>/dev/null | wc -l
```

Row keys:

- `base.userland` — `freebsd-version`; `base.kernel` —
  `freebsd-version -k`. A kernel older than the userland is
  an installed base update waiting for its reboot:
  `reboot.required=yes`, otherwise `no`.
- `pkg.vulnerable` — the count from `pkg audit -F`
  ("N problem(s) in M installed package(s) found").
- `pkg.pending` — the count from `pkg upgrade -n`; `0` for
  "Your packages are up to date".
- `pkg.branch` — `quarterly` or `latest` from the `url`
  line.
- `firewall.tool` — `pf` when `pf_enable: YES`, else
  `none`; `firewall.state` — `enabled` or `disabled` from
  the `Status:` line of `pfctl -s info`, or
  `unknown(needs-root)`.
- `time.service` — the daemon whose `_enable` is `YES`
  (`ntpd`, `chronyd`, `openntpd`); `time.synced` — `yes`
  when `ntpq -pn` shows a `*` peer. chrony's state comes
  from `chronyc tracking`; check `chronyc help` on the host
  before relying on its fields.
- `services.enabled` — the line count of `service -e`, a
  sanity number for the "Changed since" comparison.

Highlight as drift:

- Different `pkg.branch` across the FreeBSD hosts.
- `pkg.vulnerable` above 0 on any host (also a to-do).
- `firewall.tool=none` or `firewall.state=disabled` while
  the others run pf.
- `reboot.required=yes` with an uptime over 7 days: the
  base update was installed and nobody rebooted.

## 9. macOS

`rules/macos.md` names the commands. `sshd -T` from section 2
works; `systemsetup` and `sshd -T` need root, so the ladder
applies (`sudo -n`, the user must be in the admin group). A
Mac behind NAT with the firewall off is INFO, not CRITICAL
(`rules/macos.md`).

```bash
sw_vers -productVersion
softwareupdate -l 2>&1 | grep -c '^\* '
defaults read /Library/Preferences/com.apple.SoftwareUpdate \
  AutomaticCheckEnabled 2>/dev/null
defaults read /Library/Preferences/com.apple.SoftwareUpdate \
  CriticalUpdateInstall 2>/dev/null
/usr/libexec/ApplicationFirewall/socketfilterfw --getglobalstate
/usr/libexec/ApplicationFirewall/socketfilterfw --getstealthmode
systemsetup -getusingnetworktime 2>&1
systemsetup -getremotelogin 2>&1
sntp -t 1 time.apple.com 2>&1 | tail -n 1
```

Row keys:

- `macos.version` — `sw_vers -productVersion`.
- `macos.updates` — the count of `*` lines in
  `softwareupdate -l` (the man page: each available update
  is prefixed `*`, a non-recommended one `-`). `0` when the
  output says no new software is available.
- `macos.auto_check`, `macos.critical_updates` — the two
  `defaults` values, `1` or `0`; a missing key reads
  `(unset)`. `CriticalUpdateInstall` should be `1`.
- `firewall.tool=applicationfirewall`; `firewall.state` —
  `on` or `off` from `--getglobalstate`; `firewall.stealth`
  — `on` or `off`.
- `time.service` — `networktime` when
  `-getusingnetworktime` says `On`, else `off`;
  `time.synced` — `yes` when the `sntp` offset is under 5 s.
- `sshd.remotelogin` — `On` or `Off` from `-getremotelogin`.
- `reboot.required` — `(unset)`: macOS has no marker file.
  `softwareupdate -l` says in an update's line when it
  needs a restart; that goes into the to-do text.

Highlight as drift:

- `macos.critical_updates` not `1` on any Mac.
- `firewall.state` differing between Macs (INFO level).
- `time.service=off` on any host.
- `macos.updates` above 0 in two audits in a row (compare
  with the previous snapshot).

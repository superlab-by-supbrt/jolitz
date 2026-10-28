# Updates — Linux

Dry run first, apply later as a job. Commands follow the
loaded `rules/<family>.md`; a flag not named there is
checked with `--help` on the host before use.

## Dry run

**Debian/Ubuntu** (`rules/debian.md`: `apt-get`, dry-run
with `-s`):

```bash
apt-get update -qq 2>/dev/null
apt-get -s upgrade 2>/dev/null | grep "^Inst"
apt-get -s upgrade 2>/dev/null | grep "^Inst" \
  | grep -ciE "debian-security|[a-z]+-security"
apt-get -s dist-upgrade 2>/dev/null | grep -c "^Remv"
```

The first `grep` is the list, the second the security
subset, the third the packages a `dist-upgrade` would remove
(a count above 0 is a note in the table, not something this
skill runs: `rules/debian.md` prefers `upgrade`).

**RHEL/CentOS/Fedora** (`rules/rhel.md`: `dnf`, `yum` on 7):

```bash
dnf check-update --quiet 2>/dev/null | grep -c "^\S"
dnf check-update --security --quiet 2>/dev/null | grep -c "^\S"
dnf --assumeno update 2>&1 | tail -n 40
```

`dnf check-update` exits 100 when updates exist; that is
not an error.

**SUSE** (`rules/suse.md`: `zypper`, `--dry-run` after the
command, verify with `zypper help update`):

```bash
zypper --quiet list-updates 2>/dev/null | grep -c "^v"
zypper list-patches --category security 2>/dev/null
zypper --non-interactive update --dry-run 2>&1 | tail -n 40
```

## Reboot and restart state

**Debian/Ubuntu:**

```bash
test -f /var/run/reboot-required && echo "reboot-required=yes" \
  || echo "reboot-required=no"
cat /var/run/reboot-required.pkgs 2>/dev/null
OUT=$(LC_ALL=C needrestart -b 2>/dev/null \
  | grep -E 'NEEDRESTART-(KSTA|SVC)' || true)
if [ -z "$OUT" ]; then echo "needrestart=clean"
else echo "$OUT"; fi
```

`NEEDRESTART-KSTA: 1` means the running kernel is current;
`2` or `3` means a newer one is installed. Each
`NEEDRESTART-SVC:` line is a service running an old binary.
No `needrestart` on the host: the kernel comparison below
and the manual check in `jolitz-housekeeping` →
`references/baseline-linux.md` → Critical Services.

**Any Linux**, running kernel against the newest installed:

```bash
uname -r
dpkg -l 'linux-image-*' 2>/dev/null | grep "^ii" \
  | awk '{print $2}' | sed 's/linux-image-//' \
  | sort -V | tail -n 1
rpm -q kernel --qf '%{VERSION}-%{RELEASE}.%{ARCH}\n' \
  2>/dev/null | sort -V | tail -n 1
```

RHEL family: `dnf needs-restarting` (dnf-plugins-core) lists
processes with old binaries and has a reboot-hint flag;
check `dnf needs-restarting --help` on the host. SUSE:
`zypper ps -s` lists processes using deleted files, and
newer releases have `zypper needs-rebooting`; check
`zypper help` on the host.

Table values: `no`, `kernel` (new kernel installed, not
running), `pending (since <date>)` when the file is older
than the last boot, `?` when no probe could tell.

## Apply (after OK): the job

One job per host, the command in a file, output and status
beside it (`rules/ssh-connections.md` → 4). The call starts
it and returns; the upgrade outlives the connection. Nothing
else goes into `cmd`: no restart, no reboot, no cleanup.

```sh
ssh <standard options> USER@HOST 'sh -s' <<'EOS'
find ~/.jolitz/jobs -mindepth 1 -maxdepth 1 -mtime +7 \
  -exec rm -r {} + 2>/dev/null
d=~/.jolitz/jobs/$(date +%Y%m%d-%H%M%S)-upgrade
mkdir -p "$d"
cat >"$d/cmd" <<'CMD'
apt-get -o DPkg::Lock::Timeout=300 -y upgrade
CMD
(nohup sh -c 'sh "$1/cmd" >"$1/out" 2>"$1/err" &
  echo $! >"$1/pid"; trap "" TERM
  wait $!; echo $? >"$1/status"' sh "$d" \
  </dev/null >/dev/null 2>&1 &)
ps -o pgid= -p $$ | tr -d " " >"$d/pgid"
echo "$d"
EOS
```

The `CMD` line per family:

- Debian/Ubuntu: `apt-get -o DPkg::Lock::Timeout=300 -y
  upgrade` (`dist-upgrade` only when the user asked for it
  after seeing the `Remv` count).
- RHEL family: `dnf -y update` (`yum -y update` on 7).
- SUSE: `zypper --non-interactive update`, or
  `zypper --non-interactive patch --category security` for
  the security subset only (`rules/suse.md`: verify the
  syntax with `zypper help patch` first).

Record `$d` in `memory/servers/<hostname>/todo.md` as soon
as the call returns.

## Read the job back

One call that waits on the host, with the tool's timeout set
to cover it:

```sh
ssh <standard options> USER@HOST 'sh -s' <<'EOS'
d=~/.jolitz/jobs/JOBDIR
i=0
while [ ! -f "$d/status" ] && [ $i -lt 10 ]; do
  sleep 30; i=$((i+1))
done
cat "$d/status" 2>/dev/null || echo running
tail -n 40 "$d/out" "$d/err"
EOS
```

`status` 0: upgraded. Any other value: read `err` in full
before anything else runs; a lock timeout or a failed
maintainer script is reported, never retried blind. Then
the reboot and restart state again, and the changelog. Delete
the job directory once the result is in the changelog.

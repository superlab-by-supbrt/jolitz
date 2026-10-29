# AIX

Rules for IBM AIX 7.2 and 7.3 on Power. Written from
IBM Documentation (ibm.com/docs/en/aix/7.3.0) and IBM
support pages, not yet confirmed on a host: where a
line says "check on the host", look before relying on
it, and record what you found in the server's
`memory.md`.

## Version Detection

- `uname -s` prints `AIX`. `uname -M` gives the
  model, `uname -L` the LPAR.
- `oslevel -s` prints base, technology level, service
  pack and build, e.g. `7300-02-03-2420`; `oslevel -r`
  the base and TL only. Record the `-s` form in
  memory: `- OS: AIX 7.3 TL2 SP3 (7300-02-03-2420)`.
  The fleet overview reads the TL from it.
- Hardware: `prtconf` (CPU, memory with `-m`, LPAR
  with `-L`), `lsattr -E -l sys0`. `bootinfo` is
  deprecated; IBM points to `getconf` with
  `BOOT_DEVICE`, `REAL_MEMORY`, `KERNEL_BITMODE` or
  `DISK_SIZE /dev/hdiskN`.
- Support ends per technology level, not per
  release: AIX 7.3 TL2 on 2026-11-30, TL3 on
  2027-12-31 (IBM AIX support lifecycle page). Name
  the TL when you report end of life.

## Shell and Tools

- root's shell is `/usr/bin/ksh` (ksh88). Do not
  change the shell of root or another system
  account; IBM warns against it. `bash` is installed
  by default on 7.3; on 7.2 check on the host.
- `command` is a ksh built-in: `command cp`,
  `command mv`, `command rm` work as on Linux.
- The base tools are not GNU:
  - `sed` has no `-i` and no `-E`. Write to a new
    file and move it into place after the backup.
  - `grep -E`, `-F`, `-q` and `-r` exist.
  - `df` counts 512-byte blocks: use `df -g` or
    `df -m`. `-h` may be missing; check on the host.
  - There is no `stat`. Use `istat <file>` or
    `ls -l`.
- GNU versions come with the AIX Toolbox under
  `/opt/freeware/bin` and are only used when the
  host has them.

## Package Manager

Two worlds: AIX filesets (`installp`) for the
operating system, RPMs from the AIX Toolbox for open
source software.

- Installed filesets: `lslpp -L` (all), `lslpp -l
  <fileset>`, `lslpp -h` (history). Read-only.
- `installp` applies by default and leaves updates
  uncommitted; `-p` previews. Every install starts
  with `-p`:
  ```
  installp -p -agXY -d <dir> <fileset>
  ```
  `-c` commits, `-r` rejects an applied update, `-u`
  removes. Commit only after the system has run on
  the update; an uncommitted update can be rejected,
  a committed one cannot. That is the way back.
- Interim fixes: `emgr -l` lists, `emgr -p -e <pkg>`
  previews, `emgr -e <pkg>` installs, `emgr -r -L
  <label>` removes.
- Toolbox software: on 7.3 only `dnf` (yum is not
  supported there), binary `/opt/freeware/bin/dnf`,
  config `/opt/freeware/etc/dnf/dnf.conf`. If it is
  missing, IBM's bootstrap script installs it; that
  is an install like any other and asks first:
  ```
  https://public.dhe.ibm.com/aix/freeSoftware/aixtoolbox/ezinstall/ppc/dnf_aixtoolbox.sh
  ```
  After a migration from 7.2, the script has to run
  again for a 64-bit dnf.

## Automatic Security Updates

AIX has no unattended-upgrades equivalent that
installs on its own. `suma` downloads fixes:

```
suma -x -a RqType=Latest -a Action=Preview
```

`Action=Preview` downloads nothing; `Download`
fetches into the download directory; `Clean` also
deletes filesets. `suma -l` lists saved tasks. For
the fleet: report the TL, the SP and whether a
`suma` task exists. Flag a TL past its end of fix
support. Installing a TL or SP is a planned change
with its own window: preview with
`install_all_updates -p -d <dir>`, apply without
`-c`, commit later.

## Service Manager

AIX runs daemons under the System Resource
Controller (SRC).

- `lssrc -a` lists all subsystems, `lssrc -g <group>`
  a group, `lssrc -s <name>` one.
- `startsrc -s <name>`, `stopsrc -s <name>`.
- `refresh -s <name>` makes a subsystem reread its
  config, only where the subsystem supports it
  (`refresh -s syslogd` is documented). IBM keeps no
  list: when refresh fails, a stop and start is a
  restart and asks first (`rules/service-reload.md`).
- sshd: `lssrc -g ssh`; config in
  `/etc/ssh/sshd_config`, binary `/usr/sbin/sshd`,
  fileset `openssh.base`. Check the config with
  `sshd -t` and the effective one with `sshd -T`.
- Never stop `sshd`, the group `ssh`, the group
  `tcpip` or all subsystems (`stopsrc -a`). The
  guard denies it.

## Logs

- The error log is the system's own log: `errpt`
  (summary), `errpt -a` (detail), `errpt -s
  <mmddhhmmyy>` for a window, `errpt -d H` for
  hardware. Read it on every health check.
- `errclear` deletes error log entries. Never run it
  without the user asking for it; the entries are the
  evidence.
- syslog may log nothing by default. Read
  `/etc/syslog.conf` before you rely on a log file.
  Rotation is off unless configured there.
- `logger -t jolitz "…"` exists, but the line only
  lands somewhere when a `syslog.conf` entry
  matches. When none does, the journal line of
  `rules/changelog.md` is lost: write the local
  changelog, tell the user once, and offer a
  `syslog.conf` entry as a change of its own
  (backup, `refresh -s syslogd`).

## Firewall

AIX filters with the IP security filter, not with a
firewall daemon. IPFilter was removed in 7.3.

- Read: `lsdev -Cc ipsec` (is the filter device
  there), `lsfilt -v 4 -a` (active rules), `lsfilt
  -v 6 -a`.
- Rules: `genfilt` adds a rule to the table, `mkfilt
  -u` activates the table, `mkfilt -d` deactivates
  it. Without `-v`, `mkfilt` acts on IPv4 and IPv6.
- Lockout: `mkfilt -z D` makes the default rule deny.
  Activated with a table that has no permit for the
  SSH port, it cuts the session. Every filter change
  gets a rollback timer (`rules/rollback-timer.md`)
  that runs `mkfilt -d`, and the permit for the SSH
  port goes in before any deny.
- A host without the filter device has no packet
  filter. Report it as missing (CLAUDE.md, Expected
  Software); enabling it is a change that asks first.

## Disks and LVM

- Read: `lspv`, `lsvg`, `lsvg -l <vg>`, `lslv <lv>`,
  `lsfs`, `getconf DISK_SIZE /dev/hdiskN`. Disks are
  `/dev/hdiskN`, their raw device `/dev/rhdiskN`.
- `rootvg` holds the system; `hd5` is the boot
  logical volume, `hd4` is `/`.
- Destroy data, denied by the guard: `mkvg -f`,
  `extendvg -f`, `rmlv`, `reducevg -d`, `rmfs`,
  `chpv -c` (boot record), `chpv -C`, `dd` onto a
  disk device.
- `mkvg` and `extendvg` without `-f` refuse a disk
  that belongs to a volume group; they still ask
  first. `crfs` creates a new logical volume and file
  system. `chfs -a size=+<n>` grows a file system; a
  negative size shrinks JFS2, and `ea=v2`,
  `lff=yes`, `vix=yes` and `efs=yes` cannot be
  undone. Name that in the question.
- `exportvg` removes a volume group's definition, not
  its data; the group must be varied off.

## Boot

- Read: `bootlist -m normal -o`, `bootlist -m both
  -o`, `getconf BOOT_DEVICE`.
- The boot list, the boot image (`bosboot -a`), the
  boot record (`mkboot`) and rootvg clones
  (`alt_disk_copy`, `alt_disk_install`) are boot
  work and need the operator's guard override
  (`rules/efi-boot.md` for the general approach).
  `bosboot -v` only verifies.
- `alt_disk_copy` is AIX's own way back before an
  update: a clone of rootvg on a second disk, booted
  only if the update fails. Offer it before a TL
  update when a free disk exists.

## Shutdown

- `shutdown` without flags halts and, where the power
  supply allows, powers off. `-h` and `-v` halt,
  `-p` halts without power-down, `-t` restarts later
  only after shutting down now. All denied.
- The reboot is `shutdown -Fr` (`-F` skips the
  one-minute wait). `reboot -n` and `-q` skip the
  sync; never use them.

## Accounts

- `lsuser -a <attr> <name>` reads, `mkuser` creates
  (the account stays locked until `passwd` or
  `pwdadm` sets a password), `chuser` changes.
- Hashes live in `/etc/security/passwd`, attributes
  in `/etc/security/user`, `group` and `limits`.
  Never edit them directly; the guard denies it.
- `rmuser` and `rmgroup` are denied like `userdel`.
- sudo comes from the Toolbox when present. Where
  its sudoers file lives, check on the host
  (`sudo -V` as root names it). Writing it needs
  the operator's guard override, as on Linux.

## Time

AIX 7.3 ships NTPv4 (`ntp.rte`); `xntpd` is the
subsystem: `lssrc -s xntpd`, config `/etc/ntp.conf`.
There is no chronyd.

## Common Pitfalls

- Fixes come per technology level and service pack;
  a host on an old TL gets no fixes at all.
- `installp` without `-c` leaves updates applied but
  uncommitted. `installp -s` lists them; report them
  rather than committing on the side.
- Linux habits fail quietly: `sed -i`, `df -h`,
  `stat`, `systemctl`, `journalctl` do not exist.

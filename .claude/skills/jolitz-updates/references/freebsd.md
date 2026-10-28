# Updates — FreeBSD and NetBSD

Two layers on FreeBSD (`rules/freebsd.md`): `freebsd-update`
patches the base system, `pkg` the third-party packages.
Both are listed, both are applied separately. NetBSD at the
end.

## Dry run (FreeBSD)

```bash
freebsd-version
freebsd-version -k
grep -h url /etc/pkg/FreeBSD.conf /usr/local/etc/pkg/repos/*.conf \
  2>/dev/null
pkg update 2>&1 | tail -n 2
pkg upgrade -n 2>&1 | tail -n 40
pkg audit -F 2>&1 | tail -n 20
freebsd-update --not-running-from-cron fetch 2>&1 | tail -n 40
```

- `pkg upgrade -n` lists the packages and ends with the
  count; "Your packages are up to date." means none.
- `pkg audit -F` names packages with known
  vulnerabilities: the security subset of the table.
- The `url` line shows the branch (`quarterly` or `latest`);
  the host keeps the one it has (`rules/freebsd.md`).
- `freebsd-update fetch` downloads the base patches and
  lists the files `install` would replace. It installs
  nothing, but it writes under `/var/db/freebsd-update/`:
  say so when the host is read-only, and skip it there.

## Reboot state (FreeBSD)

`freebsd-version -k` is the running kernel,
`freebsd-version` the installed userland. A kernel older
than the userland means a base update was installed and
waits for the reboot: table value `kernel`. The `fetch`
output naming files under `/boot/kernel/` means the next
`install` will stage one.

## Apply (after OK): FreeBSD

Before the first job on a ZFS root, a boot environment
(`rules/freebsd.md` → Boot environments); it is the way
back for the base update:

```bash
bectl create "pre-update-$(date +%Y%m%d)"
bectl list
```

Then the job from `references/linux.md`, with one of these
as the `CMD` line, one job each, packages first:

- `pkg upgrade -y`
- `freebsd-update --not-running-from-cron fetch install`

`rules/freebsd.md`: `freebsd-update install` can stage a
kernel that needs a reboot, and `pkg upgrade -y` upgrades
every package, not only the vulnerable ones. Say both in
the question. After `pkg upgrade`, the daemons listed in
`out` run old binaries until restarted (no `needrestart`
here); each `service <name> restart` asks
(`rules/service-reload.md`). Base-system updates restore
`/usr/sbin/sshd`: on a host with the OpenSSL wrapper from
`rules/freebsd.md`, re-check it after the job.

## NetBSD

Every remote command starts with
`export PATH=/sbin:/usr/sbin:/usr/pkg/sbin:$PATH;`
(`rules/netbsd.md`). Dry run:

```bash
export PATH=/sbin:/usr/sbin:/usr/pkg/sbin:$PATH
uname -r; head -n 1 /etc/release
pkgin update 2>&1 | tail -n 2
pkgin -n upgrade 2>&1 | tail -n 40
pkg_admin fetch-pkg-vulnerabilities 2>&1 | tail -n 2
pkg_admin audit 2>&1 | tail -n 20
```

No `pkgin` on the host: `pkg_info` lists what is installed;
installing `pkgin` is ordinary work that asks first
(`rules/netbsd.md`). Base updates to a new point release go
through `sysupgrade`, replace the kernel and need a reboot:
reported as "due", never run by this skill.

Apply: the job from `references/linux.md` with
`export PATH=/sbin:/usr/sbin:/usr/pkg/sbin:$PATH;
pkgin -y upgrade` as the `CMD` line (`pkgin -y` is the
non-interactive form named in `rules/netbsd.md` for
`install`; check `pkgin -h` for `upgrade` on the host
first). `nohup` exists in base; the job pattern works
unchanged.

# The Way Back

Before heinzel changes anything, the way back is
decided. It must not depend on what the change can
break.

A backup on a host that no longer boots is not a
way back. Neither is an SSH login the new firewall
blocks, nor the old package version the upgrade
just removed. This rule applies to every change,
including kinds no other rule names.

## Four questions before every change

1. **What is the worst outcome?** A service down,
   the login gone, data gone, the host not booting.
2. **How would I repair it?** Over SSH, from a
   backup, with the old kernel, the old binary, the
   old config.
3. **Can the change hit that repair path?** Then
   the way back must work without it and trigger by
   itself.
4. **When does a failure show?** Now, or only at
   the next reboot, cron run, renewal or rotation.
   If later, run the test now or keep the way back
   ready until then.

## Three techniques, in order of preference

**A. On probation first, permanent after proof.**
Make the change so that it lasts only until the
next reload or reboot. Make it permanent once it is
verified from outside. A mistake then undoes itself.

- `sysctl -w` before a file in `sysctl.d/`;
- `mount` before the `fstab` line;
- `ip addr add` before the netplan file;
- firewalld without `--permanent`;
- a new service on a second port before the proxy
  switches to it.

Making it permanent is a step of its own, with its
own proof: read the stored setting back, then
reload or reboot once without the probation and
check the result. Do it before anything removes
the old state. Until then a plain reload or reboot
returns to the old state, so removing it first
leaves the default pointing at nothing.

**B. A dead man's switch** when the failure cuts
off the access needed to repair it: a timer on the
host restores the old state unless a fresh login
stops it. Recipe: `rules/rollback-timer.md`.

**C. Keep the old until the new is proven.**

- the previous kernel stays installed until the
  new one has booted once;
- the old PostgreSQL cluster stays until the new
  one serves the application;
- the old certificate, binary and config stay in
  `/var/backups/heinzel/`;
- DNS TTLs drop a day before a move;
- before deleting, a copy whose readability is
  proven (`rules/backups.md`);
- the old system stays until the new one is the
  permanent default (technique A) and has come up
  that way once.

## No way back

Some changes have none: `DROP DATABASE`, an upgrade
that rewrites the data format, deleting the only
copy. Say so before asking, and recommend a
snapshot where one exists: ZFS, btrfs and LVM
heinzel can take itself; a VM snapshot at the
provider the user takes.

## Make it visible

- Every "may I?" question ends with one line:
  `Way back: <how>` or `Way back: none`, in the
  user's language. A change that goes ahead without
  asking (a reload under `rules/service-reload.md`)
  needs no line, but still has its way back.
- Write the `Rollback:` line of the local changelog
  (`rules/changelog.md`) **before** the change, not
  after it.
- Test the way back where it costs nothing: a
  config test of the restored file, `--help` for
  the restore command, the backup's readability.

## Failures that show later

- `fstab`: a typo hangs the next boot in emergency
  mode. Run `findmnt --verify` and `mount -a` before
  the reboot; data disks get `nofail`.
- A unit enabled with `enable`: run
  `systemd-analyze verify` and a `start` before the
  next boot needs it.
- A cron job or timer: run its command once by hand
  as its user.
- A changed renewal or rotation config: run its dry
  run (`certbot renew --dry-run`,
  `logrotate -d`).
- A one-shot boot (`BootNext`, `grub-reboot`,
  `bootctl set-oneshot`): the boot after it takes
  the old default again. Set the default, read it
  back and reboot once without the one-shot before
  relying on it.

Boot loaders, partition tables and EFI entries are
Heinzel Pro's work (`CLAUDE.md` → What Heinzel does
not do); the same questions apply there.

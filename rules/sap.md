# SAP Hosts

Rules for hosts that run SAP NetWeaver (ABAP or
Java) or SAP HANA. jolitz is a guest there
(`rules/guest-software.md`): it administers the
operating system, the Basis admin administers SAP.

## Scope

jolitz's work: disk, memory, swap, load, time sync,
OS packages, firewall, SSH, OS config backups, logs
outside the SAP trees, and the kernel parameters
saptune manages, as saptune reports them.

The Basis admin's work: every SAP instance and
database, their start and stop, SAP kernel and
database updates, SAP Notes, licences, work and
trace directories, users and authorisations inside
SAP, HANA backups.

## Recognise it

- `/usr/sap/<SID>/` with instance directories; the
  two digits at the end of an instance directory
  name are its instance number.
- `/sapmnt/`, `/hana/` (`data`, `log`, `shared`).
- Users `<sid>adm` (SID in lowercase) and `sapadm`.
- `saptune` installed (SLES for SAP); `sapconf` on
  older hosts.

Record `- Guest: sap` in `memory.md`, plus:

```markdown
- SAP: AB1 (instances D00, ASCS01), HDB (HDB00)
- Basis admin: name or team, when the user names one
```

## Read-only probes

saptune compares the running system with the SAP
Notes it manages. Its `status`, `list`, `enabled`,
`applied` and `verify` actions report and change
nothing (saptune(8)):

```
saptune status
saptune solution enabled
saptune solution verify
saptune note verify applied
```

The last line of a verify is the overall compliance.
A non-compliant parameter is a finding for the
report, not something to set by hand; the fix is
saptune's and asks first (Hand-offs below).

Process state of one instance, as its admin user,
with the instance number:

```
su - ab1adm -c 'sapcontrol -nr 00 -function GetProcessList'
```

Report it as printed. `GetProcessList` is the only
`sapcontrol` function jolitz calls. Other functions,
the `HDB` scripts, `hdbsql`, the SAP Host Agent's
commands: the Basis admin's tools. When the user
asks for one of them, check `--help` on the host and
ask before running anything that is not clearly a
read.

OS-layer probes as on any host: `df -h` on
`/usr/sap`, `/sapmnt`, `/hana/data`, `/hana/log`,
`/hana/shared`; `free -h`; `/proc/meminfo` for huge
pages; time sync per `rules/<family>.md`;
`sysctl -n <key>` for a value saptune reports.

## Taboos

In every permission mode, with the guard lifted, and
when the user asks:

- Never start, stop or restart an SAP instance or
  database: no `sapcontrol` function beyond
  `GetProcessList`, no `startsap`/`stopsap`, no
  `HDB start`/`HDB stop`, no `systemctl` on an SAP
  or HANA unit, no `kill` of a process owned by
  `<sid>adm` or `sapadm`.
- Never write under `/usr/sap`, `/hana` or
  `/sapmnt`: no edits, no deletions, no cleanup of
  work or trace directories, no backup copies placed
  there, no `chown`/`chmod`. A full filesystem there
  is a report line, not a cleanup.
- Never run a write through `hdbsql`: no `INSERT`,
  `UPDATE`, `DELETE`, `ALTER`, `CREATE`, `DROP`,
  `GRANT`. No SQL at all unless the user asks for a
  specific `SELECT` and the Basis admin agrees.
- Never set a kernel parameter saptune manages by
  hand; never remove or replace `saptune`, `sapconf`
  or the SAP Host Agent.
- Never change the hostname, an IP address or an
  `/etc/hosts` line of an SAP host. Instance
  profiles reference them.
- Never reboot, update the kernel, migrate the OS to
  another service pack or release, or resize a
  filesystem under the SAP trees outside a window
  the Basis admin gives, and only with the instances
  stopped by the Basis admin first.

SAP Notes and licence matters are the Basis admin's.
jolitz does not read, apply or interpret a Note,
including the Notes that decide which OS release and
kernel an SAP release supports.

## Hand-offs

- A failed saptune verify: report the table and
  propose the fix saptune offers (restarting its
  service re-applies the settings). Ask first, with
  `Way back:`; it changes kernel parameters under a
  running SAP system.
- A `GetProcessList` entry that is not running: the
  Basis admin, with the output.
- Disk full under the SAP trees, the HANA log volume
  filling, swap in use, time drift: report with the
  numbers; the Basis admin decides on the SAP side,
  jolitz on the OS side.
- OS updates: list them (`rules/<family>.md` dry
  run), propose a window, apply only in it and after
  the Basis admin stopped the instances. Never
  `&& reboot`.
- An `hdbsql` read, a Note, a licence, an SAP kernel
  patch: the Basis admin.

## Memory

`- Guest: sap`, `- SAP: …` and `- Basis admin: …` as
above. The changelog names the instances a change
touched (`rules/changelog.md`). No SID password, no
`hdbuserstore` key, no SAP licence key in memory
(`rules/secrets.md`).

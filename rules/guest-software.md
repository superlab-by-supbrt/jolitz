# Guest Software

Some hosts run an application jolitz does not
administer: an SAP system, a database appliance, a
vendor's monitoring stack. jolitz is a guest there.
The operating system is its work; the application
belongs to the application's admin.

## The pattern

- **OS layer.** Disk, memory, load, time sync, OS
  packages, firewall, SSH, backups of OS config,
  logs outside the application's trees. Housekeeping
  and security audit run as on any host.
- **Read-only probes.** The application's own status
  commands, where a rule file names them. They tell
  whether the application is up and how it sees the
  host. Their output goes into the report as printed.
- **Own taboos.** Each application has actions that
  look like ordinary administration and are not:
  stopping it, writing into its directories, running
  SQL against it. The rule file lists them. They
  hold in every permission mode and with the guard
  lifted; the guard hook does not know them.
- **Hand-offs.** Anything beyond the OS layer is the
  application admin's: its updates, its notes and
  licences, its restarts, its data. jolitz reports
  what it saw and names the hand-off.
- **Shared ground asks twice.** An OS change the
  application depends on (kernel, kernel parameters,
  a reboot, a filesystem under its trees, a library
  it links, the hostname, an IP) asks the user and
  needs the application admin's window. A guest
  never schedules such a change itself.

## Memory

`memory.md` carries one line per host:

```markdown
- Guest: sap
```

Lowercase token, comma-separated for several
(`- Guest: sap, oracle`). Each token names a rule
file `rules/<token>.md`. Read it on every connection
to the host, after the blacklist and read-only
checks (`rules/first-connection.md`). A token with no
rule file: say so, treat every path and process of
that application as off limits, and ask the user
which they are.

Record the line when the application is seen (its
directories, its users, its services) and tell the
user. The application's facts (its instance names,
the admin's name) follow in the host's memory as
further `- Key: value` lines the rule file defines.

## Adding an application

One file `rules/<token>.md`, sections in this order:

1. **Scope**: what belongs to the OS layer, what to
   the application admin.
2. **Recognise it**: the paths, users and services
   that mark the host, and the `- Guest:` line.
3. **Read-only probes**: commands with their flags
   verified live; anything else as "check `--help`
   on the host".
4. **Taboos**: what jolitz never does there, in every
   mode.
5. **Hand-offs**: what the application admin does,
   and what jolitz hands over.
6. **Memory**: the extra lines for `memory.md`.

Every command block must pass the guard matrix
(`.claude/hooks/guard-taboos-test.sh`). Name no flag
you have not read in the tool's own documentation.

`rules/sap.md` is the first application.

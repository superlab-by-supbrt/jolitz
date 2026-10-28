# Your data, your rules, the logs

Back to the [README](../README.md).

## Your data: `memory/`

Plain text, gitignored:

- `user.md`: SSH users, language, operator name
- `blacklist.md`, `readonly.md`, `protected-paths.md`
- `service-policy.md`: which services may reload or restart
  without asking
- `servers/<host>/`: memory, changelog, to-do, rule overrides
- `custom-rules/`: your global overrides
- `network.md`, `housekeeping.md`: cross-server facts, your
  own checks
- `opencode.json`: your OpenCode config
- `audits/`: one snapshot per fleet audit
- `overview/`, `eol/`: pages and cached end-of-life dates
  from `bin/jolitz-overview`; safe to delete

```bash
bin/jolitz-backup                        # writes a .tar.gz
bin/jolitz-backup -o <path>              # to a given path
bin/jolitz-backup --list                 # dry run
bin/jolitz-backup --restore <file.tar.gz>
```

Restore validates the archive and refuses when `memory/`
holds user data; `--force` copies the archive over it and
removes nothing that is not in the archive.

**What leaves your machine.** Every command output jolitz
reads goes to the AI provider behind your terminal tool, as
part of the conversation: logs, configs, user lists, the
addresses in them. `--eol` sends product names such as
`debian` to endoflife.date, nothing else. Mails go where you
send them, with the report inside. Nothing else is sent
anywhere.

**What stays, and for how long.** `memory/` holds operator
names (also in every `journalctl -t jolitz` line on every
server), user names, IPs and host names. The changelog and
the audit snapshots keep two years, config backups on the
servers 30 days; the guard log, the cron log and
`memory/refusals.log` are never trimmed by jolitz. If you
administer servers for an employer or a client, their rules
for personal data and for sending data to a processor apply
to all of this.

**Team use.** Everyone keeps a personal `memory/user.md`.
To share server state, edit `.gitignore` as its comments say,
keep each member's own machine out (e.g.
`memory/servers/alices-mbp/`) and commit server memory after
sessions. `user.md`, `blacklist.md`, `readonly.md` and
`opencode.json` are never shared; back them up. Shared
memory is read by jolitz as settings: `rules.md` overrides,
mail recipients, send and install policies. Review it like
code before you pull it.

## Your own rules

Three layers, later wins:

1. `rules/<name>.md`
2. `memory/custom-rules/<name>.md` (all servers)
3. `memory/servers/<host>/rules.md` (one server)

```markdown
## Add: Docker cleanup
New rules next to the base.

## Replace: Firewall
Replaces the matching base section.

## Remove: Common Pitfalls > snap
Drops that base section.
```

A section without a prefix is an addition.
`memory/custom-rules/all.md` applies everywhere. Skills
follow the same chain, e.g.
`memory/custom-rules/jolitz-housekeeping.md`. The ethics rule
takes no override. The guard's taboos, the connection
pipeline, anomaly detection, secrets, the way back and the
rollback timer take `Add:` only.

## Logs

```bash
journalctl -t jolitz --since "2026-02-01"

# macOS
log show \
  --predicate 'senderImagePath CONTAINS "logger"' \
  --info --last 7d | grep jolitz
```

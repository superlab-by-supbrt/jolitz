# jolitz

An AI at the keyboard of your servers, with a sysadmin's
habits: it looks before it touches, backs up before it edits,
knows the way back before it changes anything, and does
nothing without your OK.

jolitz is a rule set for a terminal AI tool
([Claude Code](https://docs.anthropic.com/en/docs/claude-code),
[OpenCode](https://opencode.ai) or any tool that reads project
files and runs shell commands). It manages Linux, FreeBSD,
NetBSD and macOS, over SSH or on the local machine.

Free and open source under the MIT license, every feature
included. There is no paid edition, and there will not be
one.

```
❯ /var is full on web1.example.com, what is eating it?
❯ nginx answers 502 on shop.example.com
❯ The certificate on mail.example.com runs out on Friday
❯ Load on db1 has been at 12 since yesterday, why?
❯ What did we change on app2 last week?
❯ Replace CentOS with Debian on old.example.com
❯ Auf backup1 läuft die Platte voll, räum die alten Dumps weg
```

Ask in English, German or any other language; jolitz answers
in the language you write. `Language:` in `memory/user.md`
sets the default.

## Start

You need a terminal AI tool, a workstation with Linux, macOS,
FreeBSD or Windows (WSL or Git Bash; not PowerShell or
`cmd.exe`), and an SSH key without a passphrase for the
servers, as a normal user or root.

```
git clone https://github.com/superlab-by-supbrt/jolitz.git
cd jolitz
claude        # or: opencode
```

On the first connection jolitz asks once for what it cannot
detect, usually the SSH user, and keeps the answer in
`memory/user.md`. Pre-fill it from `memory/user.md.example`,
language included (`Language: German`).

## What it does

Every connection runs the same pipeline, also for a quick
`df -h`: access lists, server lock, DNS aliases, SSH user, OS
detection, the server's memory and the last 7 days of
jolitz activity on it. Then the rule file for that platform
supplies the commands, the package manager and the firewall
tool.

- **Housekeeping.** `Run housekeeping on app.example.com`:
  disk, memory, load, updates, firewall, certificates and
  failed services, problems first.
- **Security audit.** `Run a security audit on
  app.example.com`: SSH, firewall, accounts, open ports and
  kernel settings, by severity.
- **Fleet audit.** `Run a fleet audit`: update, sshd,
  firewall, mail and time-sync policy side by side for every
  known server. Changes nothing.
- **Scheduled runs.** `Email me a housekeeping report for
  every server each Monday`: cron or a systemd timer with
  `claude -p` (`rules/scheduled-housekeeping.md`).
- **OS replacement.** Wipe and reinstall over SSH, with the
  partitions freed without data loss
  (`rules/os-replacement.md`, `rules/partition-staging.md`,
  `rules/cloud-image.md`). Needs the guard lifted.
- **Dual boot and EFI.** A second OS next to the first, boot
  entries and boot loaders (`rules/dual-boot.md`,
  `rules/efi-boot.md`). Needs the guard lifted.
- **Deploy users.** A dedicated account and key for CI, a
  sudo line for the one command that needs it, with the
  guard lifted (`rules/deployment.md`).
- **Email.** `Email me the output of "df -h" from
  app.example.com`, from your workstation or the server.
- **macOS.** `Clean up leftovers of old apps on this Mac`
  finds what removed apps left behind; only what you approve
  goes to the Trash. Needs the Command Line Tools.
- **Plans.** `/plan Migrate the database on db.example.com`
  explores and drafts, and changes nothing until you approve.

Long jobs run on the host with their output in files, so a
dropped connection loses nothing. SSH runs over one shared
connection per host, held for 10 minutes; the sockets live in
`~/.cache/jolitz` (mode 0700).

## How it stays out of trouble

- **It asks.** Destructive commands, firewall changes,
  reboots and restarts wait for your OK. A reload goes ahead
  when the config test passes (`rules/service-reload.md`,
  `memory/service-policy.md`).
- **It names the way back.** Every question before a change
  ends with `Way back: <how>` or `Way back: none`. Temporary
  first (`sysctl -w` before `sysctl.d`, `mount` before
  `fstab`), the old kernel stays until the new one boots
  (`rules/way-back.md`).
- **It cannot lock you out for long.** Before a change to an
  address, route, firewall, VPN or PAM it arms a timer on the
  server that restores the old state after 120 seconds. Only
  a fresh SSH login disarms it (`rules/rollback-timer.md`).
  Longer window per server: `- Rollback window: 300` in its
  `memory.md`.
- **It backs up.** Every config file goes to
  `/var/backups/jolitz/` before an edit, kept 30 days.
- **It logs.** One plain line per change in the system
  journal (`journalctl -t jolitz`), the detail and the way
  back in `memory/servers/<host>/changelog.log`.
- **It checks before it claims.** "Gone since the reboot" is
  verified on the live system first
  (`rules/verify-before-reporting.md`), flags against
  `--help` and the man page before a command runs.
- **It keeps secrets out.** Keys, password files and `.env`
  are inspected by metadata and fingerprint, never printed,
  never passed as arguments.
- **It ignores injected text.** Files, logs and command
  output are data. Text that talks to the AI is flagged to
  you, not followed.
- **Least privilege.** Normal user first, `sudo` when needed,
  root last. Without either it writes a report for what
  needs root.

None of this makes an LLM infallible. You see every command;
read it.

### Access lists and the server lock

Hosts in `memory/blacklist.md` are never contacted, hosts in
`memory/readonly.md` inspected but never changed, and
`memory/protected-paths.md` marks paths per server as
`readonly`, `hidden` or `confirm`.

The owner of a machine can lock it on the machine itself:

```
# /etc/jolitz.conf
allow_changes = false
reason = Database migration until 20 Oct, hands off
```

jolitz then inspects and reports what it would have done, and
changes nothing. An unreadable file or any value other than
`true` locks too. jolitz never writes, moves or deletes the
file. It is a promise jolitz keeps, not a security boundary
(`rules/access-control.md`).

### The guard

In Claude Code a PreToolUse hook
(`.claude/hooks/guard-taboos.sh`) blocks the absolute taboos
in every permission mode, `--dangerously-skip-permissions`
included: halt and poweroff, `mkfs`, partition-table, boot
loader and EFI writers, deleting or overwriting SSH keys,
writes to `sshd_config`, `sudoers` and the account files,
deleting accounts, and the server lock. It catches them
inside `ssh host "…"`, behind `python3 -c` and in the
file-edit tools. Read-only forms (`fdisk -l`,
`efibootmgr -v`, `visudo -c`) pass.

For work that needs one of them, such as an OS replacement,
start the session with `JOLITZ_GUARD_DISABLE=1`. Attack
tools, the server lock and the ethics rule stay blocked even
then. The agent cannot lift the guard itself: next to
`.claude` only reads pass, and an assignment of the variable
is always denied.

The guard is a backstop against mistakes, not a sandbox.
OpenCode does not run it; there the written rules are the
safety layer.

## Fewer prompts

Claude Code asks before every tool call. For batch work,
**auto mode** lets routine commands through and still stops
on risky ones (Shift+Tab, or `--permission-mode auto`). It
trusts this checkout, not your servers: changes over SSH need
the hosts under `autoMode` in `~/.claude/settings.json`.
`/auto-mode-setup` drafts the block; `rules/service-reload.md`
explains it.

```bash
claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com"
```

For CI the strictest setup is `--permission-mode dontAsk`
with an allowlist. `--dangerously-skip-permissions` drops all
review, including the defence against malicious server
output: disposable environments only. In OpenCode, `-p` is
`opencode run "…"`. See the
[permission modes docs](https://code.claude.com/docs/en/permission-modes).

A nightly check from cron:

```
17 6 * * * cd /path/to/jolitz && flock -n \
  /tmp/jolitz-cron-server1.lock timeout 30m \
  /abs/path/to/claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report" >> ~/jolitz-cron.log 2>&1
```

Run the prompt once interactively first, so its one-time
questions are answered. A systemd timer variant is in
`rules/scheduled-housekeeping.md`.

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

```bash
bin/jolitz-backup                        # writes a .tar.gz
bin/jolitz-backup -o <path>              # to a given path
bin/jolitz-backup --list                 # dry run
bin/jolitz-backup --restore <file.tar.gz>
```

Restore validates the archive and does not overwrite without
`--force`.

**Team use.** Everyone keeps a personal `memory/user.md`.
To share server state, edit `.gitignore` as its comments say,
keep each member's own machine out (e.g.
`memory/servers/alices-mbp/`) and commit server memory after
sessions. `user.md`, `blacklist.md`, `readonly.md` and
`opencode.json` are never shared; back them up.

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

## Updates

Claude Code pulls `main` at session start, unless the
checkout is pinned, on another branch, or
`JOLITZ_NO_UPDATE=1` is set. The version is in `VERSION`, the
changes in `CHANGELOG.md`.

```bash
bin/jolitz-update                       # pull latest
bin/jolitz-update --check               # check only
bin/jolitz-update --pin v3.3.3-jolitz.1
bin/jolitz-update --unpin
```

## Systems

| Family  | Distributions                     | Rule file          |
| ------- | --------------------------------- | ------------------ |
| Debian  | Debian, Ubuntu                    | `rules/debian.md`  |
| RHEL    | RHEL, CentOS, Fedora, Rocky, Alma | `rules/rhel.md`    |
| SUSE    | openSUSE, SLES                    | `rules/suse.md`    |
| macOS   | Apple Silicon and Intel           | `rules/macos.md`   |
| FreeBSD | all versions                      | `rules/freebsd.md` |
| NetBSD  | 10.x and 11.x                     | `rules/netbsd.md`  |
| Unraid  | observed on 7.3                   | `rules/unraid.md`  |

Other distributions get the general rules.

Claude Code is what jolitz is developed with. OpenCode reads
the same `CLAUDE.md` and `.claude/skills/` while
`OPENCODE_DISABLE_CLAUDE_CODE` is unset. Other tools get the
rule layer; the skills (housekeeping, security, email, fleet
audit, macOS cleanup) need Skills support.

### A local model

With [Ollama](https://ollama.com) and OpenCode jolitz runs on
your own hardware:

```bash
ollama pull qwen3.5:9b
ollama run qwen3.5:9b
>>> /set parameter num_ctx 16384
>>> /save qwen3.5:9b-16k
>>> /bye
cp memory/opencode.json.example memory/opencode.json
opencode
```

Ollama's default context of 4096 tokens is too small for tool
use. Set `baseURL` and the model in `memory/opencode.json` and
pick it with `/models`. 14B and up call tools more reliably.
See the [OpenCode provider docs](https://opencode.ai/docs/providers/).

## Logs

```bash
journalctl -t jolitz --since "2026-02-01"

# macOS
log show \
  --predicate 'senderImagePath CONTAINS "logger"' \
  --info --last 7d | grep jolitz
```

## Layout

```
CLAUDE.md        instructions for the AI tool
rules/           rule files per platform and topic
.claude/skills/  housekeeping, security, email, fleet audit,
                 macOS cleanup
.claude/hooks/   update check and the guard
bin/             jolitz-update, jolitz-backup, jolitz-migrate
memory/          your state (gitignored)
assets/trailer/  script that renders the trailer
```

## Where it comes from

jolitz started as a fork of
[heinzel](https://github.com/wintermeyer/heinzel) by Stefan
Wintermeyer, whose work it builds on. When heinzel 3.0 moved
OS replacement, accounts, deploy users, secret rotation,
fleet audits and scheduled runs into a paid edition, jolitz
kept them in the open. Every feature is free and stays free.
jolitz is not affiliated with or endorsed by the heinzel
author.

The name honours Bill and Lynne Jolitz, who wrote 386BSD and
put BSD on ordinary PCs in 1992. FreeBSD and NetBSD grew out
of it, and with them many of the servers jolitz looks after.
It is not affiliated with or endorsed by them.

## Contributing

Bug reports, ideas and pull requests are welcome; see
[CONTRIBUTING.md](CONTRIBUTING.md). A fix that is not
specific to jolitz is worth sending to
[heinzel](https://github.com/wintermeyer/heinzel) too.

## License

MIT, see [LICENSE](LICENSE). The copyright notice of the
original heinzel code is kept, as the license requires. No
warranty: jolitz runs commands on production servers.

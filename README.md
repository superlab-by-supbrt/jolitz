# jolitz — System Administration with Safety Guardrails

jolitz is a set of rules that turns an AI coding assistant
into a cautious, methodical sysadmin. Describe what you need
in plain English. It works out the right commands for your
OS, explains each one, and waits for your approval. It backs
up configs, tests before applying, and remembers every server
it has worked on.

It manages Linux, FreeBSD, NetBSD and macOS, remote over SSH or on
the local machine, and runs in
[Claude Code](https://docs.anthropic.com/en/docs/claude-code),
[OpenCode](https://opencode.ai) or any terminal AI tool that
reads project files and runs shell commands.

jolitz is a fork of
[heinzel](https://github.com/wintermeyer/heinzel) by Stefan
Wintermeyer; see [Origin](#origin). Inside, the rules, skills
and journal entries still call the agent "heinzel", which
keeps merges from upstream simple. Throughout this README,
"Heinzel" means the agent as it runs.

## Install

You need:

- **An AI coding assistant in the terminal**, e.g. Claude
  Code or OpenCode.
- **SSH access to the server** as a normal user or as root,
  with a key that does not ask for a passphrase. Not needed
  for the local machine. New to SSH keys? See the
  [Arch wiki guide](https://wiki.archlinux.org/title/SSH_keys).
- **A workstation** running Linux, macOS, FreeBSD or Windows.
  On Windows use
  [WSL](https://learn.microsoft.com/windows/wsl/), or start
  the AI tool from the Git Bash of
  [Git for Windows](https://gitforwindows.org/). PowerShell
  and `cmd.exe` are not supported.

Then:

```
git clone https://github.com/superlab-by-supbrt/jolitz.git
cd jolitz
claude        # or: opencode
```

```
❯ Install postgresql on server1.example.com
```

On the first connection Heinzel may ask what it cannot
detect, usually the SSH user. The answer goes to
`memory/user.md`, so it asks once. You can pre-fill that file
from `memory/user.md.example`. It also holds your language
(`Language: German`).

Heinzel proposes every command, says what it does and why,
and waits for your approval.

Heinzel shares one SSH connection per host and keeps it open
for 10 minutes after the last call. The sockets live in
`~/.cache/heinzel` (mode 0700), so any process of your local
user can use an open connection without the key.

## What you can ask

- `Check on web1.example.com`: reads the server's memory file
  and recent changes, then picks up where the last session
  ended.
- `Run housekeeping on app.example.com`: a health report on
  disk, memory, load, updates, firewall, certificates and
  failed services. Problems come first.
- `Run a security audit on app.example.com`: SSH, firewall,
  accounts, open ports and kernel settings, by severity.
- `Run a fleet audit`: compares update, sshd, firewall, mail
  and time-sync policies across all known servers and shows
  where they disagree. Changes nothing.
- `Email me the output of "df -h" from app.example.com`:
  sends text or files by mail, from your workstation or from
  the server.
- `Clean up leftovers of old apps on this Mac`: finds what
  removed apps left in `~/Library` and `/Library`. Only what
  you approve goes to the Trash.
- `Update all Homebrew packages on this Mac`: local mode, no
  SSH, same rules.
- `Show me an overview of my servers`: builds
  `memory/overview/index.html` from memory, without SSH (see
  [Fleet overview](#fleet-overview)).
- `Give me the handover sheet for web1.example.com`: one page
  with facts, open to-dos, standing constraints, own rules
  and every change with its way back.
- `Run a fleet audit for prod`: the audit, limited to servers
  tagged `prod`, with what changed since the last audit.
- `Email me the weekly fleet summary`: changes, to-dos,
  stale servers, end-of-life dates and audit drift as one
  plain-text mail from your workstation.
- `/plan Migrate the database on db.example.com`: explores
  and drafts a plan, changes nothing until you approve.
  `/plan` is Claude Code. In OpenCode, ask Heinzel to plan
  first.

More that happens without asking:

- **OS detection.** On first contact Heinzel detects the OS
  and hardware and loads the rule file for that platform.
- **Memory.** Each server gets a folder under
  `memory/servers/` with its state, a changelog and an open
  to-do list. An interrupted job shows up as pending on the
  next connection.
- **Long jobs.** A command that can take more than a
  minute runs as a job on the host, output and exit
  status in files, and is read back later, after a
  dropped connection or in a new session too
  (`rules/ssh-connections.md`).
- **DNS aliases.** Several names for the same IP share one
  memory. Each alias can have its own SSH user.
- **SSH ports.** A port other than 22 is stored per server.
  List the ports you use as `Alternative SSH ports:` in
  `memory/user.md`. Heinzel tries those and never scans.
- **Email details.** Mails carry a short signature with the
  operator's name (`Operator name:` in `memory/user.md`) and
  headers that keep out-of-office replies away. The first
  mail per host asks where to send from. Heinzel uses an
  existing MTA and asks before installing one.

The macOS cleanup scanner needs the Command Line Tools
(`xcode-select --install`).

## Origin

This is a fork of
[heinzel](https://github.com/wintermeyer/heinzel) by Stefan
Wintermeyer, under the MIT license. It keeps the work that
upstream moved out in 3.0: partitions, boot loaders and OS
replacement (`rules/os-replacement.md`, `rules/efi-boot.md`),
accounts, SSH keys, sudo and deploy users
(`rules/deployment.md`), secret rotation (`rules/secrets.md`),
the fleet audit skill and scheduled housekeeping. It is not
affiliated with or endorsed by the upstream author.

## Safety & guardrails

The safety rules are part of every session.

- **Asks before acting.** Destructive commands, firewall
  changes, reboots and service restarts need your approval.
  Reloads run on their own when the config test passes
  (`rules/service-reload.md`, `memory/service-policy.md`).
- **Hard guardrails (Claude Code).** A PreToolUse hook
  (`.claude/hooks/guard-taboos.sh`) blocks the absolute
  taboos in every permission mode, even
  `--dangerously-skip-permissions`: halt and poweroff,
  `mkfs`, partition-table writers, boot-loader and EFI
  writers, deleting or overwriting SSH keys, writes to
  `sshd_config`, deleting accounts, writes to `sudoers`,
  the account files and the server lock. It also catches
  them inside `ssh host "…"`, behind `python3 -c` and when
  the file-edit tools target one of those files. Read-only forms
  (`fdisk -l`, `efibootmgr -v`, `visudo -c`) stay allowed.
  For a legitimate exception such as an OS replacement,
  start the session with `HEINZEL_GUARD_DISABLE=1`. Attack
  tools and the server lock stay blocked even then. With the
  guard on, the hook and `.claude/settings*.json` are
  protected, and an assignment of the variable in a command
  or in a written file is always denied, so the agent cannot
  lift the guard itself. OpenCode does not run this hook;
  there the written rules are the safety layer.
- **Guard log, opt-in.** Start the session with
  `HEINZEL_GUARD_LOG=1` and every denial is written to
  `~/.cache/heinzel/guard.log` (mode 0600): time, reason and
  the first 200 characters of the command, or only the path
  of a file edit. The overview counts them by reason. A
  denial that blocked real work is a false positive worth an
  issue. A command line can contain a secret, which is why
  the log is off by default. Writing it never changes a
  decision.
- **Verifies before it reports.** "It is gone since the
  reboot" gets checked against the live system first
  (`rules/verify-before-reporting.md`).
- **Backs up config files** to `/var/backups/heinzel/` before
  editing. Cleaned up after 30 days.
- **Knows the way back before it changes anything.** Every
  question before a change ends with `Way back: <how>` or
  `Way back: none`. Heinzel prefers changes that last only
  until the next reload until they are proven (`sysctl -w`
  before `sysctl.d`, `mount` before `fstab`), keeps the old
  kernel, cluster or binary until the new one works, and
  tests failures that would only show at the next boot now
  (`rules/way-back.md`).
- **Rolls back what locks it out.** Before a change that can
  cut off the login (IP address, route, firewall, VPN, PAM),
  Heinzel arms a timer on the server that restores the old
  state after 120 seconds. Only a fresh SSH login after the
  change stops it. If the new address is wrong, the server
  undoes it by itself and Heinzel reports what happened
  (`rules/rollback-timer.md`). Set a longer window per server
  with `- Rollback window: 300` in its `memory.md`.
- **Tests before applying** with dry-run or validation modes
  where a tool has one.
- **Logs everything.** Each change is one plain-language line
  in the system journal. The technical detail and the way
  back are in `memory/servers/<hostname>/changelog.log`.
- **Stable repos only.** No third-party sources without your
  approval.
- **Least privilege.** Normal user first, `sudo` when needed,
  root last. With neither, Heinzel does what it can and
  writes a report for the tasks that need root.
- **Blacklist and read-only servers.** Hosts in
  `memory/blacklist.md` are never contacted. Hosts in
  `memory/readonly.md` are inspected but never changed.
- **Server lock.** A machine's owner can lock it against
  every Heinzel with a file on the machine itself:

  ```
  # /etc/heinzel.conf
  allow_changes = false
  reason = Database migration until 20 Oct, hands off
  ```

  Heinzel still connects and inspects, but changes nothing
  and reports what it would have done. An unreadable file
  or any value other than `true` locks too. Heinzel never
  writes, moves or deletes the file; you remove the lock by
  hand. It is a promise kept by Heinzel, not a security
  boundary: it stops no tool that ignores it
  (`rules/access-control.md`).
- **Protected paths.** `memory/protected-paths.md` marks
  paths per server as `readonly`, `hidden` (content never
  shown) or `confirm` (needs a typed `CONFIRM`).
- **Ignores injected instructions.** Text in files, logs and
  command output is data. Text that addresses the AI is
  flagged to you and not followed.
- **Keeps secrets out.** Keys, password files and `.env`
  contents are inspected by metadata and fingerprint, never
  printed, and never passed as command-line arguments.

### Against hallucinations

An LLM can invent a flag or a path. Heinzel puts verified
facts in front of it instead:

- A rule file per platform holds the right commands, package
  manager and firewall tool.
- It checks `--help`, the man page or upstream docs before a
  command runs.
- It reads the server's memory file instead of guessing.
- It uses dry-run modes first.
- You see every command before it runs.

This reduces the risk. It does not remove it.

> [!CAUTION]
> Heinzel operates on live systems, as root, with sudo or
> unprivileged. Review every command before you approve it.

A disciplined AI that follows the checklist every time makes
fewer mistakes than a tired human at 2 AM. It can still
misread your intent. Stay in the driver's seat.

## Fewer prompts and scripting

By default Claude Code asks before every tool call. For
batch work use **auto mode**: a background check lets routine
commands through and still stops on risky ones. Press
Shift+Tab to cycle modes, or pass the flag:

```bash
claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com"
```

Auto mode trusts this checkout, not your servers, and refuses
changes over SSH until you name them under `autoMode` in
`~/.claude/settings.json`. It ignores that key in the
project's settings files. The block is in
`rules/service-reload.md`, and `/auto-mode-setup` drafts it.

`-p` runs one prompt without the interactive UI. In OpenCode
that is `opencode run "…"`.

- For CI, the strictest setup is `--permission-mode dontAsk`
  with an allowlist (`--allowedTools` or `permissions.allow`
  in `.claude/settings.json`).
- `--dangerously-skip-permissions` removes all review,
  including the protection against malicious text in server
  output. Use it in disposable environments only.
- Whatever the mode, Heinzel's own rules still apply. See
  the [permission modes docs](https://code.claude.com/docs/en/permission-modes)
  for details.

A nightly health check by cron:

```
17 6 * * * cd /path/to/heinzel && flock -n \
  /tmp/heinzel-cron-server1.lock timeout 30m \
  /abs/path/to/claude --permission-mode auto \
  -p "Run housekeeping on server1.example.com and \
email me the report" >> ~/heinzel-cron.log 2>&1
```

Run the exact prompt interactively once first, so one-time
questions are answered and stored. Details and a systemd
timer variant: `rules/scheduled-housekeeping.md`.

## Fleet overview

Everything heinzel learns about your servers is in
`memory/`. `bin/heinzel-overview` turns it into pages you can
read at a glance, without connecting to any server:

```bash
bin/heinzel-overview            # memory/overview/index.html
bin/heinzel-overview --eol      # first fetch end-of-life dates
bin/heinzel-overview --tag prod # only servers tagged prod
bin/heinzel-overview --text     # plain text, last 7 days
```

![Fleet overview with servers, end-of-life dates, to-dos, changes and audit drift](assets/overview/overview.png)

The start page shows:

- **Needs attention:** releases past or within 180 days of
  their end of life, servers not connected for 90 days,
  settings that differ between servers or changed since the
  last fleet audit, and memory files with a problem.
- **Servers:** OS, end of life, IP, SSH port, aliases, tags,
  own rule overrides, last connection (amber after 30 days,
  red after 90), open to-dos and the last change.
- **Open to-dos** of all servers, **changes** of the last 30
  days with their detail and way back, the **fleet audit**
  table, and, if you enabled it, the **guard denials**.

Tag buttons filter the page. Each server gets a handover
sheet, `memory/overview/hosts/<hostname>.html`: facts, open
to-dos, standing constraints (the `Flags:` of its
changelog), its `rules.md`, every change with detail and
rollback, its audit values and the full `memory.md`. It
prints cleanly.

![Handover sheet of one server](assets/overview/host.png)

Try it on the demo data in the repository:

```bash
bin/heinzel-overview -d assets/overview/demo -o /tmp/fleet-demo
open /tmp/fleet-demo/index.html   # xdg-open on Linux
```

### Tags

Tell heinzel what a server is for, and it records it in the
server's `memory.md`:

```markdown
# web1.example.com
- IP: 203.0.113.10
- OS: Ubuntu 24.04.3 LTS
- Tags: prod, web
- Last connected: 2026-10-27
```

`Run a fleet audit for prod`, `bin/heinzel-overview --tag
prod` and the fleet summary mail all take a tag.

### End of life

`--eol` asks [endoflife.date](https://endoflife.date) for the
releases of the systems in your memory and caches the answer
in `memory/eol/`. Only product names such as `debian` leave
your machine, never a host name. The page names the source
and the fetch date. Without `--eol` it uses the cache. Known
products: Debian, Ubuntu, RHEL, Rocky, AlmaLinux, CentOS
Stream, Fedora, openSUSE Leap, SLES, FreeBSD, NetBSD, macOS,
Alpine, Proxmox VE and OPNsense.

### Plain text and mail

`--text` prints the same summary for a terminal or a mail.
On the demo data:

```
$ bin/heinzel-overview -d assets/overview/demo --text --tag prod
jolitz fleet summary, 2026-10-28, tag prod
Hosts: 2, open to-dos: 2, changes in 7 days: 1

Changes, last 7 days
  2026-10-27 web1.example.com: nginx now sends HSTS for app.example.com — because the security audit flagged its absence

Open to-dos
  db1.example.com: Upgrade to Debian 12 (Bookworm)
  db1.example.com: Move PostgreSQL 13 to 17 with pg_upgradecluster

Not connected for 30 days or more
  none

End of life
  db1.example.com: Debian 11 (Bullseye), ended 2026-08-31

Fleet audit 2026-10-27 09:00: 3 settings differ, 2 changed since 2026-09-30 09:00
  db1.example.com firewall.state: active -> inactive
  db1.example.com time.service: chrony -> systemd-timesyncd

Memory lint: 0 warnings
EOL dates: endoflife.date, fetched debian 2026-10-28, macos 2026-10-28, rocky-linux 2026-10-28, ubuntu 2026-10-28
```

`Email me the weekly fleet summary` sends this from your
workstation. Scheduled every Monday by cron:

```
23 7 * * 1 cd /path/to/jolitz && flock -n \
  /tmp/heinzel-cron-fleet.lock timeout 10m \
  /abs/path/to/claude --permission-mode auto \
  -p "Email me the weekly fleet summary" \
  >> ~/heinzel-cron.log 2>&1
```

### Fleet audit history

Each fleet audit saves its values in
`memory/audits/<YYYY-MM-DD-HHMM>.json` and reports what
changed since the previous one. A change no changelog entry
explains was made outside heinzel, and the report says so.
Format: `.claude/skills/heinzel-fleet-audit/references/snapshot.md`.

### Memory lint

`bin/heinzel-memory-lint` checks the memory files. Heinzel
runs it after each memory update, and the overview runs it
before it writes a page:

```
$ bin/heinzel-memory-lint
WARN  db1.example.com: memory.md: tag "DB" is not lowercase letters, digits and -
ERROR web1.example.com: servers/web1.example.com/memory.md:14: looks like a secret value; record its location, not the value (rules/secrets.md)
memory lint: 4 hosts, 1 errors, 1 warnings
```

- **ERROR:** something that looks like a credential: a
  private key block, a known token format, a password in a
  URL or in `PASSWORD=`, a password hash. The line number is
  printed, the value never is. The overview refuses to build
  until it is gone, so a secret never lands in a page you
  might forward. `Database password: in /root/.pgpass
  (0600)` is a location and passes.
- **WARN:** no `OS:` or `Last connected:` line, a date that
  is not `YYYY-MM-DD`, a tag that is not lowercase, a
  `memory.md` over 40 lines, a `todo.md` with nothing open, a
  changelog entry without timestamp. `--strict` fails on
  warnings too.

The patterns are a backstop. A secret in a format they do not
know passes.

## Your data: `memory/`

All your state is plain text in one directory:

- `user.md`: SSH usernames, language, operator name
- `blacklist.md`, `readonly.md`, `protected-paths.md`: access
  policies
- `service-policy.md`: which services may reload or restart
  without asking
- `servers/<hostname>/`: memory, changelog, to-do and rule
  overrides per server
- `custom-rules/`: your global rule overrides
- `network.md`, `housekeeping.md`: cross-server facts and
  your own checks
- `audits/`: one snapshot per fleet audit
- `overview/`, `eol/`: pages and cached end-of-life dates
  from `bin/heinzel-overview`; safe to delete
- `opencode.json`: your OpenCode config

### Backup and restore

```bash
bin/heinzel-backup                        # writes a .tar.gz
bin/heinzel-backup -o <path>              # to a given path
bin/heinzel-backup --list                 # dry run
bin/heinzel-backup --restore <file.tar.gz>
```

Restore refuses to overwrite existing content without
`--force` and validates the archive before writing.

### Team setup

`memory/` is gitignored by default. To share server state:

1. Everyone copies `memory/user.md.example` to
   `memory/user.md`. This file always stays personal.
2. Edit `.gitignore` to track server memory. The comments in
   the file say which lines to change.
3. Keep each member's own machine out of git, e.g.
   `memory/servers/stefans-mbp/`.
4. Commit server memory after sessions.

`user.md`, `blacklist.md`, `readonly.md` and `opencode.json`
are never shared and still need the backup.

## Rule customization

Change behaviour without editing the upstream rule files.
Three layers, later wins:

1. `rules/<name>.md` (upstream)
2. `memory/custom-rules/<name>.md` (yours, all servers)
3. `memory/servers/<hostname>/rules.md` (one server)

```markdown
## Add: Docker cleanup
New rules applied alongside the base.

## Replace: Firewall
Replaces the matching base section entirely.

## Remove: Common Pitfalls > snap
Skip this base section.
```

A section without a prefix is an addition.
`memory/custom-rules/all.md` applies to every server. Skills
follow the same chain, e.g.
`memory/custom-rules/heinzel-housekeeping.md`.

## Updates

The version is in `VERSION`, the changes in `CHANGELOG.md`.
Claude Code updates Heinzel at session start with a
`git pull`, unless you are pinned, on another branch, or have
set `HEINZEL_NO_UPDATE=1`. Elsewhere:

```bash
bin/heinzel-update              # pull latest
bin/heinzel-update --check      # check only
bin/heinzel-update --pin v2.28.0
bin/heinzel-update --unpin
```

Coming from 1.x? The update moves `rules/custom/` and
`opencode.json` into `memory/` by itself. If you are pinned
to a 1.x tag, run `bin/heinzel-update --unpin` first.

## Supported systems

| Family  | Distributions                     | Rule file          |
| ------- | --------------------------------- | ------------------ |
| Debian  | Debian, Ubuntu                    | `rules/debian.md`  |
| RHEL    | RHEL, CentOS, Fedora, Rocky, Alma | `rules/rhel.md`    |
| SUSE    | openSUSE, SLES                    | `rules/suse.md`    |
| macOS   | Apple Silicon and Intel           | `rules/macos.md`   |
| FreeBSD | all versions                      | `rules/freebsd.md` |
| NetBSD  | 10.x and 11.x                     | `rules/netbsd.md`  |
| Unraid  | observed on 7.3                   | `rules/unraid.md`  |

Other distributions work with general best practices.

### AI tools

Claude Code is the tool Heinzel is developed with. OpenCode
reads the same `CLAUDE.md` and `.claude/skills/`, as long as
`OPENCODE_DISABLE_CLAUDE_CODE` is unset. Any other tool that
reads project files gets the rule layer. The skills
(housekeeping, security audit, email, fleet audit, macOS
cleanup) need a tool that supports Skills.

### OpenCode with a local model

Heinzel can run on your own hardware with
[Ollama](https://ollama.com):

```bash
ollama pull qwen3.5:9b
ollama run qwen3.5:9b
>>> /set parameter num_ctx 16384
>>> /save qwen3.5:9b-16k
>>> /bye
cp memory/opencode.json.example memory/opencode.json
opencode
```

Ollama's default context of 4096 tokens is too small for
tool use, hence the larger variant. Adjust `baseURL` and the
model name in `memory/opencode.json`, then pick the model
with `/models`. Larger models (14B and up) make more reliable
tool calls. See the
[OpenCode provider docs](https://opencode.ai/docs/providers/)
for more.

## Logs

Every change is logged on the server:

```bash
journalctl -t heinzel
journalctl -t heinzel --since "2026-02-01"

# macOS
log show \
  --predicate 'senderImagePath CONTAINS "logger"' \
  --info --last 7d | grep heinzel
```

## Project structure

```
CLAUDE.md        main instructions for the AI tool
rules/           rule files per platform and topic
.claude/skills/  housekeeping, security, email, fleet audit,
                 macOS cleanup
.claude/hooks/   update check and the taboo guard
bin/             heinzel-update, heinzel-backup,
                 heinzel-migrate, heinzel-overview,
                 heinzel-memory-lint
lib/             the renderer behind heinzel-overview
tests/           tests for lint and overview
memory/          your state (gitignored)
assets/trailer/  script that renders the README trailer
assets/overview/ screenshots and demo data for the overview
```

## Why the name?

A homage to Bill and Lynne Jolitz, who wrote 386BSD and put
BSD on ordinary PCs in 1992. FreeBSD and NetBSD grew out of
it, and with them a good part of the servers this tool looks
after. The project is named in their honour only; it is not
affiliated with or endorsed by them.

The upstream name comes from the
[Heinzelmännchen](https://en.wikipedia.org/wiki/Heinzelm%C3%A4nnchen),
the helpful gnomes of Cologne who did the work left undone
while the city slept.

## Contributing

Bug reports, feature requests and pull requests are welcome;
see [CONTRIBUTING.md](CONTRIBUTING.md). Changes that are not
specific to this fork are better sent to
[heinzel](https://github.com/wintermeyer/heinzel) upstream.

## License

MIT, see [LICENSE](LICENSE). The original copyright notice of
heinzel by Stefan Wintermeyer is kept, as the license
requires; changes in this fork are covered by the second
notice. The software comes without warranty: it runs commands
on production servers, so read every command before you
approve it.

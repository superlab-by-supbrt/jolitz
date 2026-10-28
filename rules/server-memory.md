# Server Memory

Each server: `memory/servers/<hostname>/` with
`memory.md`, `changelog.log`, optionally `todo.md`,
and optionally `rules.md` (per-server rule
overrides — see CLAUDE.md → Rule Overrides).

**On first connection:** create directory and
`memory.md` with at least:

```markdown
# hostname.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- CPU: 4x Intel Xeon E-2236 @ 3.40GHz
- RAM: 16 GB
- Disk: 80 GB (/ ext4, 45% used)
- Last connected: 2026-02-25
```

Adapt fields to OS (add Arch, Homebrew for macOS;
add `Mode: local` for localhost). Add
`- SSH port: <port>` only for a port other than 22
(`rules/ssh-port.md`).

Keep `- Key: value` at the start of the line for
these fields, and the date as `YYYY-MM-DD`:
`bin/jolitz-overview` reads them to build the fleet
overview, and `bin/jolitz-memory-lint` checks them.

**Tags.** When the user names a role or group for a
server ("this is prod", "one of the customer-a
boxes"), record it:

```markdown
- Tags: prod, web, customer-a
```

Lowercase letters, digits and `-`, comma-separated.
Never invent a tag. The fleet audit, housekeeping
across several hosts and the overview take a tag to
narrow their scope ("fleet audit for prod").

**Owner and services.** Two optional lines, both read
by the overview (Servers table, services matrix,
`overview.json`):

```markdown
- Owner: alice
- Services: nginx 1.26, postgresql 17, docker
```

`Owner` is a name or team the user gave. `Services`
is comma-separated; a version follows the name after
a space and may be left out. Record only what was
seen on the host or said by the user.

**Guest software.** A host that runs an application
jolitz does not administer carries `- Guest: sap`
(lowercase token, comma-separated for several); the
token names `rules/<token>.md`, read on every
connection (`rules/guest-software.md`).

**Network links.** Cross-server facts live in
`memory/network.md` as free prose. One line form is
read by the overview and drawn as a graph:

```markdown
- Link: web1.example.com -> db1.example.com (postgresql 5432)
```

The label in parentheses is optional. The host on the
left opens the connection. Prose around these lines
stays untouched.

**Lint after editing.** After changing memory
files, run `bin/jolitz-memory-lint <hostname>`. An
`ERROR` means a value that looks like a credential:
replace it with its location before anything else.
Fix `WARN` lines when they concern the file just
edited.

**Update memory immediately after any system
change.** Keep it compact (~30 lines max). Remove
outdated entries, merge related items.

**Update `Last connected:` on every connection.**

Memory files never hold credential values — see
`rules/secrets.md`.

Edit memory files with the agent's file-edit tool,
never through `sed -i` or an interpreter
(`python3 -`, `perl -e`). Memory often names key
paths and `/etc/ssh/sshd_config`. The taboo guard
blocks a command line that carries them: it cannot
tell a note about a path from a write to it.

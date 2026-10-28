# Access Control

Rules for the server blacklist and read-only server
list, which share one file format and lookup logic,
and for the server lock a host sets for itself.

## Shared File Format

Plain list — one entry per line. An optional leading
`- ` bullet is allowed and ignored. Everything after
`#` is a comment; blank lines are ignored. Entries
can be a hostname (matched against the target
hostname) or an IP address (matched against the
resolved IP). IPs are the most robust form — prefer
them. Files are created on first need — do not
pre-create them.

```markdown
# Example

- server.example.com   # bullet optional
203.0.113.50
```

The user can add or remove entries by asking jolitz
to edit the file, or by editing it directly.

## Shared Lookup Logic

For both files, the check is (stop at the first
match):

1. If the file does not exist, skip (nothing to
   check).
2. Is the target hostname, or the name `ssh -G`
   maps it to (`rules/dns-aliases.md` → Detection
   step 1), listed?
3. Resolve the target's IP(s)
   (`rules/dns-aliases.md` → Detection step 1).
   Is a resolved IP listed?
4. Resolve each listed hostname to its IP(s) the
   same way, once per session, and compare against
   the target's IP(s). This catches DNS aliases of
   listed hosts that a plain string match would
   miss.

If nothing resolves, fall back to exact string
matching and tell the user explicitly that the
IP-level check could not be performed. Err on the
side of caution for anything ambiguous.

## Server Blacklist

**File:** `memory/blacklist.md`

**When to check:** before every connection attempt —
before OS detection, before DNS alias detection,
before any SSH command. This is the very first step
when a user mentions a server.

**On match:** refuse to connect. Tell the user:
"This server is blacklisted in
`memory/blacklist.md`. I will not connect to it."
Do not proceed. Do not ask for override. Do not run
any SSH commands against the server.

## Read-Only Servers

**File:** `memory/readonly.md`

**When to check:** right after the blacklist check,
before OS detection and DNS alias detection.

**On match:** announce read-only mode to the user:
"This server is marked read-only in
`memory/readonly.md`. I will connect and inspect,
but I will not make any changes."
Proceed with the connection — unlike the blacklist,
read-only does not block access.

**Allowed in read-only mode:**
- All read-only operations: SSH inspection, status
  commands, reading files and logs
- Housekeeping checks and security audits
- Local memory and changelog updates, including
  creating `todo.md` (a purely local file — only
  the remote host is read-only)
- `logger -t jolitz` entries on the server

**Blocked in read-only mode:**
- Package install, update, or remove
- Service start, stop, enable, disable, or restart
- Config file edits on the server
- Firewall, user/group, or file write changes
- Reboots

**Deferred modifications:** when a blocked action is
needed, announce it briefly and continue with
read-only work. At session end, present a
modification report in the same format as the
unprivileged mode sysadmin report.

**No override:** read-only mode is a hard constraint.
The user must remove the entry from
`memory/readonly.md` before jolitz will modify the
server.

## Server Lock: `/etc/jolitz.conf`

The machine's owner can lock it against changes from
the machine itself, for every jolitz that connects,
whoever runs it:

```
# /etc/jolitz.conf
allow_changes = false
reason = Database migration until 20 Oct, hands off
```

`key = value` per line, `#` comments, blank lines
ignored. The same path on Linux, FreeBSD and macOS
(`/etc` is `/private/etc` there). Root-owned, mode
`0644`.

**When to check:** on every connection, as the
first SSH call once the SSH user and port are known
(`rules/first-connection.md`, step 7). Local mode
runs the same probe in the local shell. Read it
fresh every time, never from server memory. Run the
probe in a call of its own:

```bash
if [ -e /etc/jolitz.conf ]; then
  cat /etc/jolitz.conf || echo JOLITZ_CONF_UNREADABLE
else
  echo JOLITZ_CONF_ABSENT
fi
echo JOLITZ_CONF_DONE
```

**How to read it (fails closed):**

- `JOLITZ_CONF_ABSENT`: no lock, work normally.
- `allow_changes = true`: no lock. Case and the
  spaces around `=` do not matter.
- Anything else is a lock: `false`, a missing key,
  any other value, an empty file,
  `JOLITZ_CONF_UNREADABLE`.
- No `JOLITZ_CONF_DONE` means the check did not
  run. Change nothing until it has.

**On a lock:** switch to read-only mode exactly as
for `memory/readonly.md` above (same allowed and
blocked lists, same deferred modification report).
Tell the user: "This server is locked in
`/etc/jolitz.conf`. I will inspect, but I will not
change anything." Quote `reason` when it is set.

Either source is enough: a host listed in
`memory/readonly.md` stays read-only whatever the
file says.

**Never lift it.** jolitz never creates, edits,
moves, deletes or re-permissions `/etc/jolitz.conf`,
not even on request. The owner changes it by hand.
When the user asks jolitz to lock or unlock a
server, give them the command to run themselves. The
guard hook blocks every write to the file.

The lock is cooperative, not a security boundary: it
stops every tool that honours it. Only withholding
root and sudo keeps a tool out that does not.

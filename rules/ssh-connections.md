# SSH Connections: Few and Shared

Firewall rules that rate-limit new connections to
the SSH port (`ufw limit`: 6 in 30 seconds; iptables
`recent` or `hashlimit`) and network IPS signatures
for SSH scans count **TCP connections, not
commands**, successful logins included. A busy
jolitz session can trip them and lock itself out,
and the block looks like a broken host. If that has
already happened, see `rules/ssh-unreachable.md`.

fail2ban, sshguard and sshd's own
`PerSourcePenalties` (OpenSSH 9.8+) count failed
logins instead; see section 3.

## 1. Bundle commands

One call per logical step, not one per command:

    ssh … host 'sh -s' <<'EOS'
    cmd1
    cmd2
    EOS

- Send several files in one `scp`/`rsync`.
- Do not poll a host every few seconds. Run a long
  job on the host and read it back with one call
  that waits there (section 4).
- `ProxyJump` costs a connection to the jump host
  **and** one to the target.

## 2. Share connections

The standard options in `CLAUDE.md` → SSH Options
turn on OpenSSH connection sharing (`ControlMaster`):
repeated calls ride **one TCP connection per local
user, remote user, host and port**. Later calls are
several times faster, open no new connection, and
key agents that confirm each use ask only once.

It does not help with the first connection per host
and remote user, with the first one after
`ControlPersist` expires, or with **failed logins**,
which are never shared.

### Why these values

Keep them as they are:

- **Command line, not `~/.ssh/config`:** works on
  every machine without setup and overrides a
  `ControlMaster` block the user keeps for
  themselves.
- **`~/.cache/jolitz/`, not `~/.ssh/` or `/tmp`:**
  the taboo guard reads any path under `.ssh/` as key
  material and would block every remote `rm`, `mv` or
  `chmod`; `/tmp` is writable by other users.
- **`%C`:** a fixed-length hash. Readable names can
  pass the 104-byte socket path limit on macOS, and
  SSH then fails the call (`ControlPath too long`).
- **`ServerAliveInterval=15`,
  `ServerAliveCountMax=3`:** retire a master with a
  dead network path after 45 seconds, whatever
  `~/.ssh/config` says. `ConnectTimeout` does not
  cover a call over an existing connection.

### Fresh-login options

The second option set in `CLAUDE.md` → SSH Options.
Use it for:

- **Access tests.** Anything that answers "can this
  login still succeed?" — after changing
  `authorized_keys`, an account, its shell or groups,
  PAM, host keys, or firewall rules on the SSH port,
  and the root SSH probe in
  `rules/privilege-escalation.md`. A shared master
  answers from the login **before** the change, so a
  broken login reads as working. Make all related
  changes first, then run one fresh-login call that
  also prints what you need (`id; groups`).
- **A call that hangs or fails while sharing:** a
  stale master. Follow `rules/ssh-unreachable.md`.

`Session open refused by peer` is different: the
master is fine but full (sshd `MaxSessions`, default
10 per connection). Let your own parallel calls to
that host finish, then repeat the call as usual.

### Caveats

- **The socket directory must exist.** If it is
  missing, every call fails *after* the login with
  `unix_listener: cannot bind to path … No such file
  or directory` and exit 255. The host is fine:
  create the directory
  (`mkdir -p -m 700 ~/.cache/jolitz`) and repeat
  the call.
- **`scp` never starts a master.** It passes
  `-oControlMaster=no` ahead of your options, so it
  only reuses one. Open it with an `ssh` call first;
  the onboarding already does.
- **Never close a master you did not start.** Other
  jolitz sessions and scripts on the same local
  account use the same socket path, and
  `ssh -O exit`/`-O stop` ends their sessions too.
  For experiments, use a complete option set with its
  own path (e.g. `~/.cache/jolitz/test-%C`) and close
  only that one.
- **Login records understate activity.** `last`,
  `who` and the sshd log show one login for many
  calls. Use `rules/activity-check.md` to judge
  whether someone else is working on the host.
- **Security.** While a master is open, any process
  of the same local user can use it without key
  approval. Keep `~/.cache/jolitz` at `0700`; on a
  shared account, shorten `ControlPersist`.

## 3. Avoid failed logins

With fail2ban's defaults, 5 counted events in 10
minutes ban the source address for 10 minutes. If
the `recidive` jail is on, 5 bans in a day become a
week on all ports. The user's own failed logins
count toward the same limit. Counted are:

- an unknown user (`Invalid user`);
- a root login refused although the key fits
  (`ROOT LOGIN REFUSED`);
- more wrong keys than `MaxAuthTries` (default 6),
  ending in `Too many authentication failures`;
  the rejected keys count too, unless a later key
  logs in;
- in fail2ban's `ddos` and `aggressive` modes and
  for `PerSourcePenalties`: connections that never
  log in (`nc -z`, `ssh-keyscan`); those two
  fail2ban modes also count every rejected login.

The rules that own these steps keep jolitz clear of
them: `rules/ssh-user.md` (user names),
`rules/privilege-escalation.md` (root probe),
`rules/ssh-unreachable.md` (rejected logins, port
probes) and `rules/ssh-port.md` (other ports).

## 4. Long jobs

The agent's own tool limit ends a call long before
a host is done with a big job: Claude Code allows
2 minutes per call by default and 10 at most. A
dropped connection ends it just the same, and so
does a context reset. The command keeps running on
the host, and the session never learns its exit
status or sees its output.

So a command that can take more than a minute, or
that must not be left half done (package upgrades,
database migrations, `rsync` of a large tree,
backups, builds, a long `find`), never runs inside
the SSH call. It runs as a **job** on the host:
one directory, the command in a file, its output
and exit status in files beside it. The call only
starts it and returns.

    ssh … host 'sh -s' <<'EOS'
    d=~/.jolitz/jobs/$(date +%Y%m%d-%H%M%S)-upgrade
    mkdir -p "$d"
    cat >"$d/cmd" <<'CMD'
    apt-get -o DPkg::Lock::Timeout=300 -y dist-upgrade
    CMD
    (nohup sh -c 'sh "$1/cmd" >"$1/out" 2>"$1/err" &
      echo $! >"$1/pid"; trap "" TERM
      wait $!; echo $? >"$1/status"' sh "$d" \
      </dev/null >/dev/null 2>&1 &)
    ps -o pgid= -p $$ | tr -d " " >"$d/pgid"
    echo "$d"
    EOS

This needs only `sh` and `nohup`, which Linux,
FreeBSD and macOS all have: no root, no `screen` or
`tmux`, nothing to install. The job outlives the
call and the connection. Write it so that a second
run does no harm, because one day you will not know
whether the first one ran: `rsync` rather than `scp`
or `cp`, `mkdir -p`, a check before `useradd`,
`DPkg::Lock::Timeout` instead of failing on the
lock, and never `&& reboot` on the same line as the
upgrade. The reboot is its own step, after the
result was read and the user asked for it.

Read the job back with one call that waits on the
host, never with a call every few seconds (section
1). Set the tool's timeout to cover the wait (Claude
Code: `timeout`, up to 10 minutes):

    ssh … host 'sh -s' <<'EOS'
    d=~/.jolitz/jobs/20261011-141500-upgrade
    i=0
    while [ ! -f "$d/status" ] && [ $i -lt 10 ]; do
      sleep 30; i=$((i+1))
    done
    cat "$d/status" 2>/dev/null || echo running
    tail -n 40 "$d/out" "$d/err"
    EOS

`status` holds the exit code and exists only once
the job is over. No `status` after the wait means
it is still running: repeat the call. No `status`
and nothing left in the job's process group
(`ps -o pid=,comm= -g "$(cat "$d/pgid")"`) means
the host went down with the job: read what it left
behind before anything is started again
(`rules/verify-before-reporting.md` → 7).

To stop a job, signal its process group. sshd gives
every call a group of its own, and the wrapper
ignores the signal, so the status is still
recorded:

    pkill -TERM -g "$(cat "$d/pgid")"

In local mode the job shares the session's group;
stop it by pid instead:

    p=$(cat "$d/pid"); pkill -TERM -P "$p"; kill "$p"

Delete the directory once the result is in the
changelog. Before starting a job, remove what a
dead session left behind:

    find ~/.jolitz/jobs -mindepth 1 -maxdepth 1 \
      -mtime +7 -exec rm -r {} +

Record a running job and its directory in
`memory/servers/<hostname>/todo.md`, so the next
session, or a colleague's, picks it up instead of
starting it again.

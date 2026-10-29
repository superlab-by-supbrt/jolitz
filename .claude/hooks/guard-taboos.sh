#!/bin/sh
# guard-taboos.sh — PreToolUse hook (matcher: Bash, and
# Edit|Write|NotebookEdit with the argument "file").
#
# Mechanically enforces jolitz's absolute taboos from
# CLAUDE.md → Critical Safety Rules, below the model layer:
#
#   - halt / poweroff / shutdown without -r / init 0 /
#     telinit 0 / sysrq-trigger
#   - sleep: systemctl suspend/hibernate/hybrid-sleep/
#     suspend-then-hibernate/sleep and their targets,
#     acpiconf -s 3/4/5, zzz, pmset sleepnow, rtcwake (but
#     -m show), a write onto /sys/power/state
#   - mkfs / mke2fs / newfs* / wipefs (write forms), and
#     formatting under another name: cryptsetup luksFormat/
#     luksErase/erase/reencrypt, zpool labelclear, mdadm
#     --zero-superblock
#   - partition-table writers (fdisk/cfdisk/sfdisk/gdisk/
#     sgdisk/parted/gpart/gpt/diskutil write forms, the
#     attribute bits with them: sgdisk -A, gpart set/unset)
#   - raw-device wipers that leave the partition table
#     alone (blkdiscard, nvme format/sanitize, hdparm
#     secure-erase, badblocks -w, shred, dd, a redirect,
#     tee, cp or a download onto a disk device)
#   - firmware written to a drive: hdparm --fwdownload*,
#     nvme fw-download/fw-commit/fw-activate
#   - destroying SSH keys (host keys, authorized_keys, id_*,
#     or the directory holding them: ~/.ssh, an appliance
#     key store such as /conf/sshd) by any means: rm/shred/
#     truncate/mv/chmod/chown/install/ln/setfacl/patch, find
#     -delete, a redirect, ssh-keygen -f, or a write into
#     one (tee, cp/rsync/scp, dd, sed -i, an editor, curl -o
#     and other output flags)
#   - writes to sshd_config(.d/) under any .../etc/ssh, or
#     to a file an appliance merges into it (/etc/sshd_extra)
#   - the SSH service stopped, disabled or masked (systemctl,
#     service, rc-service, an init script, launchctl)
#   - boot loaders and firmware variables: efibootmgr write
#     forms, grub-install, update-grub, grub-mkconfig -o,
#     bootctl install/update/remove/set-*, bless write forms,
#     grub-set-default, grub-reboot, grub-editenv set/unset/
#     create, systemsetup -setstartupdisk, efivar -w, nvram
#     in a write form, a write or delete under
#     /sys/firmware/efi/efivars/
#   - accounts and sudo: deleting an account or a group
#     (rmuser and rmgroup on AIX), visudo, vipw/vigr, any
#     write to sudoers(.d/) or to the account files
#     (/etc/passwd, shadow, group, master.passwd, AIX's
#     /etc/security/{passwd,group,user,limits})
#   - AIX: the volume manager commands that destroy data
#     (mkvg/extendvg -f, rmlv, reducevg -d, rmfs, chpv -c/-C),
#     the boot list and image (bootlist without -o, bosboot -a,
#     mkboot, alt_disk_copy/install), stopsrc on sshd, ssh,
#     tcpip or everything, fasthalt
#   - any write after a cd into /etc/ssh, sshd_config.d,
#     /etc/sudoers.d, ~/.ssh or /conf/sshd, where the files
#     are named bare, and find -delete with one of them as
#     the root
#   - writing, moving or deleting the server lock
#     /etc/jolitz.conf, which only the machine's owner sets
#     (rules/access-control.md)
#   - a second agent session: claude or opencode with -p,
#     --print, --settings, --permission-mode or
#     --dangerously-skip-permissions
#   - any of those paths reached through a language
#     runtime (python/perl/ruby/node/awk ...), whose file
#     I/O looks nothing like a shell write; awk is exempt
#     for the account files, which the security skill reads
#     with it
#   - a redirect onto a $( ) lookup, or an arrow onto any
#     expansion, whose target nobody sees before it runs
#     (issue #51)
#   - a file-edit tool pointed at sshd_config, a key file,
#     sudoers, an account file, an efivar, the server lock or
#     a raw disk device (see the file-mode section)
#
# A write is recognised in every spelling the shell has for it:
# the three redirect operators > >> >| (ONTO), sed and perl in
# place as -i, -Ei/-ri/-ni/-pi, -i.bak or --in-place (INPLACE),
# the editors vi/vim/nvim/nano/emacs/ed/ex, and an output flag
# alone or closing a cluster (curl -o, curl -fsSLo, wget -qO).
#
# What it deliberately does NOT scan: the body of a heredoc that
# is written to an ordinary file by cat or tee (issue #8). That
# is documentation, not code. Every other heredoc — above all
# `ssh host bash -s <<EOF`, whose body runs remotely — is scanned
# in full.
#
# The taboos are EFFECTS, not a list of binaries. When a new
# tool reaches one of the effects above, it belongs in here,
# and the test matrix gets a line for it. Issue #5 came from
# the reverse: the rules named tools, so cfdisk, diskutil
# eraseDisk, gpt destroy, blkdiscard and a truncating
# redirect over authorized_keys all walked through. Issue #6
# was the same mistake one level in: the rules protecting a
# PATH named the ways a shell spells a write, so
# python3 -c "open('/etc/ssh/sshd_config','a').write(...)"
# was not a write to any of them. Issue #7 was the same
# mistake one level UP the path: the protected paths were key
# FILENAMES, so chown -R alice:alice /home/alice/.ssh
# re-permissioned every key in the directory without naming
# one, while the spelled-out chmod on authorized_keys was
# denied.
#
# The hook scans the ENTIRE command string, so taboos hidden
# inside wrappers like  ssh root@host "mkfs.ext4 /dev/sda1"
# are caught regardless of quoting. A PreToolUse deny blocks
# in every permission mode, including bypassPermissions.
#
# Rules that exempt a read-only form (fdisk -l, sfdisk -d,
# shutdown -r) evaluate that exemption per invocation, not
# against the whole string: it must sit in the same segment as
# the taboo command and follow it. This is stricter than a plain
# whole-string scan, so a taboo word inside quoted prose --
# documentation, commit messages, ticket notes -- is matched
# more often.
#
# Issue #8 narrowed the biggest source of that noise: a heredoc
# body written to a file by cat or tee is data, not code, and is
# no longer scanned. See the heredoc section below for the exact
# conditions and for why the consumer, never the body, decides.
# Writing jolitz's own changelog no longer trips the guard.
#
# Known, accepted false positives that REMAIN (the patterns are
# deliberately coarse — this guard protects production disks, not
# grep pipelines):
#   - A taboo word as a quoted ARGUMENT: `grep poweroff
#     /var/log/syslog`, `systemctl status shutdown.target`.
#     segments() splits on quotes so that ssh host "shutdown -h
#     now" is caught, and after that split an argument and an ssh
#     payload have the same shape. Separating them needs a
#     per-command list of which arguments are data, which is
#     open-ended and would reopen issue #4. Rephrase the probe
#     (`grep 'power[o]ff'`) instead.
#   - `cp /etc/ssh/sshd_config /tmp/` is blocked although it only
#     reads the file — copy out via `cat /etc/ssh/sshd_config >
#     /tmp/copy` instead.
#   - The sshd paths match as substrings, with no boundary on
#     either side: sshd_config.bak, /etc/sshd_extra.bak and a
#     copy staged under another root (mnt/etc/ssh/sshd_config)
#     count as the real file. The left side stays open on
#     purpose, so /usr/local/etc/ssh and a copy under a mount
#     point are covered. Keep backups outside the guarded
#     path, e.g. /root/backup/sshd_config. The same holds for
#     sudoers and the account files: cp /etc/sudoers /root/ is
#     denied, cat /etc/sudoers > /root/sudoers.bak is not.
#   - ssh-keygen with a private key path ANYWHERE in the command:
#     `file /etc/ssh/ssh_host_ed25519_key; ssh-keygen -lf
#     ...key.pub` is denied although each part passes alone.
#     Matching per invocation would miss K=<key>; ssh-keygen -f
#     $K, and what -l does next to ssh-keygen's write modes is
#     unverified, so there is no -l exemption either. Run the
#     fingerprint in a call of its own (rules/secrets.md).
#   - An ssh ControlPath under .ssh/ makes any rm, mv or chmod in
#     the same command look like a key operation. jolitz keeps
#     its sockets in ~/.cache/jolitz for that reason
#     (rules/ssh-connections.md).
#   - rsync with -e "ssh -i ~/.ssh/id_..." AFTER its operands
#     reads as a copy onto that key, because writes_to takes the
#     last word as the destination. Put -e before the operands.
#   - A short-flag cluster that ends in o or O (ls -lo) in front of
#     a protected path reads as an output flag, and rm or cp of an
#     unrelated file after a cd into a protected directory reads
#     as a write there. Run those from another cwd, without the
#     cluster.
#
# Being blocked is EXPECTED behavior. Explain it to the user.
# Never rephrase, re-quote, or otherwise obfuscate a command to
# evade this guard.
#
# jolitz-repo development note: a commit message piped straight
# into `git commit -m`/`-F -` still flows through the command
# string, and git is not a data sink this hook recognizes, so
# writing ABOUT taboo commands there can still trigger it. Write
# the message to a file with a non-Bash tool and use
# `git commit -F <file>` — that executes nothing on any server.
#
# Override for legitimate flows (rules/os-replacement.md runs
# mkfs/sgdisk by design, rules/efi-boot.md writes boot entries):
# the OPERATOR exports JOLITZ_GUARD_DISABLE=1 BEFORE launching
# the session. It lifts the taboos, nothing else. Always on,
# whatever the variable says:
#   - an inline or written assignment of the variable, so the
#     model cannot disarm the guard itself: in a command, or in
#     a file-edit tool's content (settings.json env, a shell rc)
#   - attack tools (rules/ethics.md)
#   - the server lock /etc/jolitz.conf
# With the guard on, its own files are protected too: the
# hooks under .claude/hooks/ and .claude/settings*.json, which
# register the hooks and can carry an env block.

INPUT=$(cat)

# The raw input for the fallbacks below, without the one field
# Claude Code always fills with a path under ~/.claude
# (transcript_path). Left in, it puts every command next to the
# guard's own files, and without jq nothing would pass, not even
# ls. Inside the tool input a quote is escaped (\"), so this
# pattern only meets the top-level field, never text the model
# wrote.
RAW=$(printf '%s' "$INPUT" | sed -E \
  's/"transcript_path"[[:space:]]*:[[:space:]]*"[^"]*"//g')

# Operator-level override: inherited environment only.
GUARD_OFF=0
[ "${JOLITZ_GUARD_DISABLE:-}" = "1" ] && GUARD_OFF=1

# Extract the Bash tool's command string. Without jq (or on
# malformed input) fall back to scanning the raw stdin text —
# that can only over-block, never under-block.
CMD=""
if command -v jq >/dev/null 2>&1; then
  CMD=$(printf '%s' "$INPUT" \
    | jq -r '.tool_input.command // empty' 2>/dev/null) || CMD=""
fi
[ -n "$CMD" ] || CMD="$RAW"
GUARD_MODE=bash
LOG_SUBJECT=$CMD

# --- Heredoc bodies that are DATA, not code -------------------
# Documentation is the one thing that legitimately contains taboo
# words: a changelog entry describing a shutdown checkpoint, a
# rule file about fdisk, a commit message. Scanned as a command
# string, that prose is indistinguishable from an invocation, and
# the guard blocked jolitz's own changelog write over the word
# "shutdown" inside a heredoc.
#
# A heredoc body is only safe to skip when the command consuming
# it CANNOT execute it. That is decided by the consumer, never by
# the body, because these two look almost identical:
#
#   cat >> notes.md <<EOF          <- the body is text
#   ssh host bash -s <<EOF         <- the body RUNS, remotely
#
# So the body is dropped only when the first line is one lone
# pure data sink -- cat or tee writing to an ordinary file -- and
# every other condition fails CLOSED back to scanning everything:
# more than one heredoc, an unrecognized consumer, a missing
# terminator, a /dev/ target, or any awk failure. Whatever sits
# OUTSIDE the body (the first line itself, and everything after
# the terminator) is always still scanned, so
#   cat >> f <<EOF ... EOF; shutdown -h now
# is caught on the trailing command exactly as before.
#
# Deliberate boundary: a data sink whose target is itself
# executed later is excluded from the exemption, because writing
# a taboo INTO such a file reaches the effect on a delay. That
# set is named by EFFECT -- "something runs this file later" --
# and enumerated as scheduler and init locations, anything under
# /etc, launchd, the executable directories, script files by
# extension, and the shell start-up files. Over-matching there
# costs nothing: it only means the body is scanned as ordinary
# command text, which is what every non-exempt command gets.
# The wider boundary stays the documented one -- this hook is a
# backstop against the everyday mistake, not a sandbox. A body
# built at runtime, or fetched, defeats any string matcher.
#
# Writes AT a protected path need no rule here: the sink line
# itself is never dropped, so cat >> ~/.ssh/authorized_keys is
# still judged by the normal SSH-key rules below.
#
# NOT fixed by this, on purpose: a taboo word quoted as an
# ARGUMENT, e.g. grep shutdown /var/log/syslog. segments() splits
# on quotes so that ssh host "shutdown -h now" is caught, and
# after that split the grep argument and the ssh payload are the
# same shape. Telling them apart needs a per-command list of
# which arguments are data, which is open-ended and would reopen
# issue #4. Rephrase the probe (grep 'shut[d]own') instead.
CMD_NOHEREDOC=$(printf '%s\n' "$CMD" | awk '
  BEGIN { q = sprintf("%c", 39) }
  NR == 1 {
    first = $0
    # A raw device target is never an ordinary file.
    if (first ~ /\/dev\//) { bad = 1; exit }
    # Targets that something later EXECUTES are not inert either.
    # Schedulers, init and unit locations, anything under /etc:
    if (first ~ /cron|systemd|init\.d|rc\.d|profile\.d|\/etc\//) {
      bad = 1; exit
    }
    # launchd jobs (macOS):
    if (first ~ /launchd|LaunchAgents|LaunchDaemons|\.plist/) {
      bad = 1; exit
    }
    # the executable directories -- /bin /sbin /usr/bin ~/bin ...:
    if (first ~ /\/s?bin\//) { bad = 1; exit }
    # a script file by extension:
    if (first ~ /\.(sh|bash|zsh|ksh|command|py|pl|rb)([ \t<]|$)/) {
      bad = 1; exit
    }
    # a shell start-up file, sourced on the next login:
    if (first ~ /\.(bashrc|bash_profile|bash_login|zshrc|zshenv|zprofile|zlogin|kshrc|cshrc|profile|login)([ \t<]|$)/) {
      bad = 1; exit
    }
    # Exactly one heredoc, so there is exactly one body to find.
    tmp = first; cnt = 0
    while (match(tmp, /<</)) { cnt++; tmp = substr(tmp, RSTART + 2) }
    if (cnt != 1) { bad = 1; exit }
    # The whole first line must be the data sink and nothing else:
    # no ; & | backtick $( ) that could smuggle a second command.
    # The target may arrive either way round -- cat writes through
    # a redirect (cat > f), tee takes it as a plain argument
    # (tee f) -- so both separators are allowed. The path token
    # itself still excludes every character that could start
    # another command, and < > stay out of it so the heredoc
    # operator can never be swallowed as a filename.
    p = "[\"" q "]?[^ \t;|&`$()<>\"" q "]+[\"" q "]?"
    shape = "^[ \t]*(cat|tee)([ \t]+-a)?(([ \t]*>>?[ \t]*|[ \t]+)" p ")?[ \t]*<<-?[ \t]*[\"" q "]?[A-Za-z_][A-Za-z0-9_]*[\"" q "]?[ \t]*$"
    if (first !~ shape) { bad = 1; exit }
    d = first
    sub(/^.*<<-?[ \t]*/, "", d)
    gsub(/["]/, "", d); gsub(q, "", d); sub(/[ \t]*$/, "", d)
    if (d !~ /^[A-Za-z_][A-Za-z0-9_]*$/) { bad = 1; exit }
    delim = d
    print first
    next
  }
  # Drop the body. Ending it on a whitespace-stripped match too is
  # deliberate: erring early only scans MORE text, never less.
  !ended {
    t = $0; sub(/^[ \t]+/, "", t)
    if ($0 == delim || t == delim) ended = 1
    next
  }
  { print }
  END { if (bad || !ended) exit 1 }
' 2>/dev/null)
GUARD_STRIP_STATUS=$?
# Only a clean parse of a recognized data sink may shrink the
# scanned text. Anything else keeps the full command string.
if [ "$GUARD_STRIP_STATUS" -eq 0 ] && [ -n "$CMD_NOHEREDOC" ]; then
  CMD="$CMD_NOHEREDOC"
fi

# The command string, plus one line per invocation it contains.
# Separators are ; & | quotes and newlines. Quotes count on
# purpose: to the shell, ssh -l root host "fdisk /dev/sda" is
# ONE invocation, but it carries a second command inside, and
# only splitting there puts fdisk in a segment that no longer
# holds ssh's -l. The FULL string stays in the list, so this can
# only ever block more, never less.
segments() {
  printf '%s\n' "$CMD"
  # The newline closes the last piece. Without it the piece ran
  # into the next line, and an exemption that must reach the end
  # of the invocation (bootlist's display form) never could.
  printf '%s\n' "$CMD" | tr ';&|"'"'"'\n' '\n'
  # Git Bash on Windows takes a quoted C:\Users\x\.ssh\id_rsa as a
  # path. With the backslashes as slashes the patterns below see
  # it; on Unix this can only add a match, never remove one.
  printf '%s\n' "$CMD" | tr '\\' '/'
}

hit() {
  segments | grep -Eq "$1"
}

# The command as the shell hands its words over: quotes and
# backslashes removed, split at ; & | and newlines. The shell
# joins ss""hd into sshd and drops the quotes around "-t", so a
# flag check that must see the real tokens reads this view.
# Removing a quote can only join words, never hide one: it adds
# matches.
# A backslash at the end of a line continues the word on the
# next one; those lines are joined first, or the split would cut
# bootlist -m normal -o \<newline>hdisk1 in two.
unquoted() {
  printf '%s\n' "$CMD" \
    | awk '{ while (sub(/\\$/, "")) { if ((getline n) > 0) $0 = $0 n; else break } print }' \
    | tr -d '"'"'"'\\' | tr ';&|' '\n\n\n'
}
hit_unq() {
  unquoted | grep -Eq "$1"
}

# Case-insensitive variant. diskutil accepts its verbs in any
# case, so eraseDisk and erasedisk are the same command.
hit_i() {
  segments | grep -Eiq "$1"
}

# A raw disk device, as opposed to /dev/null, /dev/stderr,
# /dev/shm or /dev/disk/by-id (all of which are ordinary and
# must stay usable).
# AIX: /dev/hdiskN falls under hd, its raw twin is /dev/rhdiskN.
DEV='/dev/(sd|vd|xvd|hd|nvme|mmcblk|nbd|loop|da|ada|nda|r?h?disk[0-9])'

# Any SSH key file, OR the directory that holds them. The
# directory belongs in here because re-permissioning or removing
# it reaches every key through the parent without ever naming a
# key: chown -R alice:alice /home/alice/.ssh hands the whole set
# to another owner, chmod 000 /root/.ssh hides it from sshd, and
# rm -rf ~/.ssh deletes it. The old pattern listed key FILENAMES
# only, so all three walked through while the spelled-out
# chmod 600 ~/.ssh/authorized_keys was denied. Same mistake as
# issues #5 and #6, one level up the path: the rule named the
# spelling of the target instead of the effect on it.
#
# Boundary, deliberately left open: a command that destroys an
# enclosing directory without naming .ssh at all (rm -rf /home/
# alice, a whole-filesystem operation) reaches the same effect
# and is NOT caught here. Closing it would mean treating every
# home directory as a key store. This is a backstop against the
# everyday mistake, not a sandbox.
#
# KEYPRIV additionally excludes a trailing .pub, so reading or
# copying a public key stays allowed while the private half does
# not. It stays filename-only on purpose: it guards a truncating
# redirect and ssh-keygen -f, neither of which is meaningful
# against a directory. KEYFILE is the same set without the
# trailing boundary, for writes_to below.
#
# Host keys and sshd_config do not always live in /etc/ssh:
# the OpenSSH port or package puts them in /usr/local/etc/ssh
# (FreeBSD; on OPNsense only sshd_config, see KEYDIR) or
# /opt/homebrew/etc/ssh. No pattern here
# is anchored on the left, so /etc/ssh matches every such prefix,
# and the rules must keep it that way instead of listing
# prefixes.
#
# KEYDIR adds an appliance key store (OPNsense: /conf/sshd) to
# .ssh; the rest of /conf is config and stays ordinary work.
# /etc/ssh itself is NOT a key store: it also holds ssh_config
# and moduli, so rm -rf /etc/ssh or chmod -R on it is left open,
# on the same terms as the home directory above.
HOSTKEY='(/etc/ssh|/conf/sshd)/ssh_host_'
KEYDIR='(\.ssh|/conf/sshd)'
KEY="($HOSTKEY|authorized_keys|$KEYDIR"'(/|[^[:alnum:]_.-]|$))'
KEYFILE="($HOSTKEY"'[[:alnum:]_-]*key|\.ssh/id_[[:alnum:]_-]+|authorized_keys)'
KEYPRIV="$KEYFILE"'([^.[:alnum:]]|$)'

# sshd's config: sshd_config, its drop-in directory, and a file
# an appliance merges into it when it regenerates the config
# (pfSense appends /etc/sshd_extra). The .d suffix is optional,
# so a plain hit on SSHD also finds the bare file. Then the
# editors that rewrite a file in place.
SSHD='/etc/(ssh/sshd_config(\.d(/[[:alnum:]_.-]*)?)?|sshd_extra)'
EDITOR='(vi|vim|nvim|nano|emacs|ed|ex)'

# Backed here where a write locks people out or cannot be
# undone; deliberate work on them runs with the operator's
# JOLITZ_GUARD_DISABLE. sudoers and its drop-ins, unanchored on
# the left like
# SSHD so /usr/local/etc/sudoers (FreeBSD) matches; the account
# files, where a direct write bypasses every check useradd and
# passwd make; and the firmware variables, where a deleted entry
# has bricked boards. All three stay readable.
SUDOERS='/etc/sudoers(\.d(/[[:alnum:]_.-]*)?)?'
# AIX keeps the hashes and the account attributes under
# /etc/security, as files without an extension. The boundary
# keeps Linux PAM's /etc/security/limits.conf and group.conf out.
ACCOUNTS='/etc/(passwd|shadow|gshadow|group|master\.passwd|s?pwd\.db|security/(passwd|group|user|limits)([^[:alnum:]_.-]|$))'
EFIVARS='/sys/firmware/efi/efivars'

# The server lock. The machine's owner writes it to keep jolitz
# hands-off; a jolitz that could flip it to true or delete it
# would make it worthless. Matched by its file name alone, in any
# case: the full path misses /ETC/JOLITZ.CONF on macOS, /etc//
# and /etc/./, and a relative jolitz.conf after cd /etc. A write
# to any file of that name is denied with it. Not closable: a
# glob (/etc/jolitz.c*) or a name built at run time still gets
# through, so the written rule stays the first line of defence.
# Reading it stays allowed.
LOCKFILE='[Jj][Oo][Ll][Ii][Tt][Zz]\.[Cc][Oo][Nn][Ff]'

# The guard's own files. A model that rewrites the hook, or adds
# an env block with the override to a settings file, has
# disarmed the guard as surely as an inline assignment. Matched
# in any project, not only this one: the active checkout is
# whichever one the session started in.
SELF='\.claude/(settings(\.local)?\.json|hooks/)'

# An assignment of the override, in shell (VAR=1), JSON
# ("VAR": "1") or jq (.env.VAR="1") form. A bare mention, as in
# this file's documentation, stays allowed. The backslash is for
# file mode, which scans the JSON input, where a quote is \".
DISARM='JOLITZ_GUARD_DISABLE[\"'\'']*[[:space:]]*(=|:[^-])'

# A general-purpose language runtime. See the interpreter section
# at the bottom for why this list, and not a list of the ways
# those runtimes spell a write.
INTERP='(^|[^[:alnum:]_.-])(python[0-9.]*|perl|ruby|node|nodejs|deno|bun|php[0-9.]*|lua[0-9.]*|tclsh|osascript|Rscript|julia|elixir|escript|erl|[gmn]?awk)([^[:alnum:]_.-]|$)'
# The same set without awk, for the account files: the security
# skill reads /etc/passwd and /etc/shadow with awk -F:, and awk
# writes a file only through an explicit > that the redirect
# rules see anyway.
INTERP_NOAWK='(^|[^[:alnum:]_.-])(python[0-9.]*|perl|ruby|node|nodejs|deno|bun|php[0-9.]*|lua[0-9.]*|tclsh|osascript|Rscript|julia|elixir|escript|erl)([^[:alnum:]_.-]|$)'

# True when command $1 occurs somewhere WITHOUT its read-only
# exemption $2 applying to that occurrence. Two conditions make
# an exemption count: it must sit in the same segment as the
# command, and it must FOLLOW it. Otherwise a flag belonging to
# a wrapper disarms the taboo, which is what issue #4 reported:
# ssh -l root host "fdisk /dev/sda" and lsblk -l && fdisk /dev/sdb
# were both waved through because a bare -l existed anywhere.
hit_without() { _hit_without segments "$1" "$2"; }
hit_without_unq() { _hit_without unquoted "$1" "$2"; }
_hit_without() {
  HW_SRC=$1; shift
  "$HW_SRC" | grep -Eq "$1" || return 1
  # The command IS present. From here on the only question is
  # whether the exemption belongs to it, so every failure path
  # below must deny. If this awk cannot evaluate POSIX classes,
  # we cannot prove the exemption applies: block.
  printf 'x' | awk '{ exit(($0 ~ /[[:alnum:]]/) ? 0 : 1) }' \
    2>/dev/null || return 0
  "$HW_SRC" | awk -v cmd="$1" -v exempt="$2" '
    {
      line = $0
      while (match(line, cmd)) {
        if (RLENGTH <= 0) break
        if (substr(line, RSTART) !~ exempt) { bare = 1; exit }
        line = substr(line, RSTART + RLENGTH)
      }
    }
    END { exit(bare ? 0 : 1) }' 2>/dev/null
  GUARD_AWK_STATUS=$?
  # Only a clean "every occurrence is exempt" (exit 1) allows.
  # Any other status is an awk failure, and that must not pass.
  [ "$GUARD_AWK_STATUS" -eq 1 ] && return 1
  return 0
}

# Opt-in denial log for the operator: JOLITZ_GUARD_LOG=1
# in the environment Claude Code starts with. One line per
# denial in ~/.cache/jolitz/guard.log (time, mode, reason,
# the first 200 characters of the command or the edited
# path). It shows which blocks get in the way of real work,
# so false positives reach the test matrix. A command line
# can carry a secret, hence opt-in and mode 0600. Writing
# it never changes the decision: every failure is ignored
# and deny() denies either way.
guard_log() {
  [ "${JOLITZ_GUARD_LOG:-}" = "1" ] || return 0
  (
    umask 077
    d="$HOME/.cache/jolitz"
    mkdir -p "$d" && [ -d "$d" ] && [ ! -L "$d/guard.log" ] || exit 0
    s=$(printf '%s' "$LOG_SUBJECT" | tr '\t\n\r' '   ' | cut -c1-200)
    r=$(printf '%s' "$1" | tr '\t\n\r' '   ')
    printf '%s\t%s\t%s\t%s\n' "$(date -u +%Y-%m-%dT%H:%M:%SZ)" \
      "${GUARD_MODE:-bash}" "$r" "$s" >> "$d/guard.log"
  ) >/dev/null 2>&1 || true
}

deny() {
  guard_log "$1"
  # JSON decision on stdout; blocks in all permission modes.
  # Reasons must stay plain ASCII without quotes/backslashes.
  printf '{"hookSpecificOutput":{"hookEventName":"PreToolUse",'
  printf '"permissionDecision":"deny",'
  printf '"permissionDecisionReason":"jolitz guard: %s ' "$1"
  # Without jq every rule ran on the raw hook input, so this
  # denial may be one jq would have avoided. Say so.
  command -v jq >/dev/null 2>&1 \
    || printf '(jq is missing on this workstation, so the guard read the command only roughly; installing jq avoids such denials) '
  printf '(CLAUDE.md - Critical Safety Rules). Blocked in all '
  printf 'permission modes. Explain this to the user; do not '
  printf 'rephrase the command to evade the guard."}}\n'
  exit 0
}

# --- File-edit tools (file mode) ------------------------------
# Edit, Write and NotebookEdit write a file with no command line
# at all, so none of the rules below ever sees them: in local
# mode Write could replace /etc/ssh/sshd_config or authorized_keys
# unchallenged. settings.json registers those tools with the
# argument "file", and the guard then judges the TARGET path
# alone. The text being written is data and is not scanned, which
# is what lets a memory file name every protected path
# (rules/server-memory.md).
#
# Same set as the Bash rules: sshd's config, a key file, a raw
# disk device. ~/.ssh/config and known_hosts stay editable, as
# they are for tee and a redirect.
#
# The device pattern is anchored, unlike DEV: unanchored it would
# deny every file under a directory called dev (~/dev/sdk/x.md).
# The quote anchors it in the degraded scan below, where the path
# is still inside its JSON string.
#
# A path has more than one spelling, and the kernel resolves all
# of them before the write: //, /./ and x/../ anywhere in it, and
# on a filesystem that ignores case (macOS) any capitalization.
# So the target is matched without regard to case, and both as
# given and with those three forms folded away. Every path key in
# the input is judged, not the first one found.
#
# Case folding is not ASCII-only, and APFS folds in full: the
# long s and the Kelvin sign open s and k, a sharp s opens ss,
# and the ligatures U+FB00 to U+FB06 open ff, fi, fl, ffi, ffl
# and st (measured on APFS: [ -e ] on an fi ligature finds
# config). The thirteen mapped below are every entry of
# CaseFolding.txt, Unicode 18, that folds onto ASCII letters
# alone, counted with a script and not from memory. The set is
# NOT closed: U+1DF95 joined it late, so a newer Unicode needs a
# recount. Every protected name is plain ASCII, so the target is
# also matched with them spelled out, and with every other
# non-ASCII byte removed, because HFS+ skips zero-width characters
# when it compares names.
#
# A relative target is resolved against the session's cwd, which
# the hook input carries: the bare name sshd_config is the
# protected file in /etc/ssh and an ordinary one anywhere else.
#
# When jq cannot name the target, the whole input is scanned
# instead. That also blocks a note ABOUT a protected path, and it
# must: an unproven target is a protected one. JSON can spell a
# slash or any character as a backslash escape, which only a JSON
# parser undoes, so such an escape is denied there as well, and
# so is a relative target, which cannot be resolved.
#
# Boundary, left open: a symlink to a protected file under another
# name, and any other tool that writes files (an MCP server). The
# set of writers cannot be closed from here.
if [ "${1:-}" = "file" ]; then
  TARGET=""
  if command -v jq >/dev/null 2>&1; then
    TARGET=$(printf '%s' "$INPUT" | jq -r --arg pwd "$PWD" '
      ((.cwd // $pwd) | tostring) as $c
      | .tool_input | (.file_path, .notebook_path)
      | select(. != null) | tostring
      | if startswith("/") then . else ., $c + "/" + . end' \
      2>/dev/null) || TARGET=""
    # Windows hands over C:\Users\x\.claude\settings.json. As
    # C:/Users/x/.claude/settings.json the patterns below see it.
    # A path without a leading slash is checked both as given (a
    # Windows drive path) and below the cwd (on Unix c:/../x is
    # relative); a match in either line denies.
    TARGET=$(printf '%s' "$TARGET" | tr '\\' '/')
  fi
  # The log names the path, never the text being written.
  GUARD_MODE=file
  LOG_SUBJECT=${TARGET:-"(target unreadable)"}
  if [ -z "$TARGET" ]; then
    TARGET="$RAW"
    if printf '%s\n' "$TARGET" | LC_ALL=C grep -Eq \
      '\\(/|u[0-9a-fA-F]{4})|"(file|notebook)_path"[[:space:]]*:[[:space:]]*"[^/]'
    then
      deny "the target of this file edit could not be read: jq is \
missing or failed, and the path is relative or may be hidden \
behind a JSON escape"
    fi
  fi
  # U+FB00..FB06 share the prefix EF AC and differ in the last
  # byte; the three-letter ligatures go first only for clarity.
  LIG=$(printf '\357\254')
  MAPPED=$(printf '%s\n' "$TARGET" | LC_ALL=C sed \
    -e "s/$(printf '\305\277')/s/g" \
    -e "s/$(printf '\342\204\252')/k/g" \
    -e "s/$(printf '\303\237')/ss/g" \
    -e "s/$(printf '\341\272\236')/ss/g" \
    -e "s/$(printf '\360\235\276\225')/ss/g" \
    -e "s/$(printf '\304\260')/i/g" \
    -e "s/$LIG$(printf '\203')/ffi/g" \
    -e "s/$LIG$(printf '\204')/ffl/g" \
    -e "s/$LIG$(printf '\200')/ff/g" \
    -e "s/$LIG$(printf '\201')/fi/g" \
    -e "s/$LIG$(printf '\202')/fl/g" \
    -e "s/$LIG$(printf '\205')/st/g" \
    -e "s/$LIG$(printf '\206')/st/g" 2>/dev/null) || MAPPED=""
  ASCII=$(printf '%s\n' "$MAPPED" \
    | LC_ALL=C tr -d '\200-\377' 2>/dev/null) || ASCII=""
  FOLDED=$(printf '%s\n%s\n' "$MAPPED" "$ASCII" \
    | LC_ALL=C sed -E -e 's#/+#/#g' \
      -e ':a' -e 's#/\./#/#g' -e 'ta' \
      -e ':b' -e 's#/[^/]+/\.\./#/#' -e 'tb' 2>/dev/null) || FOLDED=""
  if [ -z "$MAPPED" ] || [ -z "$ASCII" ] || [ -z "$FOLDED" ]; then
    deny "the target path of this file edit could not be \
normalised, so it cannot be shown to be an ordinary file"
  fi
  FILEDEV="(^|\"|/\\.\\.?)/+${DEV#/}"
  target_hit() {
    printf '%s\n%s\n%s\n%s\n' "$TARGET" "$MAPPED" "$ASCII" "$FOLDED" \
      | LC_ALL=C grep -Eiq "$1"
  }
  # Always on: the server lock, and content that sets the
  # override. Unlike everything else in file mode, the content
  # is scanned here, because an env block or an export line
  # disarms the guard whatever file it lands in.
  if target_hit "$LOCKFILE"; then
    deny "/etc/jolitz.conf is the server lock; only the \
machine's owner changes it, never jolitz"
  fi
  if printf '%s' "$INPUT" | LC_ALL=C grep -Eq "$DISARM"; then
    deny "this edit writes an assignment of JOLITZ_GUARD_DISABLE; \
only the operator sets it, before the session"
  fi
  [ "$GUARD_OFF" -eq 1 ] && exit 0
  if target_hit "$SELF"; then
    deny "this file registers or implements the guard; the \
operator edits it by hand"
  fi
  if target_hit "$SSHD"; then
    deny "modifying sshd_config or a file merged into it is \
never allowed, with a file-edit tool as little as with a command"
  fi
  if target_hit "$KEYFILE"; then
    deny "writing into an SSH key file replaces the keys there, \
with a file-edit tool as much as with a command"
  fi
  if target_hit "$FILEDEV"; then
    deny "writing onto a raw disk device overwrites its content \
and partition table"
  fi
  if target_hit "$SUDOERS"; then
    deny "writing sudoers or a sudoers.d file grants or revokes \
root rights; edit it with visudo -f and the operator's \
JOLITZ_GUARD_DISABLE"
  fi
  if target_hit "$ACCOUNTS"; then
    deny "writing an account file directly bypasses every check \
useradd and passwd make; use useradd, usermod or passwd"
  fi
  if target_hit "$EFIVARS"; then
    deny "writing a firmware variable can brick the board; boot \
work runs with the operator's JOLITZ_GUARD_DISABLE \
(rules/efi-boot.md)"
  fi
  exit 0
fi

# The model must not disarm the guard from inside a command.
# Matches the assignment form only — merely mentioning the
# variable name (docs, grep) is fine. Note: heredocs flow
# through the command string too, so writing the literal
# assignment into a file or commit message also triggers
# this; phrase such text without the equals sign.
if hit "$DISARM"; then
  deny "an assignment of JOLITZ_GUARD_DISABLE is not allowed - \
the operator must export it before launching the session"
fi

# --- Attack tools (rules/ethics.md) ---------------------------
# jolitz is not an attack tool. This denies the password and
# key crackers, the network attack suites and the login brute-
# forcers by name, both run and installed. Like every other
# block here it names a KNOWN set, not the effect -- a renamed
# or hand-built cracker walks past it, and the ethics rule in
# the model layer is the real boundary. This is the mechanical
# backstop against the obvious case, in every permission mode.
#
# Scoped to the program token with a boundary on either side.
# Like the taboo words elsewhere, a cracker name quoted as an
# ARGUMENT is matched too: `apt-cache show hashcat` is denied
# although it only reads. The names are distinctive, so this
# collides with ordinary work far less than `shutdown` does;
# rephrase such a probe (the pkg-search case is rare). `john`
# is the one name that is also a username, so it is matched
# only with its own flags or an .rule/.pot/wordlist operand,
# never bare.
ATTACK='hashcat|hydra|medusa|ncrack|patator|crowbar|thc-hydra'
ATTACK="$ATTACK"'|aircrack-ng|airmon-ng|aireplay-ng|reaver|bully'
ATTACK="$ATTACK"'|ophcrack|rainbowcrack|hashpump'
ATTACK="$ATTACK"'|metasploit|msfconsole|msfvenom|sqlmap'
ATTACK="$ATTACK"'|responder|crackmapexec|netexec|nxc|hashcat-utils'
if hit "(^|[^[:alnum:]_.-])($ATTACK)([^[:alnum:]_.-]|\$)"; then
  deny "this is a password/key cracker or an attack tool; \
jolitz does not recover passwords or attack systems, on any \
host (rules/ethics.md)"
fi
# john the ripper: only when it looks like the tool, not the name
if hit '(^|[^[:alnum:]_.-])john([[:space:]]+-|[[:space:]]+[^ ]*\.(pot|rec|rule|lst|txt|conf)|[[:space:]]+--)'; then
  deny "john the ripper recovers passwords; jolitz does not do \
this on any host (rules/ethics.md)"
fi
# Installing any of them is blocked too, whatever the manager.
if hit '(apt-get|apt|aptitude|dnf|yum|zypper|pkg|brew|snap|pip3?|pipx|cargo|go)[[:space:]]+(-[^ ]+[[:space:]]+)*(install|add|-S|in)([[:space:]]|$)' \
  && hit "(^|[^[:alnum:]_.-])($ATTACK|john|john-the-ripper|johntheripper)([^[:alnum:]_.-]|\$)"; then
  deny "installing a password cracker or attack tool; jolitz \
does not do this on any host (rules/ethics.md)"
fi

# True when the command writes INTO a path matching $1 (a regex
# without a trailing boundary) through a tool that names its
# target: tee or sponge, dd of=, an output flag (curl -o, wget -O,
# sort -o, openssl -out), or the destination of cp, rsync or scp.
# The same shapes reach a disk device, sshd_config and a key.
#
# A copy is judged by its DESTINATION, the last operand of its
# invocation, because the common legitimate forms name the
# protected path as the source: scp -i ~/.ssh/id_ed25519, a backup
# of authorized_keys, cp /dev/sda disk.img. Trailing options,
# redirects, a closing parenthesis or backtick and a comment are
# skipped while looking for that operand; cp -t names it up front.
# An = right before an output path is excluded on purpose:
# ssh -o IdentityFile=~/.ssh/id_ed25519 only reads the key.
#
# The set of writers cannot be closed (an archive unpacked into
# place, a git checkout, any tool with an output flag of its own),
# so this stays a backstop, not a sandbox.
#
# END includes < and >: a redirect glued to the path
# (tee /etc/ssh/sshd_config<<EOF) still ends it.
#
# The output flag may close a short-flag cluster (curl -fsSLo,
# wget -qO): the letters before it are skipped. Accepted false
# positive: a cluster that merely ends in o or O (ls -lo) next to
# a protected path.
END="([\"'[:space:];|&)\`<>]|\$)"
ENDARG="[\"']?([[:space:]]+(-[^[:space:]]*|[0-9]*[<>]+&?([[:space:]]*[^[:space:]]+)?))*[[:space:])\`]*(#.*)?\$"
writes_to() {
  hit "(^|[^[:alnum:]_.-])(tee|sponge)[[:space:]]([^;|&<>]*[[:space:]])?[^[:space:];|&]*$1$END" \
    || hit "(^|[[:space:]])of=[^[:space:]]*$1$END" \
    || hit "(^|[[:space:]])(-[[:alpha:]]*[oO]|-out|--output(-document)?)([[:space:]]+|=)?[^[:space:]=]*$1$END" \
    || hit "(^|[^[:alnum:]_.-])(cp|rsync|scp)[[:space:]]([^;&|]*[[:space:]])?[^[:space:]]*$1$ENDARG" \
    || hit "(^|[^[:alnum:]_.-])cp[[:space:]]([^;&|]*[[:space:]])?(-[[:alpha:]]*t[[:space:]]*|--target-directory[=[:space:]])[^[:space:]]*$1$END"
}

# CLOBBER: tools that delete, move, re-permission or rewrite a
# file named on their command line. The lock, the guard's own
# files and the key, sshd_config, sudoers and account rules below
# all use it.
CLOBBER='rm|shred|unlink|truncate|mv|chmod|chown|install|ln|setfacl|patch'

# ONTO: a redirect onto the path that follows it, in all three
# POSIX spellings: > and >> and the clobber form >|. segments()
# splits on |, so >| is only whole in the full string, which the
# list always keeps.
ONTO='>[>|]?[[:space:]]*["'\'']?[^[:space:];|&]*'

# INPLACE: sed or perl rewriting a file in place. The flag stands
# alone (-i), closes a cluster (-Ei, -ri, -ni, perl -pi), carries a
# suffix (-i.bak) or is spelled out; GNU getopt takes any unique
# prefix of --in-place, and --i is one. The rules pair it with the
# tool, as before.
INPLACE='(^|[[:space:]])(-[[:alpha:]]*i|--i[-a-z]*)'

# --- The server lock (always on) ------------------------------
# Same write set as sudoers below (a redirect, sed -i, an editor,
# a delete, move or re-permission, a copy or tee onto it), plus
# find -delete, which deletes by a name it never spells as a
# path. Moving or deleting it lifts the lock as surely as writing
# true. Checked before the override, which never lifts it.
if hit "$LOCKFILE"; then
  if hit "$ONTO$LOCKFILE" \
    || { hit '(^|[^[:alnum:]_-])(sed|perl)([^[:alnum:]_-]|$)' \
         && hit "$INPLACE"; } \
    || hit "(^|[^[:alnum:]_-])$EDITOR([^[:alnum:]_-]|\$)" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp)([^[:alnum:]_-]|\$)" \
    || hit '(^|[[:space:]])-delete([[:space:]]|$)' \
    || writes_to "$LOCKFILE"
  then
    deny "/etc/jolitz.conf is the server lock; only the \
machine's owner changes it, never jolitz (cat and grep read it)"
  fi
fi

if hit "$INTERP" && hit "$LOCKFILE"; then
  deny "an interpreter with /etc/jolitz.conf on its command \
line can lift the server lock - read it with cat or grep instead"
fi

# --- The operator's override ----------------------------------
# Everything above holds whatever JOLITZ_GUARD_DISABLE says.
# Everything below is a taboo the operator can lift for a
# session, by exporting it before the launch.
[ "$GUARD_OFF" -eq 1 ] && exit 0

# --- The guard's own files ------------------------------------
# Rewriting the hook, or giving a settings file an env block,
# disarms everything below.
#
# A list of the ways to write cannot be closed: every review of
# the earlier deny list found one more (ditto, curl -sSo, git
# checkout-index, a glob, a cd out of skills/). So this section
# is an ALLOW list. A command is in scope when it mentions .claude
# in any case or spelling, names settings.local.json, or runs
# with its cwd anywhere inside a .claude tree. In scope it may
# only read: every invocation must be one of the read-only tools
# below, with no redirect (except onto /dev/null or a
# descriptor) and no command or process substitution. The one
# exception is the macOS cleanup scanner, in exactly the two
# forms its skill prescribes. Everything else that touches
# .claude is denied; the operator runs it by hand.
SELF_CWD=""
if command -v jq >/dev/null 2>&1; then
  SELF_CWD=$(printf '%s' "$INPUT" | jq -r '.cwd // empty' 2>/dev/null) \
    || SELF_CWD=""
fi
LC_CWD=$(printf '%s' "$SELF_CWD" \
  | LC_ALL=C tr '[:upper:]\\' '[:lower:]/')
LC_CMD=$(printf '%s' "$CMD" | LC_ALL=C tr '[:upper:]' '[:lower:]')
IN_CLAUDE=0
CWD_IN_CLAUDE=0
case "$LC_CWD" in
  */.claude|*/.claude/*) IN_CLAUDE=1; CWD_IN_CLAUDE=1 ;;
esac
case "$LC_CMD" in
  *.claude*|*settings.local.json*) IN_CLAUDE=1 ;;
esac
# HFS+ skips zero-width characters and APFS folds a long s onto
# s, so .clau<U+200B>de and ſettings.local.json reach the same
# files. The shell drops quotes and backslashes (.cla""ude). With
# all of those removed, either name in the rest puts the command
# in scope. An umlaut elsewhere does not.
UQ_CMD=$(printf '%s' "$LC_CMD" | LC_ALL=C tr -d '\200-\377"'\''\\')
case $UQ_CMD in
  *.claude*|*ettings.local.json*) IN_CLAUDE=1 ;;
esac
# A glob, brace or expansion in a path component can resolve to
# .claude when the literal text before it still could: .c*,
# .?laude, .{c,}laude, .$x, {.cl,}aude; .bash_profile.$(date)
# cannot. Comments are skipped, as the shell skips them.
# Only unquoted ones expand: inside quotes and after a backslash
# they become _, so sed "s/ .*//" stays out of scope while
# ".cl"* does not. A $ expands inside double quotes too, and an
# unclosed quote counts as in scope. Accepted false positive:
# such a command may only read. Not closable: a name built at run
# time, in a variable (d=.cla; ${d}ude) or a command substitution
# ($(printf .cla)ude). Every $( cannot count, or cp f
# f.bak.$(date +%F) could not run. The guard is a backstop.
GLOBBED=$(printf '%s\n' "$LC_CMD" | LC_ALL=C awk '
  BEGIN { q = "" }
  {
    out = ""
    for (i = 1; i <= length($0); i++) {
      ch = substr($0, i, 1)
      if (q == "" && ch == "#" \
        && (i == 1 || index(" \t;&|(", substr($0, i - 1, 1)))) break
      # An escaped character is literal: still part of the name,
      # but no longer a glob.
      if ((q == "" && ch == "\\") \
        || (q == "\"" && ch == "\\" && index("\"\\$`", substr($0, i + 1, 1)))) {
        i++; c = substr($0, i, 1)
        out = out (index("*?[]{$", c) ? "_" : c); continue
      }
      if (q == "" && (ch == "\"" || ch == "\047")) { q = ch; continue }
      if (q != "" && ch == q) { q = ""; continue }
      if (q == "\047" && index("*?[]{$", ch)) ch = "_"
      if (q == "\"" && index("*?[]{", ch)) ch = "_"
      out = out ch
    }
    print out
  }
  END { if (q != "") print " .*" }')
if printf '%s\n' "$GLOBBED" \
  | LC_ALL=C grep -Eq \
    '(^|[[:space:]/=<>:])(\.(c(l(a(u(de?)?)?)?)?)?[][*?{$]|\{[^[:space:]/]*\.)'; then
  IN_CLAUDE=1
fi
# ANSI-C quoting with a hex, octal or Unicode escape spells any
# name ($'\x2eclaude'). $'\t' and friends stay out of scope.
if printf '%s\n' "$CMD" \
  | LC_ALL=C grep -Eq "\\\$'[^']*\\\\([xuU]|[0-7])"; then
  IN_CLAUDE=1
fi
if [ "$IN_CLAUDE" -eq 1 ]; then
  # Backslash continuations folded, blanks squeezed. Any newline
  # left separates a second command, so the scanner forms below
  # only match a single line. With the cwd inside .claude, a bare
  # redirect target is a guard file, so the redirect form is out.
  NL=$(printf '\nx'); NL=${NL%x}
  FLAT=$(printf '%s' "$CMD" \
    | awk '{ if (sub(/\\$/, "")) printf "%s ", $0; else print }' \
    | tr '\t' ' ' | tr -s ' ' | sed -e 's/^ //' -e 's/ $//')
  SCAN='\.claude/skills/jolitz-macos-cleanup/scripts/scan\.py'
  SARG='( ([A-Za-z0-9_.@:+-]+|"[A-Za-z0-9 _.@:+-]+"))*'
  # Only the options CLAUDE.md prescribes, so no ProxyCommand,
  # LocalCommand, -F or -S runs or reads anything else.
  SSHOPT='( -o (BatchMode|ConnectTimeout|ControlMaster|ControlPath|ControlPersist|ServerAliveInterval|ServerAliveCountMax|Port)=[A-Za-z0-9_.%~/:@+-]+)* [A-Za-z0-9_][A-Za-z0-9_.@:-]*'
  SOUT='( > [A-Za-z0-9_.-]+)?'
  [ "$CWD_IN_CLAUDE" -eq 1 ] && SOUT=''
  case "$FLAT" in
    *"$NL"*|*"$(printf '\r')"*) FLAT='' ;;
  esac
  if [ -n "$FLAT" ] && { printf '%s\n' "$FLAT" | LC_ALL=C grep -Eq \
       "^(/usr/bin/)?python3 $SCAN$SARG$SOUT\$" \
    || printf '%s\n' "$FLAT" | LC_ALL=C grep -Eq \
       "^ssh$SSHOPT /usr/bin/python3 -$SARG < $SCAN\$"; }
  then
    :
  else
    case "$CMD" in
      *'$('*|*'`'*|*'<('*|*'>('*)
        deny "a command or process substitution next to the \
guard's own files hides what it writes - read .claude with cat, \
grep or ls in a call of their own" ;;
    esac
    REDIR=$(printf '%s\n' "$CMD" | sed -E \
      -e 's#[0-9]*>>?[[:space:]]*/dev/null##g' -e 's#[0-9]*>&[0-9-]##g')
    if printf '%s\n' "$REDIR" | grep -q '>'; then
      deny "a redirect in a command that touches .claude can \
overwrite the guard's own files - read them with cat, grep or \
ls in a call of their own"
    fi
    READONLY='^(cd|pushd|popd|ls|cat|head|tail|wc|stat|file|grep|egrep|fgrep|diff|cmp|readlink|realpath|sha256sum|shasum|md5|md5sum|echo|printf|true|test|\[|jq|less|more)( |$)'
    READGIT='^git( -C [^ ]+)? (log|show|diff|status|blame|ls-files|grep|rev-parse|cat-file)( |$)'
    BAD=$(printf '%s\n' "$REDIR" | tr ';&|' '\n\n\n' \
      | sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//' \
      | while IFS= read -r seg; do
          [ -n "$seg" ] || continue
          # The flags as the shell sees them, quotes removed
          # (-"delete", --"output"). A brace or $ in a find or git
          # segment can build a flag (-{delete,true}), so it does
          # not pass.
          uq=$(printf "%s" "$seg" | tr -d "\"'\\\\")
          # git log/diff/show --output writes a file, grep -O and
          # --ext-diff/--textconv run a program. git takes any
          # unique prefix of a long option, so the prefixes count
          # (--exit-code and --text with them). Not closable: a
          # program set in git config (diff.external, a textconv
          # driver) runs without a flag; the guard is a backstop.
          if printf '%s\n' "$seg" | grep -Eq "$READONLY" \
            || { printf '%s\n' "$seg" | grep -Eq "$READGIT" \
              && ! printf '%s\n' "$uq" | grep -Eq \
                '[{$]|(^| )-[A-Za-z]*O|(^| )--(op|ou|ex|te)[-a-z]*(=| |$)'; }
          then
            continue
          fi
          if printf '%s\n' "$seg" | grep -Eq '^find( |$)' \
            && ! printf '%s\n' "$uq" \
              | grep -Eq '[{$]|(^| )-(delete|exec|execdir|ok|okdir|fprint0?|fprintf|fls)( |$)'
          then
            continue
          fi
          printf '%s\n' "$seg"
        done)
    if [ -n "$BAD" ]; then
      deny "a command that touches .claude may only read it (cat, \
grep, ls, head, find without -exec or -delete, git log/show/diff) \
- the guard's own files are edited by the operator by hand"
    fi
  fi
fi

# --- A second agent session ------------------------------------
# claude -p (or --print) and opencode -p start another session,
# and one started from another cwd runs without this hook;
# --settings, --permission-mode and --dangerously-skip-permissions
# give that session its permissions outright. jolitz never starts
# one of its own: the operator schedules claude -p from cron
# (rules/scheduled-housekeeping.md). --version and --help pass.
# Accepted false positive: the same flags as data (grep claude -p).
if hit '(^|[^[:alnum:]_.-])(claude|opencode)[[:space:]]+([^;&|]*[[:space:]])?(-p|--print|--settings|--permission-mode|--dangerously-skip-permissions)([[:space:]=]|$)'
then
  deny "starting a second agent session, or one with its own \
permissions, runs outside this guard; the operator starts \
sessions"
fi

# --- The SSH service ------------------------------------------
# Stopping, disabling or masking sshd closes the door the next
# session comes in through, like removing its port (CLAUDE.md).
# systemctl stop/disable/mask on ssh or sshd (.service, .socket),
# service and rc-service stop (FreeBSD onestop too), an init.d
# or rc.d script, launchctl unload/disable/bootout of
# com.openssh.sshd or ssh.plist (macOS). status, restart and
# reload stay ordinary work: a restart asks first
# (rules/service-reload.md). AIX runs sshd under the System
# Resource Controller: stopsrc -s sshd, -g ssh, -g tcpip (the
# whole network) and -a (every subsystem) close the same door;
# lssrc reads, startsrc and refresh stay ordinary work. Open, on
# purpose: pkill sshd, stopsrc -p with sshd's PID (a number says
# nothing about the subsystem) and a firewall rule, which the
# model layer weighs.
SSHSVC='(ssh|sshd)(\.service|\.socket)?'
if hit "(^|[^[:alnum:]_.-])systemctl([[:space:]]+-[^[:space:]]+)*[[:space:]]+(stop|disable|mask)([[:space:]]+[^[:space:];&|]+)*[[:space:]]+$SSHSVC([^[:alnum:]_.-]|\$)" \
  || hit '((^|[^[:alnum:]_.-])(service|rc-service|invoke-rc\.d)[[:space:]]+|/etc/(init\.d|rc\.d)/)(ssh|sshd)[[:space:]]+(one)?stop([^[:alnum:]_-]|$)' \
  || hit '(^|[^[:alnum:]_.-])launchctl[[:space:]]+(unload|disable|bootout|remove)[[:space:]]+[^;&|]*(com\.openssh\.sshd|/ssh\.plist)' \
  || hit_unq '(^|[^[:alnum:]_.-])stopsrc[[:space:]]+([^;&|]*[[:space:]])?(-[[:alpha:]]*a([[:space:]]|$)|-[[:alpha:]]*s[[:space:]]*sshd([^[:alnum:]_.-]|$)|-[[:alpha:]]*g[[:space:]]*(ssh|tcpip)([^[:alnum:]_.-]|$))'
then
  deny "stopping, disabling or masking sshd cuts off the next \
login (restart and reload are ordinary work that asks first)"
fi

# --- Power off ------------------------------------------------
# AIX fasthalt is halt without the shutdown scripts.
if hit '(^|[^[:alnum:]_-])(fast)?halt([^[:alnum:]_-]|$)' \
  || hit '(^|[^[:alnum:]_-])poweroff([^[:alnum:]_-]|$)'; then
  deny "halt/poweroff never runs without explicit user request"
fi
if hit '(^|[^[:alnum:]_.-])(tel)?init[[:space:]]+0([^0-9]|$)'; then
  deny "init 0 powers off the server"
fi
# echo o > /proc/sysrq-trigger cuts the power instantly, and b
# resets without syncing. Nothing reads this file, so any
# mention of it is a write.
if hit 'sysrq-trigger'; then
  deny "sysrq-trigger powers off or resets the server without \
shutting anything down cleanly"
fi
# AIX: shutdown without flags halts and, where the power supply
# allows, powers off; -Fr (fast) is its everyday reboot, so -r
# counts together with F. -t restarts at a later time only after
# shutting down now: the host is gone until then, so -t denies
# before the -r exemption is looked at. The flag may also end at
# the closing quote of ssh host "shutdown -r".
if hit_unq '(^|[^[:alnum:]_-])shutdown[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*t([[:space:]]|$|[0-9])'; then
  deny "shutdown -t shuts the server down now and brings it back \
only at the given time"
fi
if hit_without '(^|[^[:alnum:]_-])shutdown([^[:alnum:]_-]|$)' \
  '(^|[[:space:]])-(r|c|Fr|rF)([[:space:]"'\'']|$)'; then
  deny "shutdown without -r powers off the server (reboots \
use shutdown -r; -c cancels)"
fi

# --- Sleep ----------------------------------------------------
# A sleeping server is as unreachable as one that is off, and
# nobody stands at it to wake it. systemd's sleep verbs and the
# targets behind them (start or isolate; status reads them),
# FreeBSD acpiconf -s 3/4/5 (S5 is soft-off) and zzz, macOS pmset
# sleepnow, rtcwake in every mode but show, and a write onto
# /sys/power/state, which is read for the list of states and
# written for nothing else. macOS shutdown -s falls under the
# shutdown rule above.
SLEEP='(suspend|hibernate|hybrid-sleep|suspend-then-hibernate|sleep)'
if hit "(^|[^[:alnum:]_.-])systemctl([[:space:]]+-[^[:space:]]+)*[[:space:]]+($SLEEP([^[:alnum:]_.-]|\$)|(start|isolate)[[:space:]]+$SLEEP\.target)" \
  || hit '(^|[^[:alnum:]_.-])acpiconf[[:space:]]+([^;&|]*[[:space:]])?-s[[:space:]]*[345]([^[:alnum:]]|$)' \
  || hit '(^|[^[:alnum:]_.-])zzz([^[:alnum:]_.-]|$)' \
  || hit '(^|[^[:alnum:]_.-])pmset[[:space:]]+([^;&|]*[[:space:]])?sleepnow([^[:alnum:]_-]|$)' \
  || hit_without '(^|[^[:alnum:]_.-])rtcwake([^[:alnum:]_.-]|$)' \
       '(^|[[:space:]])(-m[[:space:]]*show|--mode[[:space:]=]show|--list-modes)([[:space:]]|$)' \
  || hit "$ONTO"'/sys/power/state' \
  || writes_to '/sys/power/state'
then
  deny "putting the server to sleep or hibernating it makes it \
as unreachable as powering it off"
fi

# --- Filesystem creation --------------------------------------
if hit '(^|[^[:alnum:]_.-])mkfs(\.[[:alnum:]]+)?([^[:alnum:]_.-]|$)'
then
  deny "mkfs destroys the filesystem on its target"
fi
# newfs_msdos and friends: the suffix must be part of the match,
# otherwise the trailing word boundary rejects the underscore.
if hit '(^|[^[:alnum:]_.-])newfs([._][[:alnum:]]+)*([^[:alnum:]_.-]|$)'
then
  deny "newfs destroys the filesystem on its target"
fi
if hit '(^|[^[:alnum:]_.-])(mke2fs|mkntfs|mkdosfs|mkexfatfs|mkudffs|mkfs2?)([^[:alnum:]_.-]|$)'
then
  deny "this filesystem creator destroys the data on its target"
fi
if hit '(^|[^[:alnum:]_.-])wipefs([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])(-a|--all|-o|--offset)'; then
  deny "wipefs in write mode erases filesystem signatures"
fi

# --- Formatting by a tool outside the mkfs family --------------
# A LUKS header written, erased or re-encrypted, a cleared ZFS
# label or a zeroed RAID superblock leaves the partition table
# alone and the data behind it unreadable: mkfs by another name.
# cryptsetup open/close/status/luksDump, zpool status/list and
# mdadm --detail read; zpool create, pvcreate and mdadm --create
# stay with the prompt (rules/partition-staging.md).
if hit '(^|[^[:alnum:]_.-])cryptsetup([[:space:]]+([^;&|]*[[:space:]])?(luksFormat|luksErase|erase|reencrypt)([^[:alnum:]_-]|$)|-reencrypt([^[:alnum:]_.-]|$))' \
  || hit '(^|[^[:alnum:]_.-])zpool[[:space:]]+([^;&|]*[[:space:]])?labelclear([^[:alnum:]_-]|$)' \
  || { hit '(^|[^[:alnum:]_.-])mdadm([^[:alnum:]_.-]|$)' \
       && hit '(^|[[:space:]])--zero-superblock([[:space:]]|$)'; }
then
  deny "this formats or erases the device by another name than \
mkfs: the LUKS header, the ZFS label or the RAID superblock go, \
and the data behind them with it"
fi

# --- AIX logical volume manager -------------------------------
# IBM documents each of these as destroying data: mkvg -f and
# extendvg -f ("previous contents of the physical volume are
# lost"), rmlv ("destroys all data in the specified logical
# volumes"), reducevg -d (deletes the logical volume data on the
# disk), rmfs (removes the file system and its volume), chpv -c
# (clears the boot record) and chpv -C (clears the owning volume
# manager). mkvg and extendvg without -f refuse a disk that
# belongs to a volume group and stay with the prompt, like
# pvcreate; lspv, lsvg, lslv and lsfs read.
if hit '(^|[^[:alnum:]_.-])(mkvg|extendvg)[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*f' \
  || hit '(^|[^[:alnum:]_.-])(rmlv|rmfs)([^[:alnum:]_.-]|$)' \
  || hit '(^|[^[:alnum:]_.-])reducevg[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*d' \
  || hit '(^|[^[:alnum:]_.-])chpv[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*[cC]'
then
  deny "this AIX volume manager command destroys the data on a \
disk or a logical volume, or the disk's boot record (lspv, lsvg, \
lslv and lsfs read)"
fi

# --- Partition table writers ----------------------------------
# Read-only inspection stays allowed: fdisk -l, sfdisk -l/-d,
# gdisk -l, sgdisk -p, parted -l/print, gpart show/status/list,
# lsblk, diskutil list.
if hit_without '(^|[^[:alnum:]_.-])fdisk([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])-l'; then
  deny "fdisk without -l opens the partition table for writing"
fi
# cfdisk has no read-only mode at all: it is the curses editor.
if hit '(^|[^[:alnum:]_.-])cfdisk([^[:alnum:]_.-]|$)'; then
  deny "cfdisk is an interactive partition editor with no \
read-only mode"
fi
if hit_without '(^|[^[:alnum:]_.-])sfdisk([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])(-l|--list|-d|--dump|-V|--verify)'; then
  deny "sfdisk in write mode modifies the partition table"
fi
if hit_without '(^|[^[:alnum:]_.-])c?gdisk([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])-l'; then
  deny "gdisk without -l opens the partition table for writing"
fi
# sgdisk and parted both take SEVERAL actions per invocation, so
# "allow when a read-only flag is present" cannot work: the read
# flag rides along with the write one (sgdisk -b backup.gpt -Z
# /dev/sda). Their write sets are closed and documented, so they
# are enumerated in full instead. Short flags below are the
# complete write set from sgdisk(8), -A/--attributes included
# (the attribute bits sit in the table); the read-only ones (a D
# E f F i L O p P V v) are absent on purpose, and so is -b, which
# writes a backup FILE and not the disk.
if hit '(^|[^[:alnum:]_.-])sgdisk([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])(-[ABcCdegGhIjklmnNorRstTuUzZ]|--(attributes|byte-swap-name|change-name|recompute-chs|delete|move-second-header|mbrtogpt|randomize-guids|hybrid|align-end|move-main-table|move-backup-table|load-backup|gpttombr|new|largest-new|clear|transpose|replicate|sort|typecode|transform-bsd|partition-guid|disk-guid|zap|zap-all))'
then
  deny "sgdisk write options modify the partition table"
fi
if hit '(^|[^[:alnum:]_.-])parted([^[:alnum:]_.-]|$)' \
  && hit '((mklabel|mktable|mkpartfs|mkpart|rescue|resize)([[:space:]]|$)|(rm|set|toggle|name|move|resizepart)[[:space:]]+[0-9]|disk_(set|toggle)[[:space:]])'
then
  deny "parted write commands modify the partition table"
fi
# growpart rewrites the partition entry to enlarge it. Its dry
# run is the only read-only form, and the exemption is scoped
# the same way as every other one here.
if hit_without '(^|[^[:alnum:]_.-])growpart([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])(-N|--dry-run)([[:space:]]|$)'; then
  deny "growpart rewrites the partition table to resize a \
partition"
fi
# FreeBSD and macOS GUID partition table editor. show is the
# read-only verb and stays allowed.
if hit '(^|[^[:alnum:]_.-])gpt[[:space:]]+(create|destroy|add|remove|modify|migrate|recover|resize|restore|boot|label|set|unset)([^[:alnum:]_-]|$)'
then
  deny "gpt write verbs modify the partition table"
fi
# macOS: the tool people actually partition with. Verbs are
# case-insensitive, hence hit_i.
if hit_i '(^|[^[:alnum:]_.-])diskutil([^[:alnum:]_.-]|$)' \
  && hit_i '(erasedisk|erasevolume|eraseoptical|zerodisk|randomdisk|secureerase|partitiondisk|splitpartition|mergepartitions|resizevolume|reformat|deletecontainer|deletevolume|erasecontainer|destroycontainer|resizecontainer|appleraid[[:space:]]+(delete|create))'
then
  deny "diskutil erase and partition verbs destroy data or the \
partition map"
fi
# set and unset flip partition attributes (active, bootme), which
# live in the table too.
if hit 'gpart[[:space:]]+(create|add|delete|destroy|modify|resize|bootcode|recover|set|unset|undo|commit)'
then
  deny "gpart write verbs modify the partition table"
fi
if hit '(^|[^[:alnum:]_-])dd([^[:alnum:]_-]|$)' \
  && hit 'of=["'\'']?/dev/'; then
  deny "dd onto a raw device overwrites disk content and \
partition table"
fi

# --- Raw-device wipers ----------------------------------------
# These leave the partition table intact and destroy everything
# it points at, which is the same effect by another route.
if hit '(^|[^[:alnum:]_.-])blkdiscard([^[:alnum:]_.-]|$)'; then
  deny "blkdiscard discards every block on the device"
fi
if hit '(^|[^[:alnum:]_.-])nvme[[:space:]]+(format|sanitize|write|delete-ns|create-ns|attach-ns|detach-ns|security-send|copy|dsm|zns)'
then
  deny "this nvme subcommand overwrites or destroys namespace \
data"
fi
if hit '(^|[^[:alnum:]_.-])hdparm([^[:alnum:]_.-]|$)' \
  && hit '(--security-(erase|erase-enhanced|set-pass|unlock|disable)|--trim-sector-ranges|--make-bad-sector|--write-sector|--dco-(restore|setmax)|--repair-sector)'
then
  deny "this hdparm option erases the drive or writes raw \
sectors"
fi
# badblocks -w is the destructive read-write test. -n and -sv
# are non-destructive and stay allowed.
if hit '(^|[^[:alnum:]_.-])badblocks([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])-[[:alnum:]]*w'; then
  deny "badblocks -w overwrites the device while testing it"
fi
if hit '(^|[^[:alnum:]_.-])shred([^[:alnum:]_.-]|$)' \
  && hit "$DEV"; then
  deny "shred on a disk device overwrites the whole device"
fi

# --- Firmware written to a drive -------------------------------
# hdparm --fwdownload (and its -mode variants) and nvme
# fw-download/fw-commit/fw-activate put a new firmware image on
# the drive; a bad one bricks it, data and all. nvme fw-log reads.
# Firmware VARIABLES (efivar, efivars, nvram) sit with the boot
# loaders below.
if { hit '(^|[^[:alnum:]_.-])hdparm([^[:alnum:]_.-]|$)' \
     && hit '(^|[[:space:]])--fwdownload'; } \
  || hit '(^|[^[:alnum:]_.-])nvme[[:space:]]+([^;&|]*[[:space:]])?fw-(download|commit|activate)([^[:alnum:]_-]|$)'
then
  deny "writing firmware to a drive can brick it together with \
its data; firmware updates are the operator's work"
fi

# A redirect, tee, cp or download onto a disk device does what
# dd of= does. /dev/null, /dev/stderr and /dev/disk/by-id are
# unaffected.
if hit ">[>|]?[[:space:]]*[\"']?$DEV" \
  || { hit "$DEV" && writes_to "${DEV}[[:alnum:]]*"; }
then
  deny "writing onto a raw disk device overwrites its content \
and partition table"
fi

# --- SSH keys and sshd_config ---------------------------------
# Deleting is only one way to lose a key. Renaming it away,
# truncating it to zero, or making it unreadable to sshd have
# the same effect, and the sshd_config rule below already
# reflected that while this one did not.
# KEY is checked once and first: most commands name no key, and
# every key rule below needs one.
# CLOBBER (defined with the server lock above): both rules below
# use it; the sshd_config rule adds cp, since a copy onto it
# replaces it.
HAS_KEY=0
hit "$KEY" && HAS_KEY=1
if [ "$HAS_KEY" -eq 1 ] \
  && { hit "(^|[^[:alnum:]_-])($CLOBBER)([^[:alnum:]_-]|\$)" \
       || hit '(^|[[:space:]])(-delete|--remove-s(ource|ent)-files)([[:space:]]|$)'; }
then
  deny "deleting, moving or re-permissioning SSH keys is never \
allowed"
fi
# A truncating redirect needs no command at all: : > key.
if hit "$ONTO$KEYPRIV"; then
  deny "redirecting onto an SSH key file truncates it"
fi
# ssh-keygen -f onto an existing private key overwrites it.
# Reading a .pub (for a fingerprint) stays allowed.
if hit '(^|[^[:alnum:]_-])ssh-keygen([^[:alnum:]_-]|$)' \
  && hit "$KEYPRIV"; then
  deny "ssh-keygen pointed at an existing key overwrites it - a \
fingerprint of a .pub runs in a call of its own, with no private \
key path in it"
fi
if hit "$SSHD"; then
  if hit "$ONTO$SSHD" \
    || { hit '(^|[^[:alnum:]_-])(sed|perl)([^[:alnum:]_-]|$)' \
         && hit "$INPLACE"; } \
    || hit "(^|[^[:alnum:]_-])$EDITOR([^[:alnum:]_-]|\$)" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp)([^[:alnum:]_-]|\$)" \
    || writes_to "$SSHD"
  then
    deny "modifying sshd_config or a file merged into it is \
never allowed (reading it is fine: cat, grep, sshd -T)"
  fi
fi

# --- Writes INTO an SSH key -----------------------------------
# Writing into a key file replaces it as surely as deleting it.
if [ "$HAS_KEY" -eq 1 ]; then
  if writes_to "($KEYFILE|$KEYDIR/?)"; then
    deny "writing into an SSH key file or a key directory \
replaces the keys there"
  fi
  # An in-place edit needs the key AFTER the tool in the same
  # invocation: in ssh -i ~/.ssh/id_ed25519 host "sed -i ..." the
  # key belongs to ssh, and the edit runs elsewhere.
  if hit "(^|[^[:alnum:]_.-])sed[[:space:]]([^;&|]*[[:space:]])?(-[[:alpha:]]*i|--in-place)[^;&|]*$KEYPRIV" \
    || hit "(^|[^[:alnum:]_-])${EDITOR}[[:space:]][^;&|]*$KEYPRIV"
  then
    deny "editing an SSH key file in place can delete keys from it"
  fi
fi

# --- Boot loaders and firmware variables ----------------------
# A boot entry, the boot order or the loader itself is the way
# back into a host that has no console. jolitz reads them
# (efibootmgr -v, bootctl status, bless --info); a write runs
# with the operator's override only (rules/efi-boot.md,
# rules/dual-boot.md). Writers, by effect: efibootmgr in any
# form that creates, deletes, reorders, activates or times an
# entry (the read forms are bare, -v and -V); grub-install and
# update-grub, which rewrite the loader or its config, and
# grub-mkconfig only with -o (without it, it prints); the
# bootctl verbs that install, update, remove or set; bless in
# any form but --info and --getBoot; efivar -w; and a write,
# delete or re-permission under the efivars mount, where a
# removed variable has bricked boards.
if hit '(^|[^[:alnum:]_.-])efibootmgr([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])(-[bBcCaAoOnNtTDwy@]|--(bootnum|delete-bootnum|create|create-only|active|inactive|bootorder|delete-bootorder|bootnext|delete-bootnext|timeout|delete-timeout|remove-dups|write-signature|sysprep|delete|append-binary-args|label|loader|disk|part))([[:space:]]|=|$)'
then
  deny "efibootmgr in a write form changes the boot entries or \
the boot order (efibootmgr -v reads them); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit_without '(^|[^[:alnum:]_.-])grub2?-install([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])(-V|--version|--help)([[:space:]]|$)'; then
  deny "grub-install rewrites the boot loader; boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit '(^|[^[:alnum:]_.-])update-grub2?([^[:alnum:]_.-]|$)' \
  || { hit '(^|[^[:alnum:]_.-])grub2?-mkconfig([^[:alnum:]_.-]|$)' \
       && hit '(^|[[:space:]])(-o|--output)([[:space:]]|=)'; }
then
  deny "update-grub and grub-mkconfig -o rewrite the boot loader \
config (grub-mkconfig without -o only prints it); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit '(^|[^[:alnum:]_.-])bootctl([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])(install|update|remove|cleanup|unlink|set-default|set-oneshot|set-timeout|set-timeout-oneshot|random-seed|reboot-to-firmware)([[:space:]]|$)'
then
  deny "this bootctl verb installs, removes or reconfigures the \
boot loader (bootctl status and list read it); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit_without '(^|[^[:alnum:]_.-])bless([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])(--info|--getBoot|--version|--help)([[:space:]]|$)'; then
  deny "bless in a write form changes what the Mac boots \
(bless --info and --getBoot read it); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit '(^|[^[:alnum:]_.-])efivar([^[:alnum:]_.-]|$)' \
  && hit '(^|[[:space:]])(-w|--write|-a|--append)([[:space:]]|$)'; then
  deny "efivar -w writes a firmware variable; boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
# The boot default, or the next boot, without touching the
# loader: grub-set-default and grub-reboot write grubenv,
# grub-editenv set/unset/create does the same by hand (list
# reads it), systemsetup -setstartupdisk picks the Mac's startup
# volume. bootctl set-* and bless sit above.
if hit '(^|[^[:alnum:]_.-])grub2?-(set-default|reboot)([^[:alnum:]_.-]|$)' \
  || hit '(^|[^[:alnum:]_.-])grub2?-editenv[[:space:]]+([^;&|]*[[:space:]])?(set|unset|create)([[:space:]]|$)' \
  || hit_i '(^|[^[:alnum:]_.-])systemsetup[[:space:]]+([^;&|]*[[:space:]])?-setstartupdisk([^[:alnum:]_-]|$)'
then
  deny "changing the default or next boot entry decides what the \
host comes back as; boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
# AIX: bootlist sets the boot order unless -o only displays it
# (-i invalidates the list); bosboot -a writes the boot image
# into hd5 (-v verifies, -q sizes); mkboot writes or with -c
# zeroes a boot record and IBM supports it for nobody;
# alt_disk_copy and alt_disk_install clone rootvg onto another
# disk and point the bootlist at it; alt_rootvg_op -X removes an
# alternate rootvg.
# The exemption is the whole display form, -m <mode> and -o and
# nothing after them: a device named next to -o is set, then
# shown.
if hit_without_unq '(^|[^[:alnum:]_.-])bootlist([^[:alnum:]_.-]|$)' \
     '^[^[:alnum:]_.-]?bootlist[[:space:]]+(-m[[:space:]]*(normal|service|both|prevboot)[[:space:]]+-o|-o[[:space:]]+-m[[:space:]]*(normal|service|both|prevboot))[[:space:])]*$' \
  || hit '(^|[^[:alnum:]_.-])bosboot[[:space:]]+([^;&|]*[[:space:]])?-[[:alpha:]]*a' \
  || hit '(^|[^[:alnum:]_.-])(mkboot|alt_disk_copy|alt_disk_install)([^[:alnum:]_.-]|$)' \
  || hit '(^|[^[:alnum:]_.-])alt_rootvg_op[[:space:]]+([^;&|]*[[:space:]])?-X'
then
  deny "this rewrites the AIX boot list, boot image or boot \
record, or clones rootvg onto another disk (bootlist -o and \
bosboot -v read); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
# nvram (macOS): nvram name=value sets a firmware variable, -d
# deletes one, -c clears them all, -f loads a file of them.
# nvram -p and nvram name print.
if hit '(^|[^[:alnum:]_.-])nvram[[:space:]]+([^;&|]*[[:space:]])?(-[[:alpha:]]*[dcf]([[:space:]]|$)|[^[:space:]=-][^[:space:]=]*=)'
then
  deny "nvram in a write form changes a firmware variable (nvram \
-p reads them); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit "$EFIVARS"; then
  if hit "$ONTO$EFIVARS" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp|dd|chattr)([^[:alnum:]_-]|\$)" \
    || hit '(^|[[:space:]])-delete([[:space:]]|$)' \
    || writes_to "$EFIVARS"
  then
    deny "writing to or deleting under the efivars mount changes \
firmware variables and can brick the board (cat and ls read \
them); boot work needs the \
operator override JOLITZ_GUARD_DISABLE"
  fi
fi

# --- Accounts and sudo ----------------------------------------
# Creating an account or adding a key is ordinary work that asks
# first. The hook backs only the part that locks someone out
# or cannot be undone: deleting an account or a group, and any
# write to sudoers or to the account files themselves, where one
# bad line is "sudo: parse error" for everybody. Creating an
# account (useradd, pw useradd) and resetting a password stay
# with the prompt: nothing breaks, and a reset is jolitz's own
# everyday work (rules/ethics.md).
if hit '(^|[^[:alnum:]_.-])(userdel|deluser|luserdel|groupdel|delgroup|lgroupdel|rmuser|rmgroup)([^[:alnum:]_.-]|$)' \
  || hit '(^|[^[:alnum:]_.-])pw[[:space:]]+(userdel|groupdel|user[[:space:]]+del|group[[:space:]]+del)([^[:alnum:]_-]|$)' \
  || hit '(^|[^[:alnum:]_.-])sysadminctl[[:space:]].*-deleteUser([^[:alnum:]_-]|$)' \
  || hit '(^|[^[:alnum:]_.-])dscl[[:space:]].*-delete[[:space:]]+/(Users|Groups)/'
then
  deny "deleting an account or a group locks people out and \
cannot be undone; account work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
# visudo edits sudoers; visudo -c only checks a file.
if hit_without '(^|[^[:alnum:]_.-])visudo([^[:alnum:]_.-]|$)' \
  '(^|[[:space:]])-c([[:space:]]|$)'; then
  deny "visudo edits sudoers, and one bad line locks everybody \
out of sudo (visudo -c only checks); sudo work needs the \
operator override JOLITZ_GUARD_DISABLE"
fi
if hit '(^|[^[:alnum:]_.-])(vipw|vigr)([^[:alnum:]_.-]|$)'; then
  deny "vipw and vigr edit the account files in place; account \
work needs the operator override JOLITZ_GUARD_DISABLE"
fi
if hit "$SUDOERS"; then
  if hit "$ONTO$SUDOERS" \
    || { hit '(^|[^[:alnum:]_-])(sed|perl)([^[:alnum:]_-]|$)' \
         && hit "$INPLACE"; } \
    || hit "(^|[^[:alnum:]_-])$EDITOR([^[:alnum:]_-]|\$)" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp)([^[:alnum:]_-]|\$)" \
    || writes_to "$SUDOERS"
  then
    deny "writing sudoers or a sudoers.d file grants or revokes \
root rights (cat and visudo -c read it); sudo work needs the \
operator override JOLITZ_GUARD_DISABLE"
  fi
fi
if hit "$ACCOUNTS"; then
  if hit "$ONTO$ACCOUNTS" \
    || { hit '(^|[^[:alnum:]_-])(sed|perl)([^[:alnum:]_-]|$)' \
         && hit "$INPLACE"; } \
    || hit "(^|[^[:alnum:]_-])$EDITOR([^[:alnum:]_-]|\$)" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp)([^[:alnum:]_-]|\$)" \
    || writes_to "$ACCOUNTS"
  then
    deny "writing an account file directly bypasses every check \
useradd and passwd make (getent, grep and awk read them); \
account work needs the \
operator override JOLITZ_GUARD_DISABLE"
  fi
fi

# --- A protected directory as the working directory -----------
# Every path rule above knows its file by the path it is spelled
# with. After cd /etc/ssh the same file is a bare sshd_config, the
# host key a bare ssh_host_ed25519_key, and under sudoers.d or
# sshd_config.d a drop-in has any name at all, so no rule saw
# `cd /etc/sudoers.d && echo ... > deploy`. The directory decides
# instead: a cd or pushd into one of them, anywhere in the
# command, makes every write in that command a write there. A
# write is a redirect onto a relative name (the absolute ones
# stay allowed, so cd /etc/ssh && cat sshd_config > /tmp/copy
# runs), sed or perl in place, an editor, the CLOBBER set with cp,
# find -delete, or a tool that names its output (writes_to, any
# path). Accepted false positive: rm or cp of an unrelated file
# from inside the directory; run it from another cwd.
#
# find -delete with such a directory as its root deletes by a
# name it never spells as a path, so it is denied on the
# directory alone, like the lock above. /etc/ssh is in the set
# only for these two forms; rm -rf /etc/ssh stays open on the
# terms given at KEYDIR.
PROTDIR='(/etc/ssh|/conf/sshd|/etc/sudoers\.d|sshd_config\.d|\.ssh)'
if hit "(^|[^[:alnum:]_.-])(cd|pushd)[[:space:]]+[\"']?[^[:space:];|&]*$PROTDIR/?[\"']?([[:space:];&|]|\$)"; then
  if hit '>[>|]?[[:space:]]*["'\'']?[^/[:space:];|&<>"'\''$]' \
    || { hit '(^|[^[:alnum:]_-])(sed|perl)([^[:alnum:]_-]|$)' \
         && hit "$INPLACE"; } \
    || hit "(^|[^[:alnum:]_-])$EDITOR([^[:alnum:]_-]|\$)" \
    || hit "(^|[^[:alnum:]_-])($CLOBBER|cp)([^[:alnum:]_-]|\$)" \
    || hit '(^|[[:space:]])-delete([[:space:]]|$)' \
    || writes_to '[^[:space:]"'\'']'
  then
    deny "after cd into a directory holding sshd_config, SSH keys \
or sudoers drop-ins, every relative name is a protected file; \
write elsewhere from another working directory"
  fi
fi
if hit "$PROTDIR" && hit '(^|[[:space:]])-delete([[:space:]]|$)'; then
  deny "find -delete under a directory holding sshd_config, SSH \
keys or sudoers drop-ins deletes by a name it never spells as a \
path; list the matches with find alone and judge each"
fi

# --- Effects reached through an interpreter -------------------
# Every rule above recognizes a write by the way it is spelled: a
# redirect, tee, sed -i, an editor, rm/mv/chmod/truncate. A
# language runtime spells none of those and reaches the same
# effects through plain file I/O:
#
#   python3 -c "open('/etc/ssh/sshd_config','a').write(...)"
#   node -e "require('fs').unlinkSync('...authorized_keys')"
#   python3 -c "open('/dev/sda','wb').write(...)"
#
# Its command line is opaque to a pattern matcher -- read and
# write look alike from outside -- so the read-only exemption
# cannot be proven, and an unprovable exemption must not apply.
# A runtime carrying a protected path or a raw device is
# therefore a write. Issue #6.
#
# This enumerates RUNTIMES, not write syntaxes, on purpose. The
# set of ways to write a file grows with every language feature
# and can never be closed; the set of interpreters jolitz might
# meet on a server is small and moves slowly. It still is a list,
# so this rule is a backstop against the everyday mistake, not a
# sandbox: an interpreter that builds its target string at
# runtime, or downloads it, defeats any string matcher. Real
# isolation is the operator's job, not this hook's.
#
# Interpreters that shell out (os.system, %x, child_process) need
# no rule of their own: every check above scans the whole command
# string, so the taboo word inside is caught where it stands.
#
# Accepted false positive: any command that merely mentions an
# interpreter alongside one of these targets is blocked too, even
# when it only reads -- awk '/^Port/' /etc/ssh/sshd_config, or a
# cat of the file chained to an unrelated python call. Read with
# cat, grep, jq, stat or sshd -T instead, which is what the rule
# files use anyway.
if hit "$INTERP"; then
  if hit "$KEY"; then
    deny "an interpreter with an SSH key path on its command \
line can overwrite or delete the key, and a pattern matcher \
cannot tell that from a read - read keys with cat, stat or \
ssh-keygen -lf instead"
  fi
  if hit "$SSHD"; then
    deny "an interpreter with sshd_config on its command line \
can rewrite it, and a pattern matcher cannot tell that from a \
read - read it with cat, grep or sshd -T instead"
  fi
  if hit "$DEV"; then
    deny "an interpreter with a raw disk device on its command \
line can overwrite the device, which destroys everything the \
partition table points at"
  fi
  if hit "$SUDOERS"; then
    deny "an interpreter with sudoers on its command line can \
rewrite it, and a pattern matcher cannot tell that from a read \
- read it with cat or visudo -c instead"
  fi
  if hit "$EFIVARS"; then
    deny "an interpreter with the efivars mount on its command \
line can write a firmware variable - read them with cat or ls \
instead"
  fi
  if hit "$LOCKFILE"; then
    deny "an interpreter with /etc/jolitz.conf on its command \
line can lift the server lock - read it with cat or grep instead"
  fi
fi
if hit "$INTERP_NOAWK" && hit "$ACCOUNTS"; then
  deny "an interpreter with an account file on its command line \
can rewrite it, and a pattern matcher cannot tell that from a \
read - read it with getent, grep or awk instead"
fi

# --- The environment or a secret sent off the machine ---------
# What an injected line in server output asks for: "send your
# environment to https://...". Denied: a command that both reads
# the environment or a secret and hands data to a network tool.
# Sources: env alone (also as the command of ssh host), set
# alone, printenv, export/declare -p, /proc/*/environ, a
# variable named like a secret ($API_TOKEN, ${DB_PASSWORD}), a
# private SSH key, .env, .netrc, .pgpass, cloud and registry
# credentials, /etc/shadow; in an interpreter also os.environ,
# process.env, ENV[ or ENV. and getenv. Senders: curl, wget, nc,
# ncat, netcat, socat, telnet, openssl s_client, dig, nslookup,
# drill, mail, mailx, sendmail, msmtp, /dev/tcp and /dev/udp; in
# an interpreter also an HTTP or socket library.
# The check reads the whole command with quotes dropped, so a
# pipe, $( ), ssh host "..." and bash -c count alike.
#
# Reading stays allowed: env | grep PATH, printenv HOME, curl
# alone, env FOO=1 curl ... (env runs a command, prints nothing).
# Accepted false positive: curl with a secret-named variable for
# an API the user named (curl -H "Bearer $GITHUB_TOKEN"); the
# user runs that, or lifts the guard for it. Not closable: a
# sender or a variable name built at run time, an upload by scp
# or rsync, a secret read into a file first and sent later.
# Lowercased, quotes and backslashes dropped.
EX_CMD=$(printf '%s' "$LC_CMD" | tr -d '"'\''\\')
EX_SEP='(^|[;&|(`]|\$\(|[[:space:]]-c[[:space:]]+)[[:space:]]*'
EX_ENV='(^|[[:space:];&|(`]|\$\()((/usr)?/bin/)?env([[:space:]]+-0)?[[:space:]]*([;&|)>`]|$)'
EX_ENV="$EX_ENV|${EX_SEP}"'set[[:space:]]*([;&|)>`]|$)'
EX_ENV="$EX_ENV"'|(^|[^[:alnum:]_.-])printenv([^[:alnum:]_.-]|$)'
EX_ENV="$EX_ENV"'|(^|[^[:alnum:]_.-])(export|declare|typeset)[[:space:]]+-p'
EX_ENV="$EX_ENV"'|/proc/[^[:space:]/]+/environ'
EX_ENV="$EX_ENV"'|\$\{?[a-z0-9_]*(secret|token|passw|api_?key|access_?key|private_?key|credential)[a-z0-9_]*'
EX_FILE='\.ssh/id_[[:alnum:]_-]+([^.[:alnum:]_-]|$)|ssh_host_[[:alnum:]_-]*key([^.[:alnum:]_-]|$)'
EX_FILE="$EX_FILE"'|(^|[[:space:]/@=<])\.env([[:space:];&|)]|$)|\.netrc|\.pgpass|\.git-credentials'
EX_FILE="$EX_FILE"'|\.aws/credentials|\.docker/config\.json|\.kube/config|\.config/gh/hosts\.yml|/etc/shadow'
EX_NET='(^|[[:space:];&|(`])((/usr)?(/local)?/s?bin/)?(curl|wget|nc|ncat|netcat|socat|telnet|dig|nslookup|drill|mail|mailx|sendmail|msmtp)([[:space:];&|)`]|$)'
EX_NET="$EX_NET"'|/dev/(tcp|udp)/|openssl[[:space:]]+s_client'
EX_IENV='os\.environ|process\.env|(^|[^[:alnum:]_.])env(\[|\.)|getenv'
EX_INET='urlopen|urllib|requests\.|httpx|http\.client|socket|fetch\(|net::http|lwp|http\.request|https?://'
EX_SRC=0
printf '%s\n' "$EX_CMD" | grep -Eq "$EX_ENV|$EX_FILE" && EX_SRC=1
EX_SEND=0
printf '%s\n' "$EX_CMD" | grep -Eq "$EX_NET" && EX_SEND=1
if hit "$INTERP_NOAWK"; then
  printf '%s\n' "$EX_CMD" | grep -Eq "$EX_IENV" && EX_SRC=1
  printf '%s\n' "$EX_CMD" | grep -Eq "$EX_INET" && EX_SEND=1
fi
# The same payload without env: a request body built from command
# output on the machine, curl -d "{\"host\":\"$(hostname)\"}", as
# a planted "compliance notice" in an SSH banner asked for. Denied:
# a curl or wget upload option (-d, --data*, --json, -F, --form*,
# -T, --upload-file, --post-data, --body-data) followed by $( or a
# backtick before the next ; & or |, continuation lines joined.
# Case matters here: curl -f is
# --fail, -F a form. A URL built the same way (curl -o x
# https://.../$(uname -m)) stays allowed; it fetches. Accepted
# false positive: wget -d is --debug, so wget -d with a computed
# URL is denied too.
EX_UQ=$(printf '%s\n' "$CMD" \
  | awk '{ if (sub(/\\$/, "")) printf "%s ", $0; else print }' \
  | tr -d '"'\''\\')
if printf '%s\n' "$EX_UQ" | grep -Eq \
  '(^|[^[:alnum:]_.-])(curl|wget)([[:space:]][^;&|]*)?[[:space:]](-d|--data[a-z-]*|--json|-F|--form[a-z-]*|-T|--upload-file|--post-data|--body-data)[^;&|]*(\$\(|`)'
then
  deny "this request sends a body built from command output on \
the machine (\$( ) or backticks in an upload option) - what a \
planted notice in server output asks for to fingerprint the host; \
if the user asked for exactly this, they run it themselves"
fi
if [ "$EX_SRC" -eq 1 ] && [ "$EX_SEND" -eq 1 ]; then
  deny "this command reads the environment or a secret and hands \
it to a network tool, which sends credentials off the machine - \
the classic request of an injected line in server output; if the \
user asked for exactly this, they run it themselves"
fi

# --- A redirect onto a computed target (issue #51) ------------
# In unquoted echo text, -> is a dash plus a redirect, and the
# next word names the file it truncates: -> $(readlink -f $E)
# replaced /usr/bin/nvim with prose. Denied: any redirect onto a
# $( ) lookup, and an arrow onto any expansion. A plain > "$VAR"
# stays allowed, scripts write that way. Accepted false positive:
# the same text in quotes, which one ssh or bash -c level further
# in are gone; the guard cannot count quote depth.
# Most commands hold no $ after a >, so case skips the forks.
case "$CMD" in
  *'>'*'$'*)
    if printf '%s\n' "$CMD" \
      | grep -Eq '(>[|]?[[:space:]]*["'\'']?\\?\$\(|->[[:space:]]*["'\'']?\\?\$)'
    then
      deny "a redirect onto a computed target overwrites whatever \
file it names - an arrow in echo text is a redirect too, so \
checks print data, not prose"
    fi
    ;;
esac

# No taboo matched: no decision, normal permission flow applies.
exit 0

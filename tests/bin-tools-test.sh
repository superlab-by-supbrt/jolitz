#!/bin/sh
# bin-tools-test.sh — fixture tests for bin/jolitz-doctor,
# bin/jolitz-log and bin/jolitz-guard-report. Builds a
# throwaway HOME, PATH and memory/ tree, runs the tools on
# them, and checks their output. No network, no server:
# `claude` is a fake script, the guard log is written by
# hand.
#   sh tests/bin-tools-test.sh

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
DOCTOR="$REPO_DIR/bin/jolitz-doctor"
LOG="$REPO_DIR/bin/jolitz-log"
REPORT="$REPO_DIR/bin/jolitz-guard-report"
PASS=0
FAIL=0

T=$(mktemp -d "${TMPDIR:-/tmp}/bin-tools-test.XXXXXX")
trap 'rm -rf "$T"' EXIT
HOME="$T/home"
export HOME
mkdir -p "$HOME"
# No agent: ssh-add -l must exit 2 the same way everywhere.
unset SSH_AUTH_SOCK
REAL_PATH=$PATH

ok() {
  if [ "$1" -eq 0 ]; then PASS=$((PASS + 1))
  else FAIL=$((FAIL + 1)); echo "FAIL: $2"; fi
}
# has <file> <fixed string> <label>
has() {
  grep -qF -- "$2" "$1"; ok $? "$3"
}
hasnt() {
  if grep -qF -- "$2" "$1"; then ok 1 "$3"; else ok 0 "$3"; fi
}
# lines <file> <n> <label>
lines() {
  n=$(wc -l < "$1" | tr -d ' ')
  [ "$n" -eq "$2" ]; ok $? "$3 (got $n lines)"
}

TODAY=$(date +%Y-%m-%d)

# --- fake claude binaries and restricted PATHs ---------------
mkdir -p "$T/bin1" "$T/bin2" "$T/nojq" "$T/noclaude"
printf '#!/bin/sh\necho "2.1.312 (Claude Code)"\n' > "$T/bin1/claude"
printf '#!/bin/sh\necho "2.0.1 (Claude Code)"\n' > "$T/bin2/claude"
chmod 755 "$T/bin1/claude" "$T/bin2/claude"
# Everything the tools need, as symlinks, so that one tool
# can be hidden without hiding the shell.
for t in sh awk grep sed sort cut tr wc head tail date git \
    cat basename dirname mktemp uniq ls stat ssh-add rm env \
    uname expr; do
  p=$(command -v "$t" 2>/dev/null) || continue
  ln -s "$p" "$T/nojq/$t"
  ln -s "$p" "$T/noclaude/$t"
done
ln -s "$T/bin1/claude" "$T/nojq/claude"
if p=$(command -v jq 2>/dev/null); then ln -s "$p" "$T/noclaude/jq"; fi

# --- memory tree ---------------------------------------------
M="$T/memory"
mkdir -p "$M/servers/web1.example.com" "$M/servers/db1.example.com" \
  "$M/eol"
cat > "$M/user.md" <<'EOF'
# User
- Operator: alice
EOF
cat > "$M/servers/web1.example.com/memory.md" <<EOF
# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- Last connected: $TODAY
EOF
cat > "$M/servers/web1.example.com/changelog.log" <<'EOF'
[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green
— because deploys interrupted visitors
  Detail: templated app@.service (blue :4003,
  green :4005).
  Rollback: re-enable app.service, restore vhost
  backup from /var/backups/jolitz/.
  Flags: migrations must now stay
  backward-compatible.
[2026-06-12 08:00] [alice as root] nginx reloaded (auto, policy)
EOF
ln -s web1.example.com "$M/servers/www.example.com"

cat > "$M/servers/db1.example.com/memory.md" <<EOF
# db1.example.com
- IP: 203.0.113.20
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, database
- Last connected: $TODAY
EOF
cat > "$M/servers/db1.example.com/changelog.log" <<'EOF'
[2025-01-01 10:00] [bob as root] PostgreSQL 15 installed
  Detail: from the Debian repo.
EOF
echo 2020-01-01 > "$M/eol/debian.fetched"
echo '{}' > "$M/eol/debian.json"
echo "$TODAY" > "$M/eol/ubuntu.fetched"
echo '{}' > "$M/eol/ubuntu.json"

# --- guard log -----------------------------------------------
GL="$T/guard.log"
{
  printf '2026-10-01T10:00:00Z\tbash\tpowering off a machine\thalt\n'
  printf '2026-10-15T11:00:00Z\tbash\tmodifying the partition table\tsgdisk -n 1 /dev/sdb\n'
  printf '%sT12:00:00Z\tbash\tpowering off a machine\tshutdown -h now\n' "$TODAY"
} > "$GL"

# --- doctor --------------------------------------------------
mkdir -p -m 700 "$HOME/.cache/jolitz"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" -d "$M" > "$T/d1.out" 2>&1; rc=$?
ok $(( rc != 0 )) "doctor exits 0 without FAIL (got $rc: $(cat "$T/d1.out"))"
has "$T/d1.out" 'OK    Claude Code 2.1.312' "doctor: new enough claude is OK"
has "$T/d1.out" 'OK    guard hooks' "doctor: both guard hooks registered"
has "$T/d1.out" 'OK    jq' "doctor: jq found"
has "$T/d1.out" 'OK    ~/.cache/jolitz' "doctor: cache dir with mode 700"
has "$T/d1.out" 'OK    memory/user.md' "doctor: user.md present"
has "$T/d1.out" 'OK    memory lint: 2 hosts' "doctor: lint summary"
has "$T/d1.out" 'WARN  EOL cache: debian fetched 2020-01-01' \
  "doctor: the oldest EOL cache decides"
has "$T/d1.out" 'OK    guard.log' "doctor: small guard log is OK"
# Without ssh-add (busybox hosts, minimal containers) the line
# is an OK with a note, and that is the documented behaviour.
if command -v ssh-add >/dev/null 2>&1; then
  has "$T/d1.out" 'WARN  ssh agent' "doctor: no agent is a warning"
else
  has "$T/d1.out" 'OK    ssh agent' "doctor: missing ssh-add is noted"
fi
has "$T/d1.out" '  git' "doctor: git state line present"
hasnt "$T/d1.out" 'FAIL' "doctor: nothing fails on a sane setup"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" -d "$M" --strict > /dev/null 2>&1; rc=$?
ok $(( rc != 1 )) "doctor --strict exits 1 on warnings (got $rc)"

PATH="$T/bin2:$REAL_PATH" "$DOCTOR" -d "$M" > "$T/d2.out" 2>&1; rc=$?
ok $(( rc != 2 )) "doctor exits 2 on an old claude (got $rc)"
has "$T/d2.out" 'FAIL  Claude Code 2.0.1' "doctor: old claude fails"
has "$T/d2.out" 'ssh agent' "doctor keeps going after a FAIL"

PATH="$T/nojq" "$DOCTOR" -d "$M" > "$T/d3.out" 2>&1; rc=$?
ok $(( rc != 2 )) "doctor exits 2 without jq (got $rc)"
has "$T/d3.out" 'FAIL  jq' "doctor: missing jq fails"
has "$T/d3.out" 'WARN  guard hooks' "doctor: hooks unchecked without jq"
has "$T/d3.out" 'ssh agent' "doctor keeps going without jq"

PATH="$T/noclaude" "$DOCTOR" -d "$M" > "$T/d4.out" 2>&1; rc=$?
ok $(( rc != 2 )) "doctor exits 2 without claude (got $rc)"
has "$T/d4.out" 'FAIL  Claude Code: claude not in PATH' \
  "doctor: missing claude fails"
has "$T/d4.out" 'ssh agent' "doctor keeps going without claude"

chmod 755 "$HOME/.cache/jolitz"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" -d "$M" > "$T/d5.out" 2>&1
has "$T/d5.out" 'WARN  ~/.cache/jolitz' "doctor: open cache dir warns"
chmod 700 "$HOME/.cache/jolitz"

rm "$M/user.md"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" -d "$M" > "$T/d6.out" 2>&1
has "$T/d6.out" 'WARN  memory/user.md' "doctor: missing user.md warns"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" -d "$T/nowhere" > "$T/d7.out" 2>&1; rc=$?
ok $(( rc != 0 )) "doctor survives an empty memory dir (got $rc)"

PATH="$T/bin1:$REAL_PATH" "$DOCTOR" --help > "$T/dh.out"; rc=$?
ok $(( rc != 0 )) "doctor --help exits 0"
has "$T/dh.out" 'Usage:' "doctor --help prints usage"
PATH="$T/bin1:$REAL_PATH" "$DOCTOR" --bogus > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "doctor rejects an unknown option (got $rc)"

# --- log -----------------------------------------------------
"$LOG" -d "$M" > "$T/l1.out"; rc=$?
ok $(( rc != 0 )) "log runs (got $rc)"
lines "$T/l1.out" 3 "log: one line per entry, aliases not twice"
head -n 1 "$T/l1.out" > "$T/l1.first"
has "$T/l1.first" '2026-06-12 08:00 web1.example.com	[alice as root] nginx reloaded (auto, policy)' \
  "log: newest first, tab before the headline"
tail -n 1 "$T/l1.out" > "$T/l1.last"
has "$T/l1.last" '2025-01-01 10:00 db1.example.com	' "log: oldest last"
has "$T/l1.out" 'app.example.com now deploys blue/green — because deploys interrupted visitors' \
  "log: unindented continuation lines join the headline"
hasnt "$T/l1.out" 'Detail:' "log: detail lines stay out"

"$LOG" -d "$M" --grep nginx > "$T/l2.out"
has "$T/l2.out" 'nginx reloaded' "log --grep: nginx line"
hasnt "$T/l2.out" 'PostgreSQL' "log --grep: PostgreSQL line dropped"
lines "$T/l2.out" 1 "log --grep: one match"
"$LOG" -d "$M" --grep NGINX > "$T/l2b.out"
lines "$T/l2b.out" 1 "log --grep: case-insensitive"
"$LOG" -d "$M" --grep 'restore vhost' > "$T/l2c.out"
has "$T/l2c.out" 'blue/green' "log --grep: matches detail lines"
lines "$T/l2c.out" 1 "log --grep: detail match prints its headline once"

"$LOG" -d "$M" --since 2026-06-12 > "$T/l3.out"
lines "$T/l3.out" 1 "log --since cuts older entries"
has "$T/l3.out" 'nginx reloaded' "log --since keeps the newer entry"
"$LOG" -d "$M" --since 2026-13-01 > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "log --since rejects a bad date (got $rc)"

"$LOG" -d "$M" --tag web > "$T/l4.out"
lines "$T/l4.out" 2 "log --tag web keeps web1 only"
hasnt "$T/l4.out" 'db1.example.com' "log --tag web drops db1"
"$LOG" -d "$M" --tag Database > "$T/l4b.out"
lines "$T/l4b.out" 1 "log --tag matches tags case-insensitively"
"$LOG" -d "$M" --host db1.example.com > "$T/l5.out"
lines "$T/l5.out" 1 "log --host keeps one host"
has "$T/l5.out" 'PostgreSQL 15 installed' "log --host prints its entry"
"$LOG" -d "$M" --host www.example.com > "$T/l5b.out"
lines "$T/l5b.out" 2 "log --host resolves an alias symlink"
"$LOG" -d "$M" --grep 'app\.example' > "$T/l5c.out"
lines "$T/l5c.out" 1 "log --grep keeps a backslash escape"
"$LOG" -d "$M" --host nope.example.com > "$T/l6.out"; rc=$?
ok $(( rc != 0 )) "log: unknown host is empty output, exit 0 (got $rc)"
lines "$T/l6.out" 0 "log: unknown host prints nothing"
"$LOG" --help > "$T/lh.out"; rc=$?
ok $(( rc != 0 )) "log --help exits 0"
has "$T/lh.out" 'Usage:' "log --help prints usage"

# --- guard report --------------------------------------------
"$REPORT" -f "$GL" > "$T/r1.out"; rc=$?
ok $(( rc != 0 )) "report runs (got $rc)"
head -n 1 "$T/r1.out" > "$T/r1.first"
has "$T/r1.first" "Guard denials 2026-10-01..$TODAY" "report: title with range"
has "$T/r1.out" "| powering off a machine | 2 | $TODAY |" \
  "report: repeated reason counted with last date"
has "$T/r1.out" '| modifying the partition table | 1 | 2026-10-15 |' \
  "report: single reason row"
has "$T/r1.out" 'A denial that blocked real work is a false positive; attach the command only after checking it for secrets.' \
  "report: closing sentence"
hasnt "$T/r1.out" 'halt' "report: no command text without --commands"
hasnt "$T/r1.out" 'sgdisk' "report: no second command either"

"$REPORT" -f "$GL" --commands > "$T/r2.out"
has "$T/r2.out" 'halt' "report --commands: first subject"
has "$T/r2.out" 'shutdown -h now' "report --commands: second subject"
has "$T/r2.out" 'sgdisk -n 1 /dev/sdb' "report --commands: other reason"

"$REPORT" -f "$GL" --since "$TODAY" > "$T/r3.out"
has "$T/r3.out" "| powering off a machine | 1 | $TODAY |" "report --since cuts"
hasnt "$T/r3.out" 'partition table' "report --since drops old reasons"
has "$T/r3.out" "Guard denials $TODAY..$TODAY" "report --since: range"

"$REPORT" -f "$T/missing.log" > "$T/r4.out" 2>&1; rc=$?
ok $(( rc != 1 )) "report exits 1 without a log (got $rc)"
cp "$GL" "$HOME/.cache/jolitz/guard.log"
"$REPORT" > "$T/r5.out"; rc=$?
ok $(( rc != 0 )) "report reads ~/.cache/jolitz/guard.log by default (got $rc)"
has "$T/r5.out" '| 2 |' "report default path: counts"
"$REPORT" --help > "$T/rh.out"; rc=$?
ok $(( rc != 0 )) "report --help exits 0"
has "$T/rh.out" 'Usage:' "report --help prints usage"

echo "bin-tools tests: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

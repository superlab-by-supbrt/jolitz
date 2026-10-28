#!/bin/sh
# memory-tools-test.sh — fixture tests for
# bin/jolitz-memory-lint and bin/jolitz-overview.
# Builds a throwaway memory/ tree, runs both tools on
# it, and checks their output. No network: the EOL
# cache is written by hand.
#   sh tests/memory-tools-test.sh

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
LINT="$REPO_DIR/bin/jolitz-memory-lint"
OVERVIEW="$REPO_DIR/bin/jolitz-overview"
PASS=0
FAIL=0

T=$(mktemp -d "${TMPDIR:-/tmp}/memory-tools-test.XXXXXX")
trap 'rm -rf "$T"' EXIT
# A home without a guard log, unless a test writes one.
HOME="$T/home"
export HOME
mkdir -p "$HOME"

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

TODAY=$(date +%Y-%m-%d)
M="$T/memory"
mkdir -p "$M/servers/web1.example.com" "$M/servers/db1.example.com" \
  "$M/servers/mac.local" "$M/audits" "$M/eol"

cat > "$M/servers/web1.example.com/memory.md" <<EOF
# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- SSH port: 2222
- Last connected: $TODAY
- Database password: in /etc/app/db.env (0600 root)
EOF
cat > "$M/servers/web1.example.com/changelog.log" <<'EOF'
[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green <script>alert(1)</script>
— because deploys interrupted visitors
  Detail: templated app@.service (blue :4003,
  green :4005).
  Rollback: re-enable app.service, restore vhost
  backup from /var/backups/jolitz/.
  Verify: 135 probes, all 200.
  Flags: migrations must now stay
  backward-compatible.
[2026-06-12 08:00] [alice as root] nginx reloaded (auto, policy)
EOF
cat > "$M/servers/web1.example.com/rules.md" <<'EOF'
## Replace: Firewall
nftables only.
EOF
ln -s web1.example.com "$M/servers/www.example.com"

cat > "$M/servers/db1.example.com/memory.md" <<'EOF'
# db1.example.com
- IP: 203.0.113.20
- OS: Debian 11 (Bullseye)
- Distro family: debian
- Tags: prod, Database
- Last connected: 2025-01-01
EOF
cat > "$M/servers/db1.example.com/todo.md" <<'EOF'
# To-do
- [x] Back up pg_hba.conf
- [ ] Upgrade to Debian 12
EOF
cat > "$M/servers/db1.example.com/changelog.log" <<'EOF'
[2025-01-01 10:00] [bob as root] PostgreSQL 15 installed
[2025-01-02] forgot the timestamp format
EOF

cat > "$M/servers/mac.local/memory.md" <<'EOF'
# mac.local
- OS: macOS 15.1 Sequoia
- Mode: local
- Last connected: yesterday
EOF

cat > "$M/eol/debian.json" <<'EOF'
{"result":{"name":"debian","releases":[
 {"name":"12","eolFrom":"2099-06-30"},
 {"name":"11","eolFrom":"2001-08-31"}]}}
EOF
echo 2026-10-28 > "$M/eol/debian.fetched"

cat > "$M/audits/2026-10-01-0900.json" <<'EOF'
{"date":"2026-10-01 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"}}}
EOF
cat > "$M/audits/2026-10-28-0900.json" <<'EOF'
{"date":"2026-10-28 09:00","hosts":{
 "web1.example.com":{"sshd.PermitRootLogin":"no","time.ntp":"chrony"},
 "db1.example.com":{"sshd.PermitRootLogin":"yes","time.ntp":"unknown(needs-root)"}},
 "skipped":{"mac.local":"macOS not covered"}}
EOF

# --- lint ----------------------------------------------------
"$LINT" -d "$M" > "$T/lint.out"; rc=$?
ok $(( rc != 0 )) "lint exits 0 with warnings only (got $rc)"
has "$T/lint.out" 'WARN  db1.example.com: memory.md: tag "Database"' \
  "lint flags an uppercase tag"
has "$T/lint.out" 'WARN  db1.example.com: changelog.log: entry without [YYYY-MM-DD HH:MM] on line 2' \
  "lint flags a changelog entry without timestamp"
has "$T/lint.out" 'WARN  mac.local: memory.md: Last connected "yesterday"' \
  "lint flags a date that is not YYYY-MM-DD"
hasnt "$T/lint.out" 'www.example.com' "lint skips an alias symlink"
hasnt "$T/lint.out" 'ERROR' "a password location is not a secret"
has "$T/lint.out" 'memory lint: 3 hosts, 0 errors, 3 warnings' \
  "lint summary counts hosts and warnings"
"$LINT" -d "$M" --strict > /dev/null; rc=$?
ok $(( rc != 1 )) "lint --strict fails on warnings (got $rc)"
"$LINT" -d "$M" web1.example.com > "$T/lint1.out"; rc=$?
ok $(( rc != 0 )) "lint of one clean host exits 0 (got $rc)"
has "$T/lint1.out" 'memory lint: 1 hosts, 0 errors, 0 warnings' \
  "lint takes host arguments"

# Secrets: built at runtime so no token sits in the repo.
S="$T/secret"
cp -R "$M" "$S"
P="gh""p_"
TOK="${P}abcdefghijklmnopqrstuvwxyz0123456789"
KEY="-----BEGIN OPENSSH ""PRIVATE KEY-----"
printf -- '- Deploy token: %s\n' "$TOK" \
  >> "$S/servers/web1.example.com/memory.md"
printf '  Detail: %s\n' "$KEY" >> "$S/servers/db1.example.com/changelog.log"
printf 'DB_PASSWORD=hunter2\n' > "$S/servers/db1.example.com/notes.txt"
printf 'mysql://app:s3cret@db1/app\n' >> "$S/network.md"
"$LINT" -d "$S" > "$T/sec.out"; rc=$?
ok $(( rc != 1 )) "lint exits 1 on a secret (got $rc)"
has "$T/sec.out" 'ERROR web1.example.com: servers/web1.example.com/memory.md:9: looks like a secret' \
  "lint names file and line of a token"
has "$T/sec.out" 'ERROR db1.example.com: servers/db1.example.com/changelog.log:3:' \
  "lint finds a private key header"
has "$T/sec.out" 'ERROR db1.example.com: servers/db1.example.com/notes.txt:1:' \
  "lint finds a password assignment in any host file"
has "$T/sec.out" 'ERROR memory: network.md:1:' \
  "lint finds a password in a URL outside the server folders"
hasnt "$T/sec.out" "$TOK" "lint never prints the secret value"
hasnt "$T/sec.out" 'hunter2' "lint never prints a password"

"$OVERVIEW" -d "$S" > "$T/sov.out" 2>&1; rc=$?
ok $(( rc != 1 )) "overview refuses to build over a secret (got $rc)"
[ ! -e "$S/overview/index.html" ]; ok $? "no page is written over a secret"

# --- overview: html ------------------------------------------
"$OVERVIEW" -d "$M" > "$T/ov.out" 2>&1; rc=$?
ok $(( rc != 0 )) "overview builds (got $rc: $(cat "$T/ov.out"))"
I="$M/overview/index.html"
[ -f "$I" ]; ok $? "index.html exists"
[ -f "$M/overview/hosts/web1.example.com.html" ]; ok $? "host page exists"
[ ! -e "$M/overview/hosts/www.example.com.html" ]; ok $? "no page for an alias"
has "$I" '<a href="hosts/db1.example.com.html">db1.example.com</a>' \
  "index links the host page"
has "$I" 'also www.example.com' "index shows the alias"
has "$I" 'ended 2001-08-31' "EOL: an ended release"
has "$I" 'until 2099-06-30' "EOL: a supported release"
has "$I" 'no data (run with --eol)' "EOL: a product without cache"
has "$I" 'data-filter="prod"' "tag filter offers prod"
has "$I" 'Upgrade to Debian 12' "open to-do listed"
hasnt "$I" 'Back up pg_hba.conf' "done to-do not listed"
has "$I" 'own rules.md' "rule override marked"
has "$I" 'ssh port 2222' "ssh port shown"
has "$I" '<code>sshd.PermitRootLogin</code> changed from <code>no</code> to <code>yes</code>' \
  "audit change since last snapshot"
has "$I" '1 settings differ' "needs-root values are not drift"
has "$I" 'endoflife.date, fetched debian 2026-10-28' "EOL source and date"
has "$I" 'line 2' "lint warning on the page"
W="$M/overview/hosts/web1.example.com.html"
has "$W" '&lt;script&gt;alert(1)&lt;/script&gt;' "headline is escaped"
hasnt "$W" '<script>alert(1)' "no raw markup from memory"
has "$W" 'migrations must now stay backward-compatible' \
  "host page lists standing constraints"
has "$W" 'Rollback: re-enable app.service' "host page keeps rollback detail"
has "$W" '- Database password: in /etc/app/db.env (0600 root)' \
  "host page shows memory.md"
has "$W" 'nftables only.' "host page shows rules.md"

# --- overview: text and tag ----------------------------------
"$OVERVIEW" -d "$M" --text --days 100000 > "$T/txt.out"; rc=$?
ok $(( rc != 0 )) "text mode runs (got $rc)"
has "$T/txt.out" 'Hosts: 3, open to-dos: 1' "text: counts"
has "$T/txt.out" '  db1.example.com: Debian 11 (Bullseye), ended 2001-08-31' \
  "text: EOL line"
has "$T/txt.out" '  db1.example.com: last 2025-01-01' "text: stale host"
has "$T/txt.out" '  db1.example.com sshd.PermitRootLogin: no -> yes' \
  "text: audit change"
has "$T/txt.out" 'Memory lint: 3 warnings' "text: lint count"
"$OVERVIEW" -d "$M" --text --tag web > "$T/tag.out"; rc=$?
ok $(( rc != 0 )) "tag filter runs (got $rc)"
has "$T/tag.out" 'Hosts: 1,' "tag filter keeps one host"
hasnt "$T/tag.out" 'db1.example.com:' "tag filter drops other hosts"
has "$T/tag.out" 'Memory lint: 0 warnings' "an empty count prints 0"
has "$T/tag.out" 'Fleet audit 2026-10-28 09:00: 0 settings differ' \
  "tag filter narrows the audit"

# --- guard log -----------------------------------------------
mkdir -p "$HOME/.cache/jolitz"
printf '%s\tbash\tpowering off a machine\thalt\n' "${TODAY}T10:00:00Z" \
  > "$HOME/.cache/jolitz/guard.log"
"$OVERVIEW" -d "$M" > /dev/null 2>&1
has "$I" 'Guard denials, last 30 days' "guard log section appears"
has "$I" '<td>1</td><td>'"$TODAY"'</td><td>powering off a machine</td>' \
  "guard log counted by reason"
"$OVERVIEW" -d "$M" --text > "$T/g.out"
has "$T/g.out" '  1x powering off a machine' "text: guard denials"

echo "memory-tools tests: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

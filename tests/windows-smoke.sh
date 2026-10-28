#!/bin/sh
# windows-smoke.sh — the guard as Claude Code runs it on native
# Windows: in Git Bash, started as `bash <hook>`, with hook input
# that carries C:\ paths and a transcript_path under ~\.claude.
# A short subset of guard-taboos-test.sh, fast enough for every
# pull request; the full matrix is slow under Git Bash and runs
# on main. Needs jq to build the fixtures.
set -u

HOOK=$(cd "$(dirname "$0")/.." && pwd)/.claude/hooks/guard-taboos.sh
W='C:\Users\max'
PASS=0
FAIL=0

result() {
  # $1 expected, $2 hook output, $3 label
  if printf '%s' "$2" | grep -q '"permissionDecision":"deny"'; then
    GOT=deny
  else
    GOT=pass
  fi
  if [ "$GOT" = "$1" ]; then
    PASS=$((PASS + 1))
  else
    FAIL=$((FAIL + 1))
    echo "FAIL [$1, got $GOT]: $3"
  fi
}

bash_case() {
  # $1 expected, $2 cwd, $3 command
  OUT=$(jq -nc --arg c "$2" --arg x "$3" --arg t "$W" '{
      session_id:"s", hook_event_name:"PreToolUse",
      transcript_path:($t + "\\.claude\\projects\\p\\s.jsonl"),
      cwd:$c, tool_name:"Bash", tool_input:{command:$x}}' \
    | env -u JOLITZ_GUARD_DISABLE bash "$HOOK")
  result "$1" "$OUT" "bash (cwd $2): $3"
}

file_case() {
  # $1 expected, $2 cwd, $3 file_path
  OUT=$(jq -nc --arg c "$2" --arg p "$3" --arg t "$W" '{
      session_id:"s", hook_event_name:"PreToolUse",
      transcript_path:($t + "\\.claude\\projects\\p\\s.jsonl"),
      cwd:$c, tool_name:"Edit",
      tool_input:{file_path:$p, old_string:"a", new_string:"b"}}' \
    | env -u JOLITZ_GUARD_DISABLE bash "$HOOK" file)
  result "$1" "$OUT" "edit (cwd $2): $3"
}

J="$W\\jolitz"

# Everyday work passes.
bash_case pass "$J" 'ls -la'
bash_case pass "$J" 'git status'
bash_case pass "$J" 'ssh -o BatchMode=yes root@web1.example.com uptime'
bash_case pass "$J" 'ssh root@web1.example.com "df -h"'
bash_case pass "$J" 'bin/jolitz-overview --text'
bash_case pass "$J" "cat '$W\\.ssh\\id_ed25519.pub'"
file_case pass "$J" "$J\\memory\\servers\\web1.example.com\\memory.md"
file_case pass "$J" 'memory\notes.md'

# Taboos stay blocked.
bash_case deny "$J" 'ssh root@web1.example.com "mkfs.ext4 /dev/sdb1"'
bash_case deny "$J" 'ssh root@web1.example.com "poweroff"'
bash_case deny "$J" "ssh root@web1.example.com 'echo x >> /etc/ssh/sshd_config'"
bash_case deny "$J" 'ssh root@web1.example.com "echo allow_changes = true > /etc/jolitz.conf"'
bash_case deny "$J" 'export JOLITZ_GUARD_DISABLE=1'

# The guard's own files and local keys, in Windows spelling.
file_case deny "$J" "$J\\.claude\\settings.json"
file_case deny "$J" "$J\\.claude\\settings.local.json"
file_case deny "$J" "$J\\.CLAUDE\\hooks\\guard-taboos.sh"
file_case deny "$J" 'C:/Users/max/jolitz/.claude/settings.json'
file_case deny "$J" "$W\\.ssh\\id_ed25519"
file_case deny "$J\\.claude" 'settings.json'
bash_case deny "$J\\.claude" 'echo x > settings.json'
bash_case deny "$J" "chmod 000 '$W\\.ssh\\id_ed25519'"
bash_case deny "$J" 'echo x > /c/Users/max/jolitz/.claude/settings.json'

echo "windows smoke: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

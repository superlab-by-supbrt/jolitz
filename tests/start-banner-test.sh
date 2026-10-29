#!/bin/sh
# start-banner-test.sh — fixture tests for the SessionStart
# hook .claude/hooks/banner.sh: the welcome on the first
# start, the banner with the version after that, valid JSON
# either way. Builds a throwaway project dir; no network.
#   sh tests/start-banner-test.sh

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
HOOK="$REPO_DIR/.claude/hooks/banner.sh"
PASS=0
FAIL=0

ok() {
  if [ "$1" -eq 0 ]; then PASS=$((PASS + 1))
  else FAIL=$((FAIL + 1)); echo "FAIL: $2"; fi
}

T=$(mktemp -d "${TMPDIR:-/tmp}/start-banner-test.XXXXXX") || exit 1
trap 'rm -rf "$T"' EXIT
mkdir -p "$T/assets" "$T/memory"
cp "$REPO_DIR/assets/welcome.txt" "$REPO_DIR/assets/banner.txt" "$T/assets/"
echo '3.3.3-jolitz.99' > "$T/VERSION"
msg() { CLAUDE_PROJECT_DIR="$T" sh "$HOOK" | jq -r '.systemMessage'; }

# First start: no memory/user.md.
OUT=$(CLAUDE_PROJECT_DIR="$T" sh "$HOOK")
printf '%s' "$OUT" | jq -e '.systemMessage | type == "string"' >/dev/null
ok $? "first start: valid JSON with a systemMessage"
msg | grep -q 'First steps'
ok $? "first start: the welcome"
[ "$(msg | sed 1d)" = "$(cat "$REPO_DIR/assets/welcome.txt")" ]
ok $? "first start: the welcome verbatim, backslashes and quotes intact"

# Every later start.
: > "$T/memory/user.md"
msg | grep -q '3.3.3-jolitz.99$'
ok $? "later start: the banner carries the version"
! msg | grep -q '{VERSION}'
ok $? "later start: no placeholder left"
msg | grep -q '^  /jolitz-housekeeping <host>'
ok $? "later start: the commands"
! msg | grep -q 'First steps'
ok $? "later start: not the welcome"
[ "$(msg | sed 1d)" = "$(sed 's/{VERSION}/3.3.3-jolitz.99/' "$REPO_DIR/assets/banner.txt")" ]
ok $? "later start: the banner verbatim, line for line"

# A VERSION with odd characters cannot break the JSON.
printf '1.0"}\\x\n' > "$T/VERSION"
CLAUDE_PROJECT_DIR="$T" sh "$HOOK" | jq -e . >/dev/null
ok $? "odd VERSION: still valid JSON"

# Missing banner: silent, exit 0.
rm "$T/assets/banner.txt"
OUT=$(CLAUDE_PROJECT_DIR="$T" sh "$HOOK"); rc=$?
[ "$rc" -eq 0 ] && [ -z "$OUT" ]
ok $? "missing banner: exit 0, nothing printed"

# Registered on startup in settings.json.
jq -e '[.hooks.SessionStart[] | select(.matcher == "startup") | .hooks[]
  | select(.command | test("banner\\.sh"))] | length == 1' \
  < "$REPO_DIR/.claude/settings.json" >/dev/null
ok $? "settings.json registers banner.sh on startup"

echo "start-banner tests: $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

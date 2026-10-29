#!/bin/sh
# SessionStart hook: shows the start screen before the first
# prompt. The model may speak only after the user did, so the
# screen comes from here, as a systemMessage that Claude Code
# prints under "SessionStart:startup says:". It never reaches
# the model's context as an instruction and changes nothing.
#
# First start (memory/user.md missing): assets/welcome.txt.
# Every later start: assets/banner.txt with {VERSION} filled
# in. No jq needed; awk builds the JSON string.

D=${CLAUDE_PROJECT_DIR:-.}
if [ -f "$D/memory/user.md" ]; then
  F="$D/assets/banner.txt"
else
  F="$D/assets/welcome.txt"
fi
[ -r "$F" ] || exit 0
V=$(head -n 1 "$D/VERSION" 2>/dev/null | tr -cd '[:alnum:]._-')
awk -v v="$V" '
  BEGIN { printf "{\"systemMessage\":\"\\n" }
  {
    gsub(/\{VERSION\}/, v)
    gsub(/\\/, "\\\\"); gsub(/"/, "\\\"")
    gsub(/\t/, "    "); gsub(/\r/, "")
    printf "%s\\n", $0
  }
  END { print "\"}" }
' "$F"
exit 0

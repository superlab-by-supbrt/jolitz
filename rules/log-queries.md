# Log Queries

A busy server writes millions of journal lines a
day. A query that reads all of them and throws
most away runs into the tool's time limit, and the
answer is lost with it. **Bound every log read by
time or count, and filter where the log is stored,
not in a pipe behind it.**

## journald

- **Count with `-n`, never with `| tail`.**
  `journalctl -t heinzel -n 20` reads from the end
  of the journal. `journalctl -t heinzel | tail -n
  20` formats every entry since the journal began
  and keeps twenty.
- **Filter by field.** `-t <identifier>`,
  `-u <unit>`, `_UID=<uid>`, `_COMM=<name>`,
  `-p warning`. The journal indexes these and skips
  everything else.
- **Search text with `-g`, never with `| grep`.**
  `journalctl --since "48 hours ago" -g heinzel -n
  20`. A pattern in lowercase matches regardless of
  case. A system without pattern support says so
  ("Compiled without pattern matching support");
  only then pipe, with `--since` as tight as the
  question allows and `grep -m <n>` so it stops
  early.
- **Always give `--since` or `-n`.** An unfiltered
  `journalctl` with neither reads the whole
  journal.
- `--no-pager` always; `-q` hides the header lines.
  `-o cat` drops timestamp and host when only the
  messages matter.

`grep heinzel` over the whole journal does not find
what Heinzel *wrote*: it finds the logins and sudo
lines of the `heinzel` account. What Heinzel writes
carries the identifier `heinzel` (`-t heinzel`, see
`rules/activity-check.md`).

## Log files

- `tail -n <n> <file>` and `grep -m <n>` for the
  newest lines; never `cat <file> | grep`.
- Rotated files only when the question reaches
  back that far, named one by one, newest first.
  No `zgrep` over `/var/log/*.gz`.

## macOS

`log show` is slow even on a small window. Always
give `--last <n>m` or `--start`, and a
`--predicate`; see `rules/macos.md`.

## One Answer Is Enough

When a precise query has answered the question
(`-t heinzel --since "48 hours ago"` returns
nothing), report it. Do not follow it with a broad
cross-check that reads the whole log. If you need
one, make it as precise as the first.

A query that hit the time limit is not repeated
unchanged and not widened. Narrow it: a shorter
`--since`, a field filter, `-n`.

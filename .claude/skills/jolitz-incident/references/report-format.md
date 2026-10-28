# Incident Report Format

One report per incident, in the user's language. Facts, one
per line (`CLAUDE.md` → Talking to Humans). Nothing before
the header, nothing after the last section.

```
## Incident: web1.example.com

Date: 2026-10-28 09:52 | Symptom: 502 on https://example.com
OS: Debian 13 | Up: 41 days | Load: 0.8 (4 cores) | Disk /: 58%

### Finding

nginx answers 502 because app.service exited at 09:41 with
"address already in use" after the deploy at 09:40.

### Evidence

09:40  changelog   [alice as root] app deployed 2.3.1
09:41  journal     app.service: bind 127.0.0.1:4000: address in use
09:41  nginx log   connect() failed (111) upstream 127.0.0.1:4000
09:52  ss          :4000 held by pid 1234, /opt/app/releases/2.3.0/app

### Proposed fix

Stop pid 1234 (the 2.3.0 binary), start app.service, confirm
200 through nginx. Not run yet.
Way back: 2.3.0 stays in /opt/app/releases/; symlink current
back and start app.service.

### What changed recently

2026-10-28 09:40  app deployed 2.3.1 (alice)
2026-10-26 06:14  unattended-upgrades: libssl3 (journal, no changelog)
```

## Rules

- **Header**: date, the symptom in the user's words, then the
  facts from the common batch that bear on it. Nothing else.
- **Finding**: one sentence, cause and effect, verified
  against the live system. Not verified: start it with
  "Unverified:" and say what is missing. Nothing found:
  "No finding — ruled out: <list>", and the Evidence section
  shows what was checked.
- **Evidence**: one line per fact, `HH:MM  source  line`,
  in time order. The source is `changelog`, `journal`,
  `<file>`, or the command. Quote the log line, shortened,
  never a credential (`rules/secrets.md`).
- **Proposed fix**: what would run, in prose, ending with
  "Not run yet." and the `Way back:` line
  (`rules/way-back.md`). Several options: one line each, at
  most three, the recommended one first. A reload that may
  auto-proceed (`rules/service-reload.md`) is still listed
  here first; it runs after the report, not before.
- **What changed recently**: the last 7 days from
  `bin/jolitz-log`, the host changelog and the journal.
  A journal change with no changelog entry is marked
  `(no changelog)`: it was made outside jolitz.
- After the fix, one more line, not a new report:
  `Fixed 10:03 — app.service active, 200 from nginx` or
  `Not fixed — <what the probe shows now>`.

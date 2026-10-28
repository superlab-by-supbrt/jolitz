---
name: jolitz-incident
argument-hint: "<hostname> [symptom]"
description: Triage a live problem on one server — "502 on
  <host>", "load is high", "disk full", "is <host> down?",
  "the site hangs". Read-only. Builds a timeline from the
  journal, lists what changed recently (bin/jolitz-log and the
  host changelog), checks service state, then reports one
  Finding with Evidence, a Proposed fix with its Way back, and
  What changed recently. Changes nothing without the user's
  OK. Use when the user reports a symptom on a host. Do NOT
  auto-invoke for routine checks — that is jolitz-housekeeping.
---

# jolitz-incident

Triage for one host with a symptom. The skill finds the cause
and proposes the fix. It does not apply it: every change waits
for the user's OK, with its `Way back:` line
(`rules/way-back.md`). **Never run automatically** — only when
the user names a symptom.

## When to use

- "502 on web1", "the site is slow", "load on db1 is 40"
- "disk full on mail1", "is app2 down?", "nginx won't start"
- after a monitoring alert the user pastes

Do NOT use for "check server <host>" (quick query) or "run a
health check" (housekeeping). A problem on several hosts at
once is a fleet question first: `jolitz-fleet-audit`.

## Workflow

1. **Pipeline.** Run `rules/first-connection.md` in full before
   the first probe. There is no incident exception: the
   pipeline is what tells you the host is read-only, has a
   non-standard port or was touched an hour ago.
2. **Load overrides.** `memory/custom-rules/jolitz-incident.md`
   if present, then `memory/servers/<hostname>/memory.md`,
   `rules.md` and `todo.md`. An open job in `todo.md`
   (`rules/ssh-connections.md` → 4) is a suspect before any
   probe runs.
3. **Name the symptom.** Pick the branch in
   `references/triage.md`: unreachable, 5xx, load, disk,
   memory, one service. A vague symptom ("it is slow") gets the
   common batch first, then the branch its output points at.
4. **Timeline, read-only.** Journal since the symptom started,
   bounded per `rules/log-queries.md`: `--since`, `-n`, `-p`,
   `-g`, never a whole journal through `grep`. jolitz's own
   entries first (`-t jolitz`), then the kernel and the
   service. On FreeBSD and NetBSD `tail -n` on `/var/log/`;
   on macOS `log show --last` with a predicate.
5. **What changed.** Locally, `bin/jolitz-log --host <hostname>
   --since <date 7 days ago>` and the host's `changelog.log`
   (`Flags:` and `Rollback:` lines). On the host, the package
   manager's history and files under `/etc` newer than the
   symptom. Then `memory/audits/`: the newest snapshot's
   "Changed since" entries for this host.
6. **Service state.** The probes of the branch, in two or three
   parallel batches. A probe that merely mentions a guarded
   path runs in a call of its own (`CLAUDE.md` → Critical
   Safety Rules).
7. **Verify the finding** against the live system before
   writing it down (`rules/verify-before-reporting.md`): the
   cause is shown, not assumed. An unproven cause is reported
   as "unverified".
8. **Report** in the format of `references/report-format.md`.
   The report ends with the proposed fix and its `Way back:`
   line. Nothing has run yet.
9. **Fix only after OK.** A reload under
   `rules/service-reload.md` may auto-proceed when its config
   test passes; a restart, a firewall change, a reboot, a
   delete and a credential rotation ask. After the fix, repeat
   the probe that showed the symptom and report one line.
10. **Record.** Journal headline and local changelog per
    `rules/changelog.md` (the `Rollback:` line before the
    fix), `memory.md` when a fact changed, `todo.md` for what
    stays open. A session that changed nothing logs
    `read-only: incident triage <symptom>`.

## References

Read on demand:

- `references/triage.md` — the common batch and one branch
  per symptom, read-only probes only.
- `references/report-format.md` — `## Incident: <host>` with
  Finding, Evidence, Proposed fix, What changed recently.

## Scope and limits

- One host. Drift between hosts is `jolitz-fleet-audit`;
  routine health is `jolitz-housekeeping`.
- Read-only until the user says yes. No "quick fix" because
  the site is down: the question with the `Way back:` line
  takes one message.
- A host that does not answer: `rules/ssh-unreachable.md`
  decides what to try; the skill does not scan ports or
  guess at the provider console.
- jolitz has no monitoring history. The timeline is what the
  journal and the changelog hold; say so when both are thin.
- Secrets in logs (a password in a failing connection
  string) are reported as a leak, never quoted
  (`rules/secrets.md`).

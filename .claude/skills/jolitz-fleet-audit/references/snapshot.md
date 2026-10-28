# Fleet Audit Snapshot

One file per audit run, written locally after the
report: `memory/audits/<YYYY-MM-DD-HHMM>.json`. It is
the history the next audit compares against, and the
source of the audit table in `bin/jolitz-overview`.
It never leaves the operator's machine; in team mode
it is shared like server memory.

## Format

```json
{
  "date": "2026-10-28 14:05",
  "hosts": {
    "web1.example.com": {
      "ua.Unattended-Upgrade": "1",
      "ua.Automatic-Reboot": "true",
      "sshd.permitrootlogin": "prohibit-password",
      "sshd.passwordauthentication": "no",
      "firewall.tool": "ufw",
      "firewall.state": "active",
      "mta.package": "postfix",
      "time.service": "chrony",
      "reboot.required": "no"
    },
    "db1.example.com": {
      "sshd.permitrootlogin": "unknown(needs-root)"
    }
  },
  "skipped": {
    "mac.local": "macOS not yet supported",
    "old.example.com": "unreachable"
  }
}
```

- Keys are `<category>.<setting>`. Categories:
  `ua`, `sshd`, `firewall`, `mta`, `time`, `reboot`,
  matching the sections of `references/probes.md`.
  Settings are the row labels of the report table;
  sshd keys in lower case, as `sshd -T` printed them
  before OpenSSH 10.4 (10.5 for every key), so snapshots
  stay comparable.
- Values are strings, exactly as rendered in the
  table. `(unset)` for a setting that is not set,
  `unknown(needs-root)` for a probe that lacked
  privileges. Both stay out of drift.
- Use the same key for the same setting in every run.
  A renamed key reads as a removed and a new setting.
- No secrets. A probe output that holds a credential
  (an SMTP relay password, a token in an MTA config)
  is recorded as `set`, never by value
  (`rules/secrets.md`).
- Write the file with the file-edit tool, then run
  `jq empty` on it. A file that does not parse is
  invisible to the overview and the next comparison.

## Comparing

Before writing the new file, read the newest file in
`memory/audits/`. For every host and key present in
both, list the ones whose value changed:

```
### Changed since 2026-10-01 09:00

| Host             | Setting              | Was | Now |
|------------------|----------------------|-----|-----|
| db1.example.com  | sshd.permitrootlogin | no  | yes |
```

Then look each one up in that host's
`changelog.log`. A change with no entry was made
outside jolitz: say so in the report.

Keep snapshots for two years, like the changelog.
Delete older files when writing a new one.

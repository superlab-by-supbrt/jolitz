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
      "reboot.required": "no",
      "certs.example.com.days": "61",
      "certs.mail.example.com.days": "12"
    },
    "db1.example.com": {
      "sshd.permitrootlogin": "unknown(needs-root)"
    },
    "bsd1.example.com": {
      "base.userland": "14.3-RELEASE-p2",
      "base.kernel": "14.3-RELEASE-p2",
      "pkg.vulnerable": "0",
      "pkg.pending": "4",
      "pkg.branch": "quarterly",
      "firewall.tool": "pf",
      "firewall.state": "enabled",
      "time.service": "ntpd",
      "time.synced": "yes",
      "services.enabled": "9",
      "reboot.required": "no"
    },
    "mac.local": {
      "macos.version": "26.1",
      "macos.updates": "0",
      "macos.auto_check": "1",
      "macos.critical_updates": "1",
      "firewall.tool": "applicationfirewall",
      "firewall.state": "on",
      "firewall.stealth": "off",
      "time.service": "networktime",
      "time.synced": "yes",
      "sshd.remotelogin": "On",
      "reboot.required": "(unset)"
    }
  },
  "skipped": {
    "nb1.example.com": "NetBSD not yet covered",
    "old.example.com": "unreachable"
  }
}
```

- Keys are `<category>.<setting>`. Categories:
  `ua`, `sshd`, `firewall`, `mta`, `time`, `reboot`,
  `certs` (`certs.<name>.days`, section 7), `base`, `pkg`
  and `services` (FreeBSD, section 8), `macos` (section 9),
  matching the sections of `references/probes.md`. The
  shared categories `sshd`, `firewall`, `time` and `reboot`
  hold whatever the host's OS section produced.
  Settings are the row labels of the report table;
  sshd keys in lower case, as `sshd -T` printed them
  before OpenSSH 10.4 (10.5 for every key), so snapshots
  stay comparable. The to-do lines in `todo.md` quote
  these keys (`references/output-format.md`).
- Values are strings, exactly as rendered in the
  table. `(unset)` for a setting that is not set,
  `unknown(needs-root)` for a probe that lacked
  privileges, `n/a` for a row the host's OS cannot
  answer. All three stay out of drift. A certificate
  row holds the days as a number, `expired` or `none`.
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

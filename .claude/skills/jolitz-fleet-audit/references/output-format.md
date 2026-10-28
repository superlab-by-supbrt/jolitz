# Fleet Audit Output Format

Render one Markdown table per probe category, then a single
"Drift detected" section at the end. Keep the text scannable.

## Header

```
## Fleet Audit

Date: YYYY-MM-DD HH:MM
Hosts in scope: host1, host2, host3
Skipped: host4.example.com (no SSH user known)
```

## One table per category

Hosts are columns (sorted alphabetically). Settings are
rows. Use the literal value where short enough; otherwise
abbreviate to `(see drift below)` and detail it in the drift
section.

```
### Unattended-upgrades

| Setting                              | host1   | host2   | host3   |
|--------------------------------------|---------|---------|---------|
| Update-Package-Lists                 | 1       | 1       | 1       |
| Unattended-Upgrade                   | 1       | 1       | 1       |
| Origins covers -security pattern     | yes     | yes     | yes     |
| Mail                                 | ops@…   | ops@…   | ops@…   |
| MailReport                           | only-on-error | only-on-error | only-on-error |
| Automatic-Reboot                     | true    | true    | true    |
| Automatic-Reboot-Time                | (unset) | (unset) | (unset) |
| Remove-Unused-Kernel-Packages        | true    | true    | true    |
```

Mark rows that differ across columns with a `!` prefix in
the row label and keep the literal value in each cell — so
the eye lands on it:

```
| ! Automatic-Reboot-Time              | (unset) | (unset) | 05:30   |
```

## Needs-root cells

Probes that could not run for lack of privileges (see
"Privilege handling" in `references/probes.md`) return the
sentinel `unknown(needs-root)`. Render those cells as
`needs root`:

```
| PermitRootLogin     | prohibit-password | needs root |
```

Needs-root cells are **excluded from drift detection**: an
unreadable value is not a disagreement. Never mark such a
row with the `!` prefix or list it in "Drift detected"
solely because one cell reads `needs root` — the other
hosts' values may still drift against each other. Instead,
mention the affected hosts once in a one-line note under
the table, e.g.:

```
host2: sshd and firewall state unreadable (no root, no
passwordless sudo) — re-run with a privileged user for
full coverage.
```

## Drift detected

After all tables, a numbered list. Each entry: which hosts
disagree, what the setting means, suggested fix.

```
### Drift detected

1. **Automatic-Reboot-Time**: host3 sets "05:30", others
   leave it unset. Deferring the kernel reboot by ~22h
   leaves new userland running on the old kernel
   (vulnerability + ABI risk). Suggested fix: comment out
   the line on host3 to match the rest of the fleet.

2. **PasswordAuthentication**: host2 has it set to "yes",
   host1 and host3 have "no". Suggested fix: confirm
   intent on host2; if no policy reason, disable.

3. **Firewall tool**: host1 uses ufw, host2 has no
   firewall installed. Suggested fix: install ufw on
   host2 or document the exception in
   memory/servers/<host2-fqdn>/memory.md.
```

## Empty drift

If every probe agrees across the fleet, replace the section
with a single line:

```
### Drift detected

None — fleet is consistent across all audited categories.
```

## Skipped hosts

Mention skipped hosts at the top of the report (in the
header), and again at the bottom in a one-line footer so
they are not forgotten:

```
Skipped: host4.example.com (no SSH user known),
         host5.example.com (unreachable)
```

## Per-OS tables

Unattended-upgrades and MTA list the Linux hosts. The
FreeBSD and macOS tables (`references/probes.md` → 8, 9)
list the hosts of that OS. The sshd, firewall, time and
reboot tables carry every host with the values its OS
section produced (`pf` next to `ufw`, `applicationfirewall`
next to `nftables`). A row an OS cannot answer reads `n/a`
and stays out of drift, like `needs root`.

```
### FreeBSD

| Setting          | bsd1            | bsd2            |
|------------------|-----------------|-----------------|
| base.userland    | 14.3-RELEASE-p2 | 14.3-RELEASE-p2 |
| ! base.kernel    | 14.3-RELEASE-p2 | 14.3-RELEASE-p1 |
| ! pkg.branch     | quarterly       | latest          |
| pkg.vulnerable   | 0               | 0               |
| pkg.pending      | 4               | 11              |
```

## Certificates

One row per certificate, every host as a column; a cell is
the number of days, `expired`, `none` or `n/a`. The rows
differ by nature, so the `!` prefix is not used here; the
threshold makes the entry:

```
### Certificates

| Certificate        | web1 | web2 | bsd1 |
|--------------------|------|------|------|
| example.com        | 61   | 61   | n/a  |
| mail.example.com   | 12   | n/a  | n/a  |
```

Under 30 days the certificate gets its own numbered entry
in "Drift detected" (WARN; under 7 days or `expired`
CRITICAL) naming the renewal suspect, and a to-do line.

## To-do lines

Every entry of "Drift detected", and every certificate
under 30 days, is also written to
`memory/servers/<hostname>/todo.md` of each host it names,
with the agent's file-edit tool, after the report is out:

```
- [ ] audit: sshd.passwordauthentication is yes, fleet has no (2026-10-28)
- [ ] audit: certs.mail.example.com.days is 12, renewal timer inactive (2026-10-28)
```

Shape: `- [ ] audit: <key> is <value>, <fleet value or
suspect> (<audit date>)`. The key is the snapshot key from
`references/snapshot.md`, so the line can be found again.

- One line per key and host. Before writing, search the
  file for `audit: <key>`. An open `- [ ]` line exists:
  leave it, date included. A closed `- [x]` line exists and
  the drift is back: replace that line with the new open
  one. Nothing: append. Never a second line for one key.
- A later audit that finds the key consistent, or the
  certificate renewed, marks the line `- [x]`.
- No `todo.md` yet: create it with the heading `# To-do`
  and the line. `bin/jolitz-overview` counts the open lines
  and shows them per host.
- `needs root` and `n/a` cells never produce a line.
- No secret in a line: a value that is a credential is
  written as `set` (`rules/secrets.md`).

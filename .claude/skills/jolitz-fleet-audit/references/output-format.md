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

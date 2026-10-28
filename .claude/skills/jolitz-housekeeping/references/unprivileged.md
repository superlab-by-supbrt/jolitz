# Unprivileged Mode

When running in unprivileged mode (no sudo, no root SSH), run
every check that works as a regular user and skip those that
require root.

At the end of the report, add a section:

```
### Skipped (needs root)

- Pending security updates (apt-get update)
- Firewall status (ufw requires root)
- SSL certificate files (/etc/letsencrypt/)
```

List each skipped check with a brief reason.

Backup presence: most probes survive without root —
`command -v`, `systemctl list-timers`, and `/etc/cron.d` is
usually world-readable. The root crontab (`crontab -l` as
root) and `/var/spool/cron` are not; report those as
"skipped: needs root" and never escalate just for this
check.

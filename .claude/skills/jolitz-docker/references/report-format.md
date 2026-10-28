# Docker Report Format

One report per host. Facts, one per line, in the user's
language. The Issues section only when something is wrong.

```
## Docker: web1.example.com

Date: 2026-10-28 14:05
Engine: 29.4.2 | Backend: iptables | Compose projects: 2 | Firewall: ufw active

### Issues

WARN   db publishes 0.0.0.0:5432 past ufw (no DOCKER-USER rule)
WARN   worker restarting: 14 restarts in 24 h, last error "ECONNREFUSED redis:6379"
INFO   nginx:1.25 built 11 months ago (tag latest)

### Containers

| Container | Image                | State          | Restart        | Ports                  |
|-----------|----------------------|----------------|----------------|------------------------|
| app       | ghcr.io/x/app:2.3.1  | Up 6 days      | unless-stopped | 127.0.0.1:4000->4000   |
| db        | postgres:17.2        | Up 6 days      | unless-stopped | ! 0.0.0.0:5432->5432   |
| worker    | ghcr.io/x/app:2.3.1  | Restarting     | always         |                        |
| backup    | offen/docker-volume-backup:v2 | Exited (0) 4 h ago | no |                 |

### Projects

app      /srv/app/compose.yml          running(3)
monitor  /srv/monitor/compose.yml      running(2)
(none)   backup — started by hand, not in a project

### Disk

Images         30 (14 active)   36.5 GB   13.4 GB reclaimable
Containers     14 (13 active)   31.5 MB
Local volumes  10 (2 active)    5.1 GB    4.8 GB reclaimable
Build cache    283              21.0 GB   13.4 GB reclaimable
Docker root    /var/lib/docker on / (58% used)
```

## Rules

- **Header**: engine version, firewall backend, project
  count, the host firewall's state in one word.
- **Issues**: severity from `references/inventory.md` and
  `references/ports-firewall.md`; CRITICAL first, then WARN,
  then INFO. A public port gets its own line.
- **Containers**: every container, running or not. A public
  port carries a `!` in front; a port recorded as public in
  `memory.md` does not. Columns stay as shown.
- **Projects**: one line per compose project with its config
  file and the `STATUS` column of `docker compose ls`; a
  final line for containers outside any project.
- **Disk**: the four `docker system df` lines, then where
  the Docker root lives and how full that filesystem is.
- **Skipped (needs root)**: when `docker ps` or the firewall
  state could not be read, a last section naming what was
  skipped, as in `jolitz-housekeeping` →
  `references/unprivileged.md`.
- No proposal in the report. A prune, a rebind or a rule is
  a question after it, one line each, at most three, each
  with its `Way back:`.

---
name: jolitz-docker
argument-hint: "[hostname]"
description: Inventory Docker on a server — containers and
  compose projects, published ports against the firewall
  (DOCKER-USER), image age, restart policies, disk use
  (docker system df). Read-only unless the user asks for a
  change. Use when the user asks "what runs in Docker on
  <host>", "docker inventory", "which ports do the containers
  publish", "docker disk usage", "prune docker on <host>".
  Do NOT auto-invoke for "is container X up" — that is a quick
  query.
---

# jolitz-docker

What Docker runs on a host, what it exposes and what it
costs in disk. The inventory is read-only. A prune, a pull,
a restart or a port change is a separate step that asks
first, with its `Way back:` line. **Never run
automatically** — only on explicit user request.

## Workflow

1. **Pipeline.** `rules/first-connection.md` in full. Then
   the overrides: `memory/custom-rules/jolitz-docker.md` if
   present, the host's `memory.md` and `rules.md`. A port the
   memory records as "meant to be public" stays quiet in the
   report.
2. **Detect.** `command -v docker`; without it, `command -v
   podman` (reported, not inventoried: Podman is out of
   scope). `docker ps` needs root or the `docker` group:
   the privilege ladder from `jolitz-fleet-audit` →
   `references/probes.md` (root, `sudo -n`, else
   `skipped: needs root`). Unraid hosts: `rules/unraid.md`
   first, its Docker layout differs.
3. **Inventory** in one batch from `references/inventory.md`:
   engine and firewall backend, containers, compose projects,
   restart policies and counts, images, volumes, disk.
4. **Ports against the firewall** from
   `references/ports-firewall.md`: every published port,
   its bind address, the `DOCKER-USER` rules or the nftables
   table that covers it, the host firewall's state.
5. **Report** in the format of `references/report-format.md`.
6. **Memory.** Record the compose projects and the public
   ports the user confirmed in `memory.md` (one line per
   project; `- Services: docker` where the line exists), so
   the next run and `jolitz-housekeeping` stay quiet about
   them.
7. **Changes only on request**, each one asked with its way
   back (`rules/way-back.md`):
   - `docker compose pull` and `up -d` recreate containers:
     a restart (`rules/service-reload.md` asks). Way back:
     the previous image tag stays local until pruned; pin it
     in the compose file.
   - `docker system prune` removes stopped containers,
     unused networks and dangling images; with `-a` every
     unused image; with `--volumes` data. List what it would
     remove (`docker system df -v`) before asking; never
     `--volumes` without the volume list in the question.
     Way back for a pruned volume: none; say so.
   - Binding a port to `127.0.0.1` or a `DOCKER-USER` rule:
     a firewall change, `CLAUDE.md` → Firewall & network.
8. **Record.** `logger -t jolitz "read-only: docker
   inventory"` when nothing changed; otherwise the headline
   and the local changelog per `rules/changelog.md`.

## References

Read on demand:

- `references/inventory.md` — the read-only batch and how to
  read it (states, restart loops, image age, disk).
- `references/ports-firewall.md` — published ports, bind
  addresses, `DOCKER-USER`, the nftables backend, severity.
- `references/report-format.md` — the report.

## Scope and limits

- Docker Engine and Compose v2 (`docker compose`). Podman,
  Kubernetes and Swarm services are named when seen, not
  inventoried.
- `docker inspect` without `--format` prints `Config.Env`,
  and that is where passwords live. Always `--format`, never
  the whole JSON into the conversation; `.env` files are
  secrets (`rules/secrets.md`).
- Whether a newer image exists needs the registry; a pull
  downloads and is asked for. The report shows the local
  image's age only.
- macOS: Docker Desktop runs in a VM; the Application
  Firewall does not filter its ports. Report them as INFO.
  FreeBSD has no native Docker.

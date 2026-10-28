# Docker Inventory

All read-only. One SSH call, root or `docker` group. The
`--format` templates below are the fields the report needs;
check `docker ps --help` on the host when one is missing on
an old engine.

## The batch

```bash
docker info --format '{{.ServerVersion}} {{.FirewallBackend.Driver}}'
docker ps -a --format \
  '{{.Names}}|{{.Image}}|{{.Status}}|{{.RunningFor}}|{{.Ports}}'
for c in $(docker ps -aq); do
  docker inspect --format \
    '{{.Name}} {{.State.Status}} {{.HostConfig.RestartPolicy.Name}} restarts={{.RestartCount}}' \
    "$c"
done
docker compose ls -a
docker image ls --format '{{.Repository}}:{{.Tag}} {{.CreatedSince}} {{.Size}}'
docker volume ls --format '{{.Name}} {{.Driver}}'
docker system df
```

Per compose project from the `CONFIG FILES` column of
`docker compose ls`:

```bash
docker compose -f CONFIGFILE ps --format '{{.Name}} {{.Service}} {{.Status}}'
```

## Reading it

**States.** `Up` is fine; `Up (unhealthy)` has a failing
healthcheck; `Restarting` is a crash loop; `Exited (N)` with
a restart policy of `no` is a one-shot or a leftover. A high
`restarts=` next to `Up` is a loop that happens to be up
right now: WARN, with the count. The last lines of its log:

```bash
docker logs --tail 30 --since 24h CONTAINER 2>&1
```

Logs can hold credentials (a connection string in a stack
trace). Read them for the error, quote the error line only
(`rules/secrets.md`).

**Restart policies.** `unless-stopped` or `always` for a
service that should survive a reboot; `no` is INFO for a
long-running container and fine for a one-shot. `on-failure`
without a max count restarts forever.

**Image age.** `CreatedSince` is when the image was built,
not when it was pulled. Older than 180 days: INFO, with the
tag. A `latest` tag says nothing about the version: name the
digest-free fact ("tag latest, built 11 months ago") and
leave the judgement to the user. Whether a newer image
exists is a registry question; `docker compose pull` answers
it and downloads, so it asks first.

**Compose.** A container that no compose project claims
was started by hand; say so, it will not come back after
`docker compose up`. A project whose config file is missing
on disk (`compose ls` shows it, `ls` does not) cannot be
recreated: WARN.

**Volumes.** Named volumes hold the data; anonymous ones
(64 hex characters) usually belong to removed containers.
`docker system df -v` lists which container uses which. A
volume nobody uses is a prune candidate and a question,
never a default.

**Disk.** `docker system df` gives images, containers,
local volumes and build cache with the reclaimable share.
`RECLAIMABLE` for images is dangling and unused layers; for
volumes it is data of no running container. Report the four
lines as they are; the proposal to prune is a separate
question with the size it frees.

## Severity

- **CRITICAL** — a container the memory names as a service
  is not `Up`.
- **WARN** — a crash loop (`Restarting`, or `restarts=` above
  5 in 24 hours), an unhealthy container, a published port
  past the firewall (`references/ports-firewall.md`), a
  compose project without its config file, disk reclaimable
  above 10 GB or the Docker root filesystem above 85 %.
- **INFO** — image older than 180 days, restart policy `no`
  on a long-running container, a container outside any
  compose project, anonymous volumes.

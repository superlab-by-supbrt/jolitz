# Published Ports Against the Firewall

Docker rewrites the destination of a published port in the
`nat` table before the packet reaches the INPUT chain that
ufw and firewalld filter
(https://docs.docker.com/engine/network/packet-filtering-firewalls/).
`-p 8080:80` is reachable from the internet behind
`ufw default deny incoming`. The full reasoning and the
nftables backend: `jolitz-security` →
`references/firewall-nftables-docker.md`; this file is the
inventory's view of it.

## Probe

Root or `sudo -n`; the `iptables` lines are empty for a
normal user, which is not the same as "no rules" — then the
cell says `needs root`.

```bash
docker info --format '{{.FirewallBackend.Driver}}'
docker ps --format '{{.Names}} {{.Ports}}'
for c in iptables ip6tables; do $c -S DOCKER-USER 2>/dev/null; done
nft list chains 2>/dev/null | grep -B1 -e ^table -e "hook"
ufw status 2>/dev/null | head -n 1
firewall-cmd --state 2>/dev/null
```

`ss -tlnp` shows the same ports under `docker-proxy`
(`rules/port-check.md`); report each port once, here.

## Classify every published port

A `Ports` entry with `->` is published. Its bind address
decides:

- `127.0.0.1:` or `[::1]:` — local, behind a reverse proxy
  or for the host alone. OK.
- `0.0.0.0:`, `[::]:` or no address — public, unless one of:
  - a `DOCKER-USER` rule beyond `-N DOCKER-USER` (and an
    older engine's `-j RETURN`) limits the port or its
    sources; read the rule, name what it covers;
  - the engine's backend is `nftables` (Docker Engine 29,
    experimental, no `DOCKER-USER` chain): a table with a
    base chain on Docker's hooks in `nft list chains`
    carries the restriction, if any;
  - the host's `memory.md` records the port as meant to be
    public (a web frontend on 443). Then OK, and the
    report says "public by design".
- An entry without `->` (`8080/tcp`) is exposed inside the
  Docker network only, not published. Not a finding.

Databases (5432, 3306, 27017, 6379), admin interfaces (9000,
9090, 8080, 15672) and metrics (9100) on `0.0.0.0` are the
rows to flag first (`rules/best-practices.md` → Network &
Exposure).

## Severity

- **WARN** — "Docker publishes `<container>` `<port>` past
  the firewall" for each public port with no rule and no
  memory line. One line per port.
- **OK** — every published port local, restricted or
  recorded.
- **needs root** — the state could not be read; never
  "none".

## Proposed fixes (asked, never run by the inventory)

Two shapes, in order of preference, both firewall changes
under `CLAUDE.md` → Firewall & network:

1. **Bind to loopback.** In the compose file,
   `ports: "127.0.0.1:8080:80"` instead of `"8080:80"`,
   then `docker compose up -d` recreates the container (a
   restart: asks). A reverse proxy with TLS on 443 fronts
   it. Way back: the previous line, same `up -d`.
2. **Restrict in `DOCKER-USER`.** A rule inserted at the top
   of `DOCKER-USER` that drops the port for every source
   but the allowed ones. The shape is in Docker's own
   documentation (link above); check `iptables --help` and
   that documentation on the host before writing one. A
   live rule does not survive a reboot on its own: find how
   this host persists rules (`iptables-persistent`, ufw's
   `after.rules`, firewalld direct rules) before adding it,
   and say so in the question. Way back: delete the same
   rule with `-D`.

Neither touches the SSH port, so no rollback timer is
needed; a rule that could (an interface-wide drop) gets one
(`rules/rollback-timer.md`).

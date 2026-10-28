# Install the hub on a team host

The hub host is a server like any other: pipeline, class and
port check, backup, changelog, `Way back:`. The commands below
run on the host over SSH with the options of `CLAUDE.md` → SSH
Options, as root or via `rules/privilege-escalation.md`. Example
values throughout: host `ops.example.com`, URL
`https://hub.example.com`, version `3.3.3-jolitz.8`, first admin
`alice`. Replace them with the real ones.

## 1. Pipeline and facts

1. `rules/first-connection.md`, all ten steps.
2. Local: `cat VERSION` in the jolitz checkout. That is the
   release to install; hub and clients run the same version.
3. On the host: `uname -s` and `uname -m`. The release asset
   is `jolitz-hub_<os>_<arch>` with Go's names: `linux` or
   `freebsd`; `amd64` for `x86_64`, `arm64` for `aarch64` and
   `arm64`. The release page lists the builds; a combination
   that is not there stops here with that statement.
4. `rules/service-class-check.md`, phase 1: is a web server
   installed? That decides question 3 below. `jolitz-hub`
   itself belongs to no class.
5. `rules/port-check.md` for 7331 on `127.0.0.1`. Occupied: the
   rule's options; a free port replaces 7331 in every command
   below.
6. Does the host reach GitHub? `curl -fsSI
   https://github.com -o /dev/null` says so. Otherwise the
   download happens on the workstation, the checksum is verified
   there, and `rsync` carries the binary to the host
   (`rules/ssh-connections.md`).

## 2. Questions, in order

1. **URL.** "Which address will the team use? It becomes the
   hub's `--base-url` and the proxy's site name." The name must
   resolve to this host before a certificate can be issued;
   `getent ahostsv4 hub.example.com` on the host shows what it
   resolves to today.
2. **First admin.** "Whose admin account do I create first?"
   Usually the operator's own name from `memory/user.md`.
3. **Reverse proxy.** The hub speaks plain HTTP on
   `127.0.0.1:7331` and sets its session cookie `Secure` only
   behind TLS, so the proxy is where TLS lives. Options:
   - **(a) Caddy**, when no web server is on the host: one
     package, a two-line site block, certificates from Let's
     Encrypt by itself (needs 80 and 443 reachable from the
     internet and the DNS name pointing here).
   - **(b) the web server already there** (nginx, Caddy,
     Apache): a server block the skill shows before writing;
     the certificate is the one the host already has, or the
     one the host's certbot issues. Ask which.
   - **(c) none**: the hub stays on `127.0.0.1:7331`; the team
     reaches it through a VPN or an SSH tunnel with
     `http://127.0.0.1:7331` as the URL. The UI cookie is then
     not `Secure`; say so.
4. **Firewall** (`CLAUDE.md` → Firewall Awareness). With a
   proxy: open 443 (and 80 for Caddy's certificate) to all, to
   the team's network only, or no change. Recommend the team
   network when the hub is not meant for the internet. Port
   7331 is never opened; it listens on localhost.

## 3. The plan and its way back

Show one list before the first change:

1. download `jolitz-hub_linux_amd64` and `SHA256SUMS` from the
   release `v3.3.3-jolitz.8`, verify the checksum;
2. system user `jolitz-hub`, `/var/lib/jolitz-hub` mode 750;
3. `/usr/local/bin/jolitz-hub`;
4. the unit (or `rc.d` script), enabled and started;
5. the proxy block (option a or b), with backup and config test;
6. the firewall rule (if chosen), with a rollback timer;
7. the first admin token, shown once;
8. memory, changelog, journal.

`Way back: systemctl disable --now jolitz-hub, the unit and the
binary removed, the proxy block restored from
/var/backups/jolitz/, the firewall rule deleted;
/var/lib/jolitz-hub stays.` The `Rollback:` line of the local
changelog is written now (`rules/changelog.md`).

## 4. Download and verify

On the host (or on the workstation when step 1.6 said so):

```
mkdir -p /root/jolitz-scratch/jolitz-hub
cd /root/jolitz-scratch/jolitz-hub
curl -fsSL -o jolitz-hub_linux_amd64 \
  https://github.com/superlab-by-supbrt/jolitz/releases/download/v3.3.3-jolitz.8/jolitz-hub_linux_amd64
curl -fsSL -o SHA256SUMS \
  https://github.com/superlab-by-supbrt/jolitz/releases/download/v3.3.3-jolitz.8/SHA256SUMS
sha256sum --check --ignore-missing SHA256SUMS
```

The last line must print `jolitz-hub_linux_amd64: OK`. Anything
else (`FAILED`, no output, a curl error) stops the install
before anything moves; the files in the scratch directory are
removed. `--check` reads the digests from the file,
`--ignore-missing` skips the builds not downloaded (GNU
coreutils `sha256sum`; FreeBSD's base `sha256sum` takes the same
options, man page `sha256(1)`, both read 2026-10-28). On macOS
`shasum -a 256 --check --ignore-missing SHA256SUMS` does the
same for a workstation-side download.

## 5. User, directory, binary

Linux:

```
getent passwd jolitz-hub >/dev/null || useradd --system --user-group \
  --shell /usr/sbin/nologin --home-dir /var/lib/jolitz-hub \
  --no-create-home jolitz-hub
install -d -o jolitz-hub -g jolitz-hub -m 750 /var/lib/jolitz-hub
install -o root -g root -m 755 jolitz-hub_linux_amd64 /usr/local/bin/jolitz-hub
/usr/local/bin/jolitz-hub version
```

FreeBSD:

```
pw usershow jolitz-hub >/dev/null 2>&1 || pw useradd jolitz-hub \
  -d /var/lib/jolitz-hub -s /usr/sbin/nologin -c "jolitz team hub"
pw groupshow jolitz-hub
install -d -o jolitz-hub -g jolitz-hub -m 750 /var/lib/jolitz-hub
install -o root -g wheel -m 755 jolitz-hub_freebsd_amd64 /usr/local/bin/jolitz-hub
/usr/local/bin/jolitz-hub version
```

`version` must print the version from step 1.2. The data
directory holds `hub.db` and nothing else of value; it is the
one path the service may write.

## 6. The service

### systemd (Linux)

The unit is a new file; its way back is removing it. Written
through `tee` with a heredoc, then verified before it is
enabled (`rules/way-back.md` → Failures that show later):

```
tee /etc/systemd/system/jolitz-hub.service <<'EOF'
[Unit]
Description=jolitz team hub
After=network-online.target
Wants=network-online.target

[Service]
User=jolitz-hub
Group=jolitz-hub
ExecStart=/usr/local/bin/jolitz-hub serve --data /var/lib/jolitz-hub --listen 127.0.0.1:7331 --base-url https://hub.example.com
Restart=on-failure
DynamicUser=no
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/jolitz-hub

[Install]
WantedBy=multi-user.target
EOF
systemd-analyze verify /etc/systemd/system/jolitz-hub.service
systemctl daemon-reload
systemctl enable --now jolitz-hub
systemctl --no-pager status jolitz-hub
curl -fsS http://127.0.0.1:7331/api/v1/health
```

The hardening lines, checked against
https://www.freedesktop.org/software/systemd/man/latest/systemd.exec.html
on 2026-10-28: `DynamicUser=no` keeps the fixed account created
above (the default, written out so nobody turns it on and loses
the directory's ownership); `ProtectSystem=strict` mounts the
whole file system read-only for the service except `/dev`,
`/proc` and `/sys`; `ReadWritePaths=` allow-lists the data
directory inside that; `NoNewPrivileges=yes` stops the process
and its children from gaining privileges through `execve()`;
`ProtectHome=yes` hides `/home`, `/root` and `/run/user`;
`PrivateTmp=yes` gives it a private `/tmp` and `/var/tmp`.
With option (c), no proxy, `--base-url` is
`http://127.0.0.1:7331`. The health check answers
`{"ok":true,"version":"…"}` without a token; that version is the
one for `memory.md`.

### rc.d (FreeBSD)

Shape from the FreeBSD rc scripting article
(https://docs.freebsd.org/en/articles/rc-scripting/) and
`daemon(8)`, `rcorder(8)`, read 2026-10-28: the knob is
`${name}_enable`, the script name is a valid shell identifier,
`daemon -u` runs the program as the service user, `-p` writes
the child's pid file, `-f` points the standard descriptors at
`/dev/null` and, with `-S`, sends stdout and stderr to syslog
instead. The `# PROVIDE:`/`# REQUIRE:`/`# KEYWORD:` lines are
one `#` and one space each, consecutive. The script is written
to the scratch directory first and installed from there, the
way `rules/backups.md` writes a new config next to its backup
(the directory exists since step 4).

```
tee /root/jolitz-scratch/jolitz_hub.rc <<'EOF'
#!/bin/sh

# PROVIDE: jolitz_hub
# REQUIRE: NETWORKING
# KEYWORD: shutdown

. /etc/rc.subr

name=jolitz_hub
rcvar=jolitz_hub_enable

load_rc_config $name
: ${jolitz_hub_enable:=NO}
: ${jolitz_hub_user:=jolitz-hub}
: ${jolitz_hub_listen:=127.0.0.1:7331}
: ${jolitz_hub_base_url:=http://127.0.0.1:7331}

pidfile="/var/run/${name}.pid"
procname="/usr/local/bin/jolitz-hub"
command="/usr/sbin/daemon"
command_args="-f -S -p ${pidfile} -u ${jolitz_hub_user} ${procname} serve --data /var/lib/jolitz-hub --listen ${jolitz_hub_listen} --base-url ${jolitz_hub_base_url}"

run_rc_command "$1"
EOF
install -o root -g wheel -m 755 /root/jolitz-scratch/jolitz_hub.rc /usr/local/etc/rc.d/jolitz_hub
sysrc jolitz_hub_enable=YES
sysrc jolitz_hub_base_url=https://hub.example.com
service jolitz_hub start
service jolitz_hub status
curl -fsS http://127.0.0.1:7331/api/v1/health
```

`rc.conf` is a config file: `rules/backups.md` before `sysrc`
writes to it. The service name is `jolitz_hub` with an
underscore; the account and the binary keep the hyphen.

## 7. Reverse proxy

### (a) Caddy

Install from the distribution's stable repository when it has
the package (`rules/debian.md` → Stable Branch Only), after the
phase-2 dry run of `rules/service-class-check.md`:

```
apt-get install --simulate -y caddy 2>/dev/null | awk '/^Inst /{print $2}'
```

```
apt-get update && apt-get install -y caddy
```

FreeBSD: `pkg install -n caddy`, then `pkg install -y caddy`,
`sysrc caddy_enable=YES`; its Caddyfile path stands in
`/usr/local/etc/rc.d/caddy`, read it there. The Debian package
runs `caddy.service` as user `caddy` with
`/etc/caddy/Caddyfile` (https://caddyserver.com/docs/running,
2026-10-28). The site block, checked against
https://caddyserver.com/docs/caddyfile/directives/reverse_proxy
on 2026-10-28, is two lines; a site address with a domain name
turns on automatic HTTPS, certificates included
(https://caddyserver.com/docs/automatic-https, same date).

Config edit per `rules/backups.md`: backup, new version next to
it, diff, config test, then the copy. Debian's stock Caddyfile
serves `/usr/share/caddy` on `:80`; when nothing uses that,
propose a Caddyfile that holds only the hub block (the diff
shows what goes), otherwise append.

```
mkdir -p /var/backups/jolitz
command cp /etc/caddy/Caddyfile /var/backups/jolitz/Caddyfile.20261028-101500
tee /var/backups/jolitz/Caddyfile.new <<'EOF'
hub.example.com {
	reverse_proxy 127.0.0.1:7331
}
EOF
diff -u /var/backups/jolitz/Caddyfile.20261028-101500 /var/backups/jolitz/Caddyfile.new
caddy validate --config /var/backups/jolitz/Caddyfile.new
```

After the user's OK on the diff:

```
command cp /var/backups/jolitz/Caddyfile.new /etc/caddy/Caddyfile
caddy validate --config /etc/caddy/Caddyfile
systemctl reload caddy
rm /var/backups/jolitz/Caddyfile.new
```

`Way back: command cp /var/backups/jolitz/Caddyfile.20261028-101500
/etc/caddy/Caddyfile && systemctl reload caddy`.

### (b) existing nginx

The block, shown before anything is written. `proxy_pass` and
`proxy_set_header` as in
https://nginx.org/en/docs/http/ngx_http_proxy_module.html
(2026-10-28); without the `Host` line nginx sends `$proxy_host`,
the upstream's address. The certificate paths are the host's
own (certbot's `live/` directory, or whatever `memory.md` names
under `SSL`); ask when there is none.

```
mkdir -p /var/backups/jolitz
tee /var/backups/jolitz/jolitz-hub.conf.new <<'EOF'
server {
    listen 443 ssl;
    listen [::]:443 ssl;
    server_name hub.example.com;

    ssl_certificate     /etc/letsencrypt/live/hub.example.com/fullchain.pem;
    ssl_certificate_key /etc/letsencrypt/live/hub.example.com/privkey.pem;

    location / {
        proxy_pass http://127.0.0.1:7331;
        proxy_set_header Host $host;
    }
}
EOF
```

Where it goes is where this host's `nginx.conf` includes
files: `nginx -T 2>/dev/null | grep -E '^\s*include'`. Debian:
`/etc/nginx/sites-available/jolitz-hub.conf` plus the symlink in
`sites-enabled/`; RHEL and FreeBSD: `conf.d/`. The backup of the
directory's state is the file list; the new file's way back is
removing it (`rules/backups.md` → drop-in directories: no
`.bak` next to it).

```
command cp /var/backups/jolitz/jolitz-hub.conf.new /etc/nginx/sites-available/jolitz-hub.conf
ln -s /etc/nginx/sites-available/jolitz-hub.conf /etc/nginx/sites-enabled/jolitz-hub.conf
nginx -t
systemctl reload nginx
rm /var/backups/jolitz/jolitz-hub.conf.new
```

An existing Caddy takes the block of (a) appended to its
Caddyfile; an existing Apache gets a `ProxyPass` block the same
way, written only after `apachectl -M` shows `proxy_http`.

### (c) none

Nothing to do. The join URL is `http://127.0.0.1:7331` through
the tunnel each member opens; `references/join.md` names the
tunnel.

## 8. Firewall

Only with a proxy and only after the user chose in question 4.
Every firewall change arms a rollback timer first
(`rules/rollback-timer.md`); an `allow` rule cannot lock anyone
out, but the rule applies to every firewall change, and a
mistyped `deny` is one line away. Follow the OS rule
(`rules/debian.md`, `rules/rhel.md`, `rules/freebsd.md` →
Firewall) and the probation-first order of `rules/way-back.md`.

Debian/Ubuntu, ufw, open to all:

```
ufw allow 80/tcp
ufw allow 443/tcp
ufw status verbose
```

Restricted to the team network:

```
ufw allow from 10.20.0.0/16 to any port 443 proto tcp
ufw status verbose
```

RHEL family, firewalld: first without `--permanent`, then, after
the health check from outside succeeded, the permanent form:

```
firewall-cmd --add-service=https
firewall-cmd --add-service=http
```

```
firewall-cmd --permanent --add-service=https
firewall-cmd --permanent --add-service=http
firewall-cmd --reload
firewall-cmd --list-services
```

FreeBSD, pf: the line
`pass in on $ext_if proto tcp to port { 80 443 }` goes into
`/etc/pf.conf` through the backup, diff and `pfctl -nf` steps of
`rules/freebsd.md` → Firewall, then `pfctl -f /etc/pf.conf`.

Afterwards, from the workstation:
`curl -fsS https://hub.example.com/api/v1/health`. That answer
is the proof the rollback timer waits for.

## 9. The first admin

On the host, as the service user, so that `hub.db` and its
journal files keep their owner:

```
runuser -u jolitz-hub -- /usr/local/bin/jolitz-hub user add alice --role admin
```

FreeBSD (`su -m` keeps root's shell, so the account's `nologin`
does not stop it; `su(1)`, 2026-10-28):

```
su -m jolitz-hub -c '/usr/local/bin/jolitz-hub user add alice --role admin'
```

The command prints the token once. Hand it to the user as the
command's own output and nothing else: not paraphrased, not in
the session summary, not in memory, changelog, todo or journal
(`rules/secrets.md`). The hub stores a SHA-256 of it; a lost
token is replaced with `jolitz-hub user rotate alice`, never
recovered. The user puts it into their token file as
`references/join.md` says; on their own workstation the same
session continues with `join`.

## 10. Verify from outside

- `curl -fsS https://hub.example.com/api/v1/health` from the
  workstation (option c: through the tunnel).
- `https://hub.example.com/login` in a browser takes the token
  once and shows the fleet page, empty until the first push.
- `systemctl --no-pager status jolitz-hub` (or
  `service jolitz_hub status`) is `active`; `journalctl -u
  jolitz-hub -n 20 --no-pager` shows the start line and no
  error.

## 11. Record

`memory/servers/ops.example.com/memory.md`
(`rules/server-memory.md`, `rules/port-check.md`,
`rules/service-class-check.md`):

```markdown
- Services: jolitz-hub 3.3.3-jolitz.8
- Hub: https://hub.example.com
- jolitz-hub: port 7331 (tcp, 127.0.0.1)
- Web server: caddy
```

`bin/jolitz-memory-lint ops.example.com` afterwards. Local
changelog with `Rollback:` from step 3 and the journal headline
(`rules/changelog.md`):

```
logger -t jolitz "[alice as root] ops.example.com now runs the jolitz team hub at https://hub.example.com behind Caddy, the team's shared server memory and overview — because <the user's reason>"
```

Offer the daily backup from `references/operate.md` → Backup;
it is a cron line or timer and asks on its own.

## 12. Tell the team

Each member asks their own jolitz to "join the hub"; the admin
creates one token per person with `user add <name>`
(`references/operate.md` → Users) and passes it on over a
channel of their choice. The hub is empty until the first
`bin/jolitz-sync push`, which the changelog step runs after the
next change (`rules/changelog.md`); `bin/jolitz-sync push` by
hand fills it now.

## Way back, in full

```
systemctl disable --now jolitz-hub
rm /etc/systemd/system/jolitz-hub.service /usr/local/bin/jolitz-hub
systemctl daemon-reload
```

FreeBSD: `service jolitz_hub stop`, `sysrc -x jolitz_hub_enable
jolitz_hub_base_url`, remove the script and the binary. The
proxy block comes back from `/var/backups/jolitz/`, the
firewall rule is deleted with the same tool that added it.
`/var/lib/jolitz-hub` stays: it is the team's memory, and
`GET /api/v1/export` or a copy of `hub.db` is the way to carry
it elsewhere. The account `jolitz-hub` stays too; deleting an
account is the operator's job, the guard denies it to jolitz
(`CLAUDE.md` → Critical Safety Rules).

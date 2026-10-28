# jolitz-hub

The optional team service of jolitz. One static Go binary mirrors
every member's `memory/` into SQLite (or an existing PostgreSQL),
serves it as a JSON API and
a dense web UI, and knows who is on which host. It runs no command
on any server, holds no token in clear text and refuses a file in
which a value looks like a credential.

Clients never see the database. `bin/jolitz-sync` (in the jolitz
repository) pushes the raw memory files and pulls them back byte
for byte; the hub parses them server-side into the tables the UI
shows. Without `~/.config/jolitz/hub.conf` on a workstation
nothing talks to a hub.

## Build

Go 1.26 or newer, no cgo (the SQLite driver and the PostgreSQL
driver `github.com/jackc/pgx/v5` are pure Go).

```sh
cd hub
go build -ldflags "-X main.version=$(cat ../VERSION)" -o jolitz-hub .
go test ./...
sh scripts/pg-test.sh
```

`go test ./...` runs the store tests against SQLite and skips
PostgreSQL. With `JOLITZ_HUB_TEST_PG` naming a file that holds a
DSN, the store tests run against that database as well, each in
a schema of its own that is dropped afterwards.
`scripts/pg-test.sh` does it all with a throwaway cluster:
`initdb` in a temporary directory, `pg_ctl` on a free port on
127.0.0.1, the DSN in a mode-600 file, `go test ./...`, the
cluster stopped and removed on exit. It needs `initdb`, `pg_ctl`
and `psql` in `PATH` (or `PG_BIN`); arguments go to `go test`.

Cross-compile the same way with `GOOS`/`GOARCH`; the release
workflow attaches `jolitz-hub_<os>_<arch>` binaries and a
`SHA256SUMS` to every tag. Without `-ldflags` the version reads
`dev`.

## Run

```sh
jolitz-hub serve --data /var/lib/jolitz-hub \
  --listen 127.0.0.1:7331 --base-url https://hub.example.com
```

`--data` holds `hub.db` (and its WAL files); it is created with
mode 0750 when missing. `JOLITZ_HUB_DATA` replaces the default.
The hub speaks plain HTTP on localhost; TLS belongs to the reverse
proxy below. `--base-url` is only used for the curl examples on
the `/api` page.

Logs go to stderr, one line per request: method, path, status,
duration. Never the query string, never a body, never a token.

## Config file

`hub.toml` is optional. The hub reads `<data>/hub.toml` when it
exists, or the file named with `--config FILE`, once at start; a
flag on the command line wins over a key in the file. Without
a file the defaults below apply. The annotated example is
`hub/hub.toml` in the repository; it states the defaults.

```toml
listen = "127.0.0.1:7331"
base_url = ""
data_dir = "/var/lib/jolitz-hub"
metrics_public = false
db = "sqlite"
db_dsn_file = ""

[retention]
sessions_days = 90
guard_days = 30
versions_years = 2

[eol]
enabled = true

[summary]
weekday = "mon"
hour = 7

[notify.ntfy]
server = "https://ntfy.sh"
topic = ""
header_file = ""

[notify.matrix]
server = ""
room = ""
header_file = ""

[notify.webhook]
curlrc_file = ""

[oidc]
issuer = ""
client_id = ""
client_secret_file = ""
username_claim = "preferred_username"
```

The format is a subset of TOML: `key = "string"` (escapes `"`,
`\`, `	`, `
`), `key = 123`, `key = true` or `false`,
`[section]` headers with dotted names, `#` comments. No arrays,
inline tables, multi-line strings or dates. An unknown key or
section is an error with its line number, so a typo cannot
switch a job off unnoticed.

Secrets never stand in this file. The notify sections name the
files that hold the token (`header_file`) or the webhook URL
(`curlrc_file`), `db_dsn_file` the PostgreSQL DSN,
`oidc.client_secret_file` the OIDC client secret; a relative
path is taken under the data directory (`--data` when given).
Create them with mode 600, owned by the service user; the hub
logs a warning when a secret file is readable by group or
others. It reads the notify files at send time, the DSN and the
client secret at start, and keeps none of them in the database
or a log line.

## Database

`db = "sqlite"`, the default, keeps everything in
`<data>/hub.db`. `db = "postgres"` connects to a PostgreSQL
database the team already runs; the DSN is one line in the file
`db_dsn_file` names, never in `hub.toml`, on the command line or
in the environment:

```
postgres://jolitz_hub:<password>@db1.example.com:5432/jolitz_hub?sslmode=verify-full
```

The URL form and libpq's `key=value` form both work; the driver
is pgx, which also reads `~/.pgpass` and the `PG*` variables of
the service user for what the DSN leaves out. The role needs to
create tables in the database: the owner of an empty database
does (PostgreSQL 15 and later give the `public` schema to the
database owner). Every command (`serve`, `user`, `migrate`,
`export`, `import`) opens the database `hub.toml` names.

Both backends hold the same tables. The schema is versioned:
`schema_migrations` records each step, `serve` and every other
command apply the missing ones at open, each in a transaction;
`jolitz-hub migrate` does only that and prints the version.
Migrations ship inside the binary (`internal/store/migrations/
sqlite|postgres/NNNN_name.sql`). A SQLite `hub.db` from before
the numbering is carried forward on first open, a phase-1 file
without file versions included. On PostgreSQL text columns use
`COLLATE "C"`, so lists sort byte-wise as on SQLite; times are
RFC 3339 text in UTC on both.

### Moving between backends

```sh
jolitz-hub export --out /var/backups/jolitz/hub-export.tar
# stop the hub, set db and db_dsn_file in hub.toml
jolitz-hub import /var/backups/jolitz/hub-export.tar
jolitz-hub user list
```

`export` writes a new file (mode 600; an existing file is never
overwritten): every file with every version, deleted paths'
histories included, the users with their token hashes and the
claims. `import` reads it into an empty database in one
transaction, checks every body against its sha256 and every
path and user against the rules a push follows, and rebuilds the
derived tables; a database with users, files or claims is
refused. Tokens keep working: their hashes moved. Sessions,
guard denials, OIDC browser sessions, the endoflife.date cache
and the job runs stay behind.

The archive, format 1:

| Entry | What |
|---|---|
| `manifest.json` | `format`, `schema`, `backend`, `hub_version`, `exported_at`, `files` (path, version, sha256 of every current file), `counts` |
| `users.json` | name, role, token_sha256, created_at; oidc_issuer and oidc_subject for a user bound through OIDC |
| `claims.json` | host, user, note, since |
| `history.json` | every version: path, version, sha256, size, updated_at, updated_by |
| `bodies/<sha256>` | each distinct body once |
| `memory/<path>` | the current files, as `GET export` gives them; import ignores them |

## OIDC

Optional browser login through an OpenID Connect issuer
(Keycloak, Authentik, Dex, an Entra ID or Google app). Token
login stays; `jolitz-sync` keeps its bearer tokens.

```toml
base_url = "https://hub.example.com"

[oidc]
issuer = "https://id.example.com/realms/ops"
client_id = "jolitz-hub"
client_secret_file = "oidc-secret"
username_claim = "preferred_username"
```

Register `<base_url>/oidc/callback` as the redirect URI of the
client at the issuer. `base_url` is required; the issuer must be
`https://` (plain `http` only on 127.0.0.1 or localhost).
`client_secret_file` holds the secret of a confidential client,
sent with HTTP Basic; leave it empty for a public client.

`/login` then offers "log in with the team's identity provider".
The flow: authorization code with PKCE (S256), a random `state`
and `nonce` in a ten-minute HttpOnly cookie on `/oidc/`, the
code exchanged at the token endpoint from
`/.well-known/openid-configuration`, the ID token verified with
the standard library only: the issuer's JWKS (fetched once,
again when a token names an unknown `kid`, at most once a
minute), RS256 (RSA, 2048 bits or more) or ES256 (P-256),
nothing else; `iss`, `aud` (and `azp` when there are several
audiences), `exp`, `iat`, `nbf` with one minute of clock skew,
and the nonce of this login. The `username_claim` must name an
existing hub user exactly; OIDC creates nobody, an unknown name
gets a 403 page that says how an admin adds it. The role comes
from the users table.

The name claim only finds the user: many providers let a user
change `preferred_username`. The first OIDC login of a hub user
binds it to the provider's issuer and subject (`iss`, `sub`);
every later login must bring the same pair, and a pair bound to
one hub user cannot log in as another. Both get a 403 page. A
token without `sub` is refused. `user unbind NAME` clears the
binding and ends the user's browser sessions; the next OIDC login
binds it again. `user list` marks bound users `oidc: bound`. With
`username_claim = "email"` the token must also carry
`email_verified: true`. A good login sets the same `jolitz_hub`
cookie as the token login, holding a random browser session the
hub stores as SHA-256 for twelve hours; logout and `user rm` end
it. The session works for the UI and for the API calls the
browser makes with the cookie, never as a bearer token.

## Jobs

`serve` runs a scheduler that wakes once a minute. Each job
records its last run and result; the admin's `/settings` page
shows them.

- `retention`, daily: deletes closed sessions older than
  `sessions_days` and guard denials older than `guard_days`.
- `eol`, daily when `[eol] enabled`: fetches
  `https://endoflife.date/api/v1/products/<product>/` for every
  product derived from memory (the mapping of
  `bin/jolitz-overview`), product names only, never a host name;
  caches the answer in the database. A failed fetch keeps the
  cache and marks the run failed.
- `summary`, weekly at the `[summary]` weekday and hour in the
  hub host's local time: sends the text of
  `bin/jolitz-overview --text` through every configured notify
  channel.

The summary job is armed at the first start and sends from the
next scheduled hour on; a hub that was down at that hour sends
when it is back. Retention removes file versions older than
`retention.versions_years`; the current version of every file
stays. The hub
holds no memory-lint output (the lint runs before every push
and a hit is refused with 422), so the summary has no "Memory
lint" section.

## Metrics

`GET /metrics` serves the Prometheus text format: the gauges of
`bin/jolitz-overview --prom` and three of the hub's own.

```
jolitz_fleet_hosts
jolitz_host_eol_days{host}             0 once ended, -1 unknown
jolitz_host_last_connected_days{host}  -1 unknown
jolitz_host_todos{host}
jolitz_guard_denials_30d
jolitz_hub_sessions_open
jolitz_hub_files
jolitz_hub_last_push_age_seconds{user}
```

A token (viewer role) is required unless `metrics_public =
true`; Prometheus sends it with `bearer_token_file`. With the
public setting the reverse proxy or the firewall decides who
reaches the path.

## overview.json

`GET /api/v1/overview.json` has the shape of
`bin/jolitz-overview --json`: `generated`, `today`, `days`
(30), `tag` (empty), `hosts` with `eol` and `audit` per host,
`todos`, `changes`, `network`, `audit`, `guard` and `lint`
(always empty on the hub).

## Notify channels

The three channels of `rules/notify.md`, as the hub sends them:

- **ntfy**: `POST <server>/<topic>`, the text as body, header
  `Title`, plus every line of `header_file` as a header
  (usually `Authorization: Bearer …`). Without `header_file`
  the topic is published unauthenticated.
- **Matrix**: `PUT <server>/_matrix/client/v3/rooms/<room>/send/
  m.room.message/<txnId>` with `{"msgtype":"m.text","body":…}`,
  the room id percent-encoded, the transaction id from the time
  and a counter, `Authorization` from `header_file`.
- **Webhook** (Slack-compatible): `POST` of `{"text":…}` with
  `Content-Type: application/json` to the `url = "…"` line of
  `curlrc_file`.

A failed send is logged with channel and HTTP status, never
with the URL or a header, and retried once. "Send test message"
on `/settings` (admin) and `POST /api/v1/notify/test` try a
channel once.

## First admin

```sh
jolitz-hub user add alice --role admin --data /var/lib/jolitz-hub
```

`user` reads `<data>/hub.toml` like `serve` (or `--config FILE`),
so on a PostgreSQL hub it writes there.

The token is printed once (stdout); the hub stores its SHA-256.
The user puts it into a mode-600 file as one line
`Authorization: Bearer <token>` (`~/.config/jolitz/hub-token`),
which `curl -H @file` and `jolitz-sync` read, so it never reaches
argv. Further users: `user add NAME --role viewer|operator|admin`,
`user list`, `user rm NAME`, `user rotate NAME`,
`user unbind NAME` (clears the OIDC binding, see OIDC).

Roles: `viewer` reads, `operator` also writes files, sessions,
claims and guard denials, `admin` also releases other people's
claims, ends their sessions and takes a claim over.

## systemd unit

```ini
[Unit]
Description=jolitz hub
After=network-online.target
Wants=network-online.target

[Service]
User=jolitz-hub
Group=jolitz-hub
ExecStart=/usr/local/bin/jolitz-hub serve --data /var/lib/jolitz-hub \
  --listen 127.0.0.1:7331 --base-url https://hub.example.com
Restart=on-failure
NoNewPrivileges=yes
ProtectSystem=strict
ProtectHome=yes
PrivateTmp=yes
ReadWritePaths=/var/lib/jolitz-hub

[Install]
WantedBy=multi-user.target
```

Create the system user and the data directory first:

```sh
useradd --system --home /var/lib/jolitz-hub --shell /usr/sbin/nologin jolitz-hub
install -d -m 0750 -o jolitz-hub -g jolitz-hub /var/lib/jolitz-hub
```

On FreeBSD an `rc.d` script with `daemon -u jolitz-hub` does the
same; the `jolitz-hub` skill carries both.

## Reverse proxy

The hub expects TLS in front and reads `X-Forwarded-Proto` to mark
its login cookie `Secure`. Caddy, two lines:

```
hub.example.com {
    reverse_proxy 127.0.0.1:7331
}
```

An existing nginx works the same way with `proxy_pass` and
`proxy_set_header X-Forwarded-Proto $scheme;`. Rate limiting and
IP restrictions belong to the proxy or the firewall; the hub is
meant for the team's own network or a VPN.

## Data and backup

Everything is in `/var/lib/jolitz-hub/hub.db` (SQLite, WAL mode).
Back it up live with SQLite's own copy, never with `cp` alone
while the service runs:

```sh
sqlite3 /var/lib/jolitz-hub/hub.db ".backup /var/backups/jolitz/hub-$(date +%F).db"
```

On PostgreSQL the database is backed up like the team's other
databases (`pg_dump -Fc`). `jolitz-hub export` works on both and
is the backend-neutral copy.

`GET /api/v1/export` (or `/export` in the UI) returns every memory
file as a tar archive with paths relative to `memory/`; unpack it
with `tar -x -C memory/`. That is the way back to "only files".

What the hub holds: the raw files `servers/<host>/{memory.md,
changelog.log,todo.md,rules.md}`, alias files `servers/<alias>`
(body: the canonical host name), `network.md` and
`audits/<YYYY-MM-DD-HHMM>.json`; the tables derived from them
(hosts, changes, todos, links, audits, aliases); sessions, claims
and guard denials (reason and time, never the command); users as
name, role and token hash. `user.md`, blacklist, read-only and
protected-path lists, `opencode.json` and tokens never arrive:
their paths are refused with 400.

## API

All paths under `/api/v1/`, JSON unless noted, `Authorization:
Bearer <token>` on every request except `health`. Errors are
`{"error": "…"}`. The `/api` page in the UI shows the same table
with a curl example per line.

| Method | Path | Role | What |
|---|---|---|---|
| GET | `health` | none | `{ok, version}` |
| GET | `whoami` | viewer | `{user, role}` of the token |
| GET | `files` | viewer | manifest: path, sha256, version, size, updated_at, by |
| GET | `files/{path}` | viewer | raw bytes; `ETag` sha256, `X-Version`; `?version=N` |
| GET | `files/{path}/history` | viewer | every version: sha256, size, when, by |
| PUT | `files/{path}` | operator | raw body; 428, 409, 422, 400: see below |
| DELETE | `files/{path}` | operator | remove a file and its derived rows |
| GET | `export` | viewer | tar of every file, `application/x-tar` |
| GET | `hosts` | viewer | fleet as in `overview.json` + claim, sessions |
| GET | `hosts/{name}` | viewer | host: changes, to-dos, rules, memory, audit |
| GET | `hosts/{name}/handover` | viewer | claim, sessions, open to-dos, jobs, last 3 changes |
| GET | `changes` | viewer | `?host= &since= &q= &limit=`, newest first |
| GET | `todos` | viewer | `?host=` |
| GET | `audits` | viewer | every snapshot's values, `?host=` |
| GET | `links` | viewer | the `- Link:` lines of network.md |
| POST | `sessions` | operator | `{host, workstation}` → `{id}` |
| POST | `sessions/{id}/heartbeat` | operator | stale at 10 min, closed at 60 |
| DELETE | `sessions/{id}` | operator | own session, or admin |
| GET | `sessions` | viewer | open sessions, `age_seconds`, `stale` |
| GET | `claims` | viewer | host, user, note, since |
| PUT | `claims/{host}` | operator | `{note}`; 409 when another holds it |
| DELETE | `claims/{host}` | operator | own claim, or admin; else 403 |
| POST | `guard` | operator | `{time, reason}`; a `command` key is 400 |
| GET | `guard` | viewer | counts by reason, `?since=` (default 30 d) |
| GET | `overview.json` | viewer | the shape of `bin/jolitz-overview --json` |
| POST | `notify/test` | admin | `{channel}`; 404 not configured, 502 failed |

`PUT files/{path}`: a new file needs no `If-Match`; an existing
one needs `If-Match: <sha256>` of the version it builds on, else
428. A sha that is not the stored one gives 409 with
`{error, version, sha256, yours_sha256, diff, lines_added,
lines_removed}`, `diff` being a unified diff of the hub's body
against yours; `If-Match: *` overwrites on purpose. Every
accepted write is a new version, kept in the history until
retention removes it (never the current one). A value that
looks like a credential gives 422 with
`{line, reason}` and nothing is stored; a path outside the allowed
set gives 400. `PUT claims/{host}` on a host another person holds
gives 409 with `{claimed_by, since, note}`; an admin takes the
claim over instead, which the log records.

Allowed file paths: `servers/<host>/(memory.md|changelog.log|
todo.md|rules.md)`, `servers/<alias>`, `network.md`,
`audits/<YYYY-MM-DD-HHMM>.json`; `<host>` and `<alias>` match
`^[A-Za-z0-9][A-Za-z0-9.-]*$`.

## UI

`/login` (paste the token once; HttpOnly cookie, `Secure` behind
TLS, SameSite=Lax; with `[oidc]` also the issuer's login through
`/oidc/login` and `/oidc/callback`), `/` fleet with filter bar, presence strip
and the network links to add or remove, `/hosts/<name>`
handover sheet: claim and release, who is on it, open to-dos
with check and add, running jobs, the last changes with their
way back, and Owner and Tags to edit. Every edit is a new file
version by the UI user, through the same secret check, and
reaches the workstations with their next pull; viewers see no
forms. `/timeline`, `/activity` (every changelog, searchable),
`/presence`, `/audit`, `/guard`, `/api`, `/export`; for admins
`/settings` with the effective configuration (paths of the
secret files, never their content), the last run of each job
and a test button per notify channel. Monospace,
dense, dark by default and light with `prefers-color-scheme`,
the colour variables of `lib/overview.awk`. No external asset, no
build step; one inline script for the filter buttons, `/` to focus
the search box and a 30-second refresh on `/` and `/presence`.
Every value from memory is escaped by `html/template`.

## Layout

```
main.go                   CLI: serve, user add|list|rm|rotate, migrate,
                          export, import, version
hub.toml                  the annotated example config (= the defaults)
scripts/pg-test.sh        go test against a throwaway PostgreSQL
internal/config           hub.toml: the TOML subset, defaults, validation
internal/store            schema and migrations (SQLite, PostgreSQL),
                          derivation, queries, export/import
internal/auth             OIDC: discovery, PKCE, JWKS, ID token checks
internal/auth/authtest    a test issuer that signs with generated keys
internal/parse            memory.md, changelog.log, todo.md, network.md, audits
internal/lint             the TOKENS/ASSIGN regexes of bin/jolitz-memory-lint
internal/overview         Go port of lib/overview.awk: text, json, prom
internal/notify           ntfy, Matrix, webhook senders
internal/jobs             scheduler: retention, eol fetch, weekly summary
internal/api              /api/v1/, auth, export, /metrics
internal/ui               pages, templates/, static/hub.css
```

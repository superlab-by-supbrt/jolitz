#!/bin/sh
# sync-test.sh — fixture tests for bin/jolitz-sync against a
# running hub. With a Go toolchain and hub/go.mod it builds
# jolitz-hub into a temp dir and starts it on a free port;
# without Go it starts a small Python fake that implements the
# endpoints jolitz-sync uses with the JSON shapes of the plan.
# Without either it prints SKIP and exits 0. Everything else is
# a throwaway HOME and memory/ tree; nothing leaves 127.0.0.1.
#   sh tests/sync-test.sh
#   JOLITZ_SYNC_TEST_FAKE=1 sh tests/sync-test.sh   (force the fake)

REPO_DIR="$(cd "$(dirname "$0")/.." && pwd)"
SYNC="$REPO_DIR/bin/jolitz-sync"
DOCTOR="$REPO_DIR/bin/jolitz-doctor"
PASS=0
FAIL=0
PATH="/opt/homebrew/bin:$PATH"
export PATH

for t in curl jq; do
  if ! command -v "$t" >/dev/null 2>&1; then
    echo "SKIP: $t missing"
    exit 0
  fi
done

T=$(mktemp -d "${TMPDIR:-/tmp}/sync-test.XXXXXX")
HUB_PID=""
cleanup() {
  if [ -n "$HUB_PID" ]; then
    kill "$HUB_PID" 2>/dev/null
    wait "$HUB_PID" 2>/dev/null
  fi
  rm -rf "$T"
}
trap cleanup EXIT

# Tools are resolved and the hub is built with the real HOME:
# a pyenv shim and the Go module cache live under it.
PYTHON=$(python3 -c 'import sys; print(sys.executable)' 2>/dev/null) || PYTHON=""
MODE=""
if [ -z "$JOLITZ_SYNC_TEST_FAKE" ] && [ -f "$REPO_DIR/hub/go.mod" ] \
    && command -v go >/dev/null 2>&1; then
  if (cd "$REPO_DIR/hub" && go build -o "$T/jolitz-hub" . 2>"$T/build.err"); then
    MODE=real
  else
    echo "hub build failed:"; cat "$T/build.err"
    FAIL=$((FAIL + 1))
  fi
fi
if [ -z "$MODE" ] && [ -n "$PYTHON" ]; then
  MODE=fake
fi
if [ -z "$MODE" ]; then
  echo "SKIP: hub not buildable"
  exit 0
fi
echo "sync tests against the $MODE hub"

HOME="$T/home"
export HOME
mkdir -p "$HOME/.config/jolitz" "$HOME/.cache/jolitz"
chmod 700 "$HOME/.config/jolitz" "$HOME/.cache/jolitz"
CONF="$HOME/.config/jolitz/hub.conf"

ok() {
  if [ "$1" -eq 0 ]; then PASS=$((PASS + 1))
  else FAIL=$((FAIL + 1)); echo "FAIL: $2"; fi
}
has() {
  grep -qF -- "$2" "$1"; ok $? "$3"
}
hasnt() {
  if grep -qF -- "$2" "$1"; then ok 1 "$3"; else ok 0 "$3"; fi
}
# same <a> <b> <label>: byte-identical files
same() {
  cmp -s "$1" "$2"; ok $? "$3"
}

# --- which hub -----------------------------------------------
# Token header files for the test's own curl calls: the sync
# client's (alice, operator), a colleague (bob, operator) and
# a viewer. Tokens never appear in argv.
HDR_A="$HOME/.config/jolitz/hub-token"
HDR_B="$T/bob.hdr"
HDR_V="$T/viewer.hdr"
HDR_X="$T/wrong.hdr"
umask 077
printf 'Authorization: Bearer not-a-valid-token\n' > "$HDR_X"

free_port() {
  if [ -n "$PYTHON" ]; then
    "$PYTHON" -I -c 'import socket
s = socket.socket(); s.bind(("127.0.0.1", 0)); print(s.getsockname()[1])'
  else
    echo $((20000 + $$ % 20000))
  fi
}

if [ "$MODE" = real ]; then
  DATA="$T/data"
  mkdir -p "$DATA"
  # user add prints the token once on stdout (Task A's CLI).
  # Both argument orders are tried; the first that works wins.
  user_add() {
    "$T/jolitz-hub" user add "$1" --role "$2" --data "$DATA" 2>/dev/null \
      || "$T/jolitz-hub" --data "$DATA" user add "$1" --role "$2" 2>/dev/null
  }
  for u in alice:operator:$HDR_A bob:operator:$HDR_B viewer:viewer:$HDR_V; do
    name=${u%%:*}; rest=${u#*:}; role=${rest%%:*}; hdr=${rest#*:}
    tok=$(user_add "$name" "$role" | tail -n 1 | tr -d '\r')
    if [ -z "$tok" ]; then
      echo "FAIL: jolitz-hub user add $name printed no token; CLI says:"
      "$T/jolitz-hub" --help 2>&1 | head -n 20
      exit 1
    fi
    printf 'Authorization: Bearer %s\n' "$tok" > "$hdr"
  done
  PORT=$(free_port)
  "$T/jolitz-hub" serve --data "$DATA" --listen "127.0.0.1:$PORT" \
    > "$T/hub.log" 2>&1 &
  HUB_PID=$!
else
  # The fake: the JSON shapes of the plan, the lint patterns of
  # bin/jolitz-memory-lint that the fixtures hit, If-Match,
  # roles. Users come from a file, never from argv.
  printf 'alice\toperator\tfake-token-alice-%s\n' "$$" > "$T/users.tsv"
  printf 'bob\toperator\tfake-token-bob-%s\n' "$$" >> "$T/users.tsv"
  printf 'viewer\tviewer\tfake-token-viewer-%s\n' "$$" >> "$T/users.tsv"
  chmod 600 "$T/users.tsv"
  printf 'Authorization: Bearer fake-token-alice-%s\n' "$$" > "$HDR_A"
  printf 'Authorization: Bearer fake-token-bob-%s\n' "$$" > "$HDR_B"
  printf 'Authorization: Bearer fake-token-viewer-%s\n' "$$" > "$HDR_V"
  cat > "$T/fakehub.py" <<'PYEOF'
import hashlib, json, re, sys, time, uuid
from http.server import BaseHTTPRequestHandler, ThreadingHTTPServer

users_file, port_file = sys.argv[1], sys.argv[2]
USERS = {}
for line in open(users_file):
    name, role, tok = line.rstrip("\n").split("\t")
    USERS[tok] = (name, role)

PATH_RE = re.compile(r"^(network\.md|audits/\d{4}-\d{2}-\d{2}-\d{4}\.json|"
                     r"servers/[A-Za-z0-9][A-Za-z0-9.-]*"
                     r"(/(memory\.md|changelog\.log|todo\.md|rules\.md))?)$")
B = r"(^|[^A-Za-z0-9_])"
TOKENS = re.compile("|".join([
    r"-----BEGIN ([A-Z0-9]+ )*PRIVATE KEY-----",
    B + r"gh[pousr]_[A-Za-z0-9]{36}",
    B + r"github_pat_[A-Za-z0-9_]{22,}",
    B + r"glpat-[A-Za-z0-9_-]{20}",
    B + r"xox[abprs]-[A-Za-z0-9-]{10,}",
    B + r"AKIA[0-9A-Z]{16}",
    B + r"sk-(ant-|proj-)?[A-Za-z0-9_-]{32,}",
    B + r"sk_live_[A-Za-z0-9]{20,}",
    r"[A-Za-z][A-Za-z0-9+.-]*://[^/\s:@]+:[^/\s@$<{]+@",
]))
ASSIGN = re.compile(r"(^|[^A-Za-z0-9])(password|passwd|passphrase|secret|"
                    r"token|api[_-]?key)\s*=\s*[^\s$<{\"']", re.I)

FILES = {}      # path -> dict(body, sha256, updated_at, updated_by, version)
HISTORY = {}    # path -> list of dict(version, sha256, size, updated_at, updated_by)
SESSIONS = {}   # id -> dict
CLAIMS = {}     # host -> dict

def now():
    return time.strftime("%Y-%m-%dT%H:%M:%SZ", time.gmtime())

class H(BaseHTTPRequestHandler):
    protocol_version = "HTTP/1.1"

    def log_message(self, *a):
        pass

    def send(self, code, obj=None, raw=None, ctype="application/json"):
        body = raw if raw is not None else (
            b"" if obj is None else json.dumps(obj).encode())
        self.send_response(code)
        self.send_header("Content-Type", ctype)
        self.send_header("Content-Length", str(len(body)))
        self.end_headers()
        self.wfile.write(body)

    def body(self):
        n = int(self.headers.get("Content-Length") or 0)
        return self.rfile.read(n)

    def auth(self, write):
        a = self.headers.get("Authorization", "")
        if not a.startswith("Bearer ") or a[7:] not in USERS:
            self.send(401, {"error": "no valid token"})
            return None
        name, role = USERS[a[7:]]
        if write and role == "viewer":
            self.send(403, {"error": "viewer tokens read only"})
            return None
        return name

    def route(self):
        p = self.path.split("?")[0]
        if not p.startswith("/api/v1/"):
            return None
        return p[len("/api/v1/"):]

    def do_GET(self):
        r = self.route()
        if r == "health":
            return self.send(200, {"ok": True, "version": "fake"})
        user = self.auth(False)
        if not user:
            return
        if r == "whoami":
            return self.send(200, {"user": user, "role": USERS[self.headers["Authorization"][7:]][1]})
        if r == "files":
            return self.send(200, [
                {"path": p, "version": f["version"], "sha256": f["sha256"],
                 "size": len(f["body"]), "updated_at": f["updated_at"],
                 "updated_by": f["updated_by"]}
                for p, f in sorted(FILES.items())])
        if r.startswith("files/") and r.endswith("/history"):
            p = r[6:-8]
            if p not in HISTORY:
                return self.send(404, {"error": "no such file"})
            return self.send(200, [{k: v for k, v in h.items() if k != "body"}
                                   for h in reversed(HISTORY[p])])
        if r.startswith("files/"):
            p = r[6:]
            q = self.path.split("?", 1)[1] if "?" in self.path else ""
            m = re.match(r"^version=(\d+)$", q)
            if m:
                for h in HISTORY.get(p, []):
                    if h["version"] == int(m.group(1)):
                        return self.send(200, raw=h["body"], ctype="application/octet-stream")
                return self.send(404, {"error": "no such version"})
            if p not in FILES:
                return self.send(404, {"error": "no such file"})
            return self.send(200, raw=FILES[p]["body"],
                             ctype="application/octet-stream")
        m = re.match(r"^hosts/([^/]+)/handover$", r)
        if m:
            h = m.group(1)
            if not any(p.startswith("servers/" + h + "/") for p in FILES):
                return self.send(404, {"error": "no such host"})
            todo = FILES.get("servers/" + h + "/todo.md", {}).get("body", b"").decode("utf-8", "replace")
            items = [l[6:] for l in todo.split("\n") if l.startswith("- [ ] ")]
            t = time.time()
            c = CLAIMS.get(h)
            return self.send(200, {
                "host": h,
                "claim": ({"host": h, "user": c["user"], "note": c["note"], "since": c["since"]} if c else None),
                "sessions": [{"id": i, "user": s["user"], "host": s["host"], "workstation": s["workstation"],
                              "age_seconds": int(t - s["t0"]), "stale": False}
                             for i, s in SESSIONS.items() if s["host"] == h],
                "todos": [x for x in items if ".jolitz/jobs/" not in x],
                "jobs": [x for x in items if ".jolitz/jobs/" in x],
                "changes": []})
        if r == "sessions":
            t = time.time()
            return self.send(200, [
                {"id": i, "user": s["user"], "host": s["host"],
                 "workstation": s["workstation"], "started_at": s["started_at"],
                 "last_beat": s["last_beat"],
                 "age_seconds": int(t - s["t0"]), "stale": t - s["t0"] > 600}
                for i, s in SESSIONS.items()])
        if r == "claims":
            return self.send(200, [
                {"host": h, "user": c["user"], "note": c["note"],
                 "since": c["since"]} for h, c in sorted(CLAIMS.items())])
        self.send(404, {"error": "unknown endpoint"})

    def do_PUT(self):
        r = self.route()
        data = self.body()
        user = self.auth(True)
        if not user:
            return
        if r.startswith("files/"):
            p = r[6:]
            if not PATH_RE.match(p) or "<" in p or '"' in p:
                return self.send(400, {"error": "path outside the allowed set"})
            for n, line in enumerate(data.decode("utf-8", "replace").split("\n"), 1):
                if TOKENS.search(line) or ASSIGN.search(line):
                    return self.send(422, {"line": n,
                                           "reason": "looks like a secret value"})
            cur = FILES.get(p)
            im = self.headers.get("If-Match", "").strip('"')
            yours = hashlib.sha256(data).hexdigest()
            if im and im != "*" and (cur is None or cur["sha256"] != im.lower()):
                return self.send(409, {"error": "If-Match does not match",
                                       "version": cur["version"] if cur else 0,
                                       "sha256": cur["sha256"] if cur else "",
                                       "yours_sha256": yours, "diff": "",
                                       "lines_added": 0, "lines_removed": 0})
            if not im and cur is not None:
                return self.send(428, {"error": "If-Match required", "version": cur["version"],
                                       "sha256": cur["sha256"], "yours_sha256": yours, "diff": ""})
            v = (HISTORY[p][-1]["version"] + 1) if p in HISTORY else 1
            FILES[p] = {"body": data, "sha256": yours, "updated_at": now(),
                        "updated_by": user, "version": v}
            HISTORY.setdefault(p, []).append({"version": v, "sha256": yours, "size": len(data),
                                             "updated_at": now(), "updated_by": user,
                                             "body": data})
            return self.send(201 if v == 1 else 200,
                             {"path": p, "version": v, "sha256": yours})
        if r.startswith("claims/"):
            h = r[7:]
            try:
                note = json.loads(data or b"{}").get("note", "")
            except ValueError:
                return self.send(400, {"error": "bad json"})
            if h in CLAIMS and CLAIMS[h]["user"] != user:
                return self.send(409, {"error": "claimed by " + CLAIMS[h]["user"]})
            CLAIMS[h] = {"user": user, "note": note, "since": now()}
            return self.send(200, {"host": h})
        self.send(404, {"error": "unknown endpoint"})

    def do_POST(self):
        r = self.route()
        data = self.body()
        user = self.auth(True)
        if not user:
            return
        if r == "sessions":
            try:
                req = json.loads(data or b"{}")
            except ValueError:
                return self.send(400, {"error": "bad json"})
            i = uuid.uuid4().hex[:12]
            SESSIONS[i] = {"user": user, "host": req.get("host", ""),
                           "workstation": req.get("workstation", ""),
                           "started_at": now(), "last_beat": now(),
                           "t0": time.time()}
            return self.send(201, {"id": i})
        m = re.match(r"^sessions/([^/]+)/heartbeat$", r)
        if m:
            if m.group(1) not in SESSIONS:
                return self.send(404, {"error": "no such session"})
            SESSIONS[m.group(1)]["t0"] = time.time()
            SESSIONS[m.group(1)]["last_beat"] = now()
            return self.send(200, {"ok": True})
        if r == "guard":
            try:
                req = json.loads(data or b"{}")
            except ValueError:
                return self.send(400, {"error": "bad json"})
            if "command" in req:
                return self.send(400, {"error": "no command text"})
            return self.send(204)
        self.send(404, {"error": "unknown endpoint"})

    def do_DELETE(self):
        r = self.route()
        user = self.auth(True)
        if not user:
            return
        if r.startswith("files/"):
            if FILES.pop(r[6:], None) is None:
                return self.send(404, {"error": "no such file"})
            return self.send(204)
        if r.startswith("sessions/"):
            if SESSIONS.pop(r[9:], None) is None:
                return self.send(404, {"error": "no such session"})
            return self.send(204)
        if r.startswith("claims/"):
            h = r[7:]
            if h not in CLAIMS:
                return self.send(404, {"error": "no such claim"})
            CLAIMS.pop(h)
            return self.send(204)
        self.send(404, {"error": "unknown endpoint"})

srv = ThreadingHTTPServer(("127.0.0.1", 0), H)
with open(port_file, "w") as f:
    f.write(str(srv.server_address[1]))
srv.serve_forever()
PYEOF
  "$PYTHON" -I "$T/fakehub.py" "$T/users.tsv" "$T/port" > "$T/hub.log" 2>&1 &
  HUB_PID=$!
  i=0
  while [ ! -s "$T/port" ] && [ "$i" -lt 100 ]; do sleep 0.1; i=$((i + 1)); done
  PORT=$(cat "$T/port" 2>/dev/null)
  if [ -z "$PORT" ]; then
    echo "FAIL: the fake hub did not start"; cat "$T/hub.log"; exit 1
  fi
fi

URL="http://127.0.0.1:$PORT"
i=0
until curl -sS -o /dev/null "$URL/api/v1/health" 2>/dev/null; do
  i=$((i + 1))
  if [ "$i" -gt 100 ]; then
    echo "FAIL: hub did not answer on $URL"; cat "$T/hub.log"; exit 1
  fi
  sleep 0.1
done

# hub <header file> <api path> [curl args]: the test's own calls.
# Prints the HTTP code; the body lands in $T/hub.out.
hub() {
  h=$1; p=$2; shift 2
  curl -sS -o "$T/hub.out" -w '%{http_code}' -H @"$h" "$@" "$URL/api/v1/$p"
}

# --- memory tree ---------------------------------------------
TODAY=$(date +%Y-%m-%d)
M="$T/memory"
mkdir -p "$M/servers/web1.example.com" "$M/servers/db1.example.com" \
  "$M/audits" "$M/eol" "$M/overview"
cat > "$M/user.md" <<'EOF'
# User
- Operator: alice
EOF
echo "never" > "$M/blacklist.md"
echo "never" > "$M/readonly.md"
echo "never" > "$M/protected-paths.md"
echo '{"provider": "local"}' > "$M/opencode.json"
echo '{}' > "$M/eol/debian.json"
echo '<html>' > "$M/overview/index.html"
cat > "$M/servers/web1.example.com/memory.md" <<EOF
# web1.example.com
- IP: 203.0.113.10
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, web
- Owner: alice
- Last connected: $TODAY
- Database password: in /etc/app/db.env (0600 root)
EOF
cat > "$M/servers/web1.example.com/changelog.log" <<'EOF'
[2026-06-11 09:50] [alice as root]
app.example.com now deploys blue/green <script>alert(1)</script>
— because deploys interrupted visitors
  Detail: templated app@.service (blue :4003,
  green :4005).
  Rollback: re-enable app.service.
[2026-06-13 08:00] [alice as root] quoted "value" and back\slash
EOF
cat > "$M/servers/web1.example.com/rules.md" <<'EOF'
## Replace: Firewall
nftables only.
EOF
echo "not synced" > "$M/servers/web1.example.com/notes.txt"
ln -s web1.example.com "$M/servers/www.example.com"
cat > "$M/servers/db1.example.com/memory.md" <<EOF
# db1.example.com
- IP: 203.0.113.20
- OS: Debian 12 (Bookworm)
- Distro family: debian
- Tags: prod, database
- Last connected: $TODAY
EOF
cat > "$M/servers/db1.example.com/todo.md" <<'EOF'
# To-do
- [ ] Upgrade PostgreSQL
EOF
cat > "$M/network.md" <<'EOF'
# Network
- Link: web1.example.com -> db1.example.com (postgresql 5432)
EOF
cat > "$M/audits/2026-10-01-0900.json" <<'EOF'
{"date":"2026-10-01 09:00","hosts":{"web1.example.com":{"time.ntp":"chrony"}}}
EOF
echo "x" > "$M/audits/notes.json"

# --- no hub.conf ---------------------------------------------
"$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 2 )) "pull without hub.conf exits 2 (got $rc)"
has "$T/o" 'hub.conf' "pull without hub.conf names the file"
"$SYNC" -d "$M" session end > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session end without hub.conf exits 0 (got $rc)"
[ ! -s "$T/o" ]; ok $? "session end without hub.conf is silent"
"$SYNC" --help > "$T/o"; rc=$?
ok $(( rc != 0 )) "--help exits 0"
has "$T/o" 'Usage:' "--help prints usage"
"$SYNC" -d "$M" bogus > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "unknown command exits 2 (got $rc)"

# --- unreachable, bad config ---------------------------------
printf 'url=http://127.0.0.1:1\ntoken_file=%s\nworkstation=test-ws\n' \
  "$HDR_A" > "$CONF"
"$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 3 )) "pull with no hub listening exits 3 (got $rc)"
has "$T/o" 'unreachable' "unreachable is said"
"$SYNC" -d "$M" session end > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session end with no open session exits 0 (got $rc)"

# Plain http carries the token in clear: loopback only.
printf 'url=http://198.51.100.7:7331\ntoken_file=%s\n' "$HDR_A" > "$CONF"
"$SYNC" -d "$M" status > "$T/o" 2>&1; rc=$?
ok $(( rc != 2 )) "plain http to a remote host exits 2 (got $rc)"
has "$T/o" 'plain http' "plain http to a remote host is refused with a reason"

printf 'url = %s\ntoken_file = %s\nworkstation = test-ws\n' \
  "$URL" "$HDR_X" > "$CONF"
"$SYNC" -d "$M" status > "$T/o" 2>&1; rc=$?
ok $(( rc != 2 )) "a rejected token exits 2 (got $rc)"
has "$T/o" '401' "a rejected token names 401"
hasnt "$T/o" 'not-a-valid-token' "the token value is never printed"

printf 'url=%s\ntoken_file=%s\nworkstation=test-ws\n' "$URL" "$HDR_A" > "$CONF"
chmod 644 "$CONF"
"$SYNC" -d "$M" status > "$T/o" 2>&1; rc=$?
has "$T/o" 'not mode 600' "a readable hub.conf warns"
chmod 600 "$CONF"

# --- status before the first push ----------------------------
"$SYNC" -d "$M" status > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "status runs (got $rc: $(head -n 3 "$T/o" | tr '\n' ' '))"
has "$T/o" "hub: $URL" "status names the url"
has "$T/o" 'token: accepted (alice, operator)' "status: token accepted, user and role"
has "$T/o" 'push: 8 files' "status counts the synced set to push"
has "$T/o" '  servers/www.example.com' "status lists the alias"
has "$T/o" 'pull: 0 files' "status: nothing to pull from an empty hub"
hasnt "$T/o" 'user.md' "status never lists user.md"
hasnt "$T/o" 'notes' "status never lists an unknown file"
has "$T/o" 'workstation: test-ws' "status shows the workstation"
has "$T/o" 'pulled: never, stale (never pulled; run jolitz-sync pull)' "status: stale before the first pull"
"$SYNC" -d "$M" status --short > "$T/o" 2>&1
has "$T/o" "$URL: hub " "status --short starts with the url"
has "$T/o" ', alice, operator, 8 to push, 0 to pull, 0 conflicts' "status --short: user, role, counts"
has "$T/o" ', stale' "status --short says stale before the first pull"
n=$(wc -l < "$T/o" | tr -d ' ')
[ "$n" -eq 1 ]; ok $? "status --short is one line (got $n)"

# --- push ----------------------------------------------------
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "push exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'push: servers/web1.example.com/memory.md' "push names a file"
has "$T/o" 'push: servers/www.example.com' "push sends the alias"
has "$T/o" 'push: 8 files' "push summary"
code=$(hub "$HDR_B" files)
[ "$code" = 200 ]; ok $? "GET files as bob (got $code)"
jq -r '.[].path' "$T/hub.out" | sort > "$T/paths"
cat > "$T/want" <<'EOF'
audits/2026-10-01-0900.json
network.md
servers/db1.example.com/memory.md
servers/db1.example.com/todo.md
servers/web1.example.com/changelog.log
servers/web1.example.com/memory.md
servers/web1.example.com/rules.md
servers/www.example.com
EOF
same "$T/paths" "$T/want" "the hub holds exactly the synced set ($(tr '\n' ' ' < "$T/paths"))"
jq -r '.[] | select(.path == "servers/www.example.com") | .sha256' "$T/hub.out" > "$T/s"
printf 'web1.example.com\n' | shasum -a 256 2>/dev/null | cut -d' ' -f1 > "$T/s2" \
  || printf 'web1.example.com\n' | sha256sum | cut -d' ' -f1 > "$T/s2"
same "$T/s" "$T/s2" "the alias body is the canonical name"
code=$(hub "$HDR_B" files/servers/www.example.com)
has "$T/hub.out" 'web1.example.com' "GET of the alias returns the canonical name"
[ -f "$M/.hub-sync.tsv" ]; ok $? "push writes the sync state"
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "second push exits 0 (got $rc)"
has "$T/o" 'push: 0 files' "second push sends nothing"
"$SYNC" -d "$M" status --short > "$T/o" 2>&1
has "$T/o" '0 to push, 0 to pull' "status after push: in sync"

# --- pull into a fresh tree is byte-identical ----------------
M2="$T/memory2"
mkdir -p "$M2"
"$SYNC" -d "$M2" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull into an empty tree exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'pull: 8 files' "pull summary"
for f in servers/web1.example.com/memory.md servers/web1.example.com/changelog.log \
    servers/web1.example.com/rules.md servers/db1.example.com/memory.md \
    servers/db1.example.com/todo.md network.md audits/2026-10-01-0900.json; do
  same "$M/$f" "$M2/$f" "pull: $f byte-identical"
done
[ -L "$M2/servers/www.example.com" ]; ok $? "pull recreates the alias symlink"
[ "$(readlink "$M2/servers/www.example.com")" = web1.example.com ]
ok $? "the alias points to the canonical directory"
[ ! -e "$M2/user.md" ]; ok $? "pull never writes user.md"
[ ! -e "$M2/servers/web1.example.com/notes.txt" ]; ok $? "pull never writes an unknown file"
"$SYNC" -d "$M2" pull --quiet > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "quiet pull with nothing to do exits 0 (got $rc)"
[ ! -s "$T/o" ]; ok $? "quiet pull with nothing to do is silent"
[ -s "$M2/.hub-sync.pulled" ]; ok $? "pull records its time"
"$SYNC" -d "$M2" status > "$T/o" 2>&1
grep -q '^pulled: 20[0-9-]*T[0-9:]*Z$' "$T/o"; ok $? "status: pulled with a time, not stale ($(grep '^pulled' "$T/o"))"
"$SYNC" -d "$M2" status --short > "$T/o" 2>&1
hasnt "$T/o" 'stale' "status --short is not stale after a pull"
# An unreachable hub at the next pull marks the copy stale.
printf 'url=http://127.0.0.1:1\ntoken_file=%s\nworkstation=test-ws\n' "$HDR_A" > "$CONF"
"$SYNC" -d "$M2" pull --quiet > "$T/o" 2>&1; rc=$?
ok $(( rc != 3 )) "pull against a dead hub exits 3 (got $rc)"
[ -s "$M2/.hub-sync.pull-failed" ]; ok $? "a failed pull is recorded"
printf 'url=%s\ntoken_file=%s\nworkstation=test-ws\n' "$URL" "$HDR_A" > "$CONF"
"$SYNC" -d "$M2" status > "$T/o" 2>&1
grep -q '^pulled: 20.*, stale (pull failed at 20.*; run jolitz-sync pull)$' "$T/o"
ok $? "status: stale after a failed pull ($(grep '^pulled' "$T/o"))"
"$SYNC" -d "$M2" status --short > "$T/o" 2>&1
has "$T/o" ', stale' "status --short says stale after a failed pull"
"$SYNC" -d "$M2" pull --quiet > /dev/null 2>&1
[ ! -e "$M2/.hub-sync.pull-failed" ]; ok $? "a pull that ran clears the failure"
"$SYNC" -d "$M2" status --short > "$T/o" 2>&1
hasnt "$T/o" 'stale' "status --short: fresh again"

# A host directory that is a symlink out of memory/ (planted,
# or a mistake) must not receive the hub's files: the write
# would land wherever the link points.
M3="$T/memory3"
OUT3="$T/outside"
mkdir -p "$M3/servers" "$OUT3"
ln -s "$OUT3" "$M3/servers/db1.example.com"
"$SYNC" -d "$M3" pull > "$T/o" 2>&1; rc=$?
[ ! -e "$OUT3/memory.md" ]; ok $? "pull never writes through a host symlink that leaves memory/"
has "$T/o" 'outside memory/' "pull names the link that leaves memory/"
[ -f "$M3/servers/web1.example.com/memory.md" ]; ok $? "pull still writes the other hosts"

# --- a colleague's change arrives ----------------------------
printf '%s\n- Tags: prod, web, bob\n' "$(cat "$M/servers/web1.example.com/memory.md")" \
  > "$T/bob.md"
# hub_sha <path>: the hub's current sha256 of a file.
hub_sha() {
  hub "$HDR_B" files > /dev/null
  jq -r --arg p "$1" '.[] | select(.path == $p) | .sha256' "$T/hub.out"
}
# Once a file exists the hub wants If-Match (428 without).
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md -T "$T/bob.md")
[ "$code" = 428 ]; ok $? "a PUT on an existing file without If-Match is 428 (got $code)"
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md -T "$T/bob.md" \
  -H "If-Match: $(hub_sha servers/web1.example.com/memory.md)")
[ "$code" = 200 ] || [ "$code" = 201 ] || [ "$code" = 204 ]
ok $? "bob PUTs memory.md with If-Match (got $code)"
jq -e '.version == 2' "$T/hub.out" > /dev/null 2>&1; ok $? "the PUT answer carries version 2"
"$SYNC" -d "$M2" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull of an unchanged local file exits 0 (got $rc)"
has "$T/o" 'pull: servers/web1.example.com/memory.md' "pull fetches the changed file"
same "$M2/servers/web1.example.com/memory.md" "$T/bob.md" "the local copy is bob's version"

# --- conflict: changed on both sides -------------------------
printf '%s\n- Tags: prod, web, alice\n' "$(cat "$M/servers/web1.example.com/memory.md")" \
  > "$M/servers/web1.example.com/memory.md"
cp "$M/servers/web1.example.com/memory.md" "$T/alice.md"
"$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "pull with a conflict exits 1 (got $rc)"
has "$T/o" 'conflict: servers/web1.example.com/memory.md (kept; hub version in servers/web1.example.com/memory.md.hub, +1 -1 lines against yours; resolve --mine|--theirs)' \
  "pull lists the conflict with the diff summary"
has "$T/o" '1 conflict' "pull summary counts the conflict"
same "$M/servers/web1.example.com/memory.md" "$T/alice.md" "the local file is kept"
same "$M/servers/web1.example.com/memory.md.hub" "$T/bob.md" ".hub holds the hub version"
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "push with a conflict exits 1 (got $rc)"
has "$T/o" 'conflict: servers/web1.example.com/memory.md' "push lists the conflict"
hasnt "$T/o" 'push: servers/web1.example.com/memory.md' "push does not send the conflicting file"
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md)
same "$T/hub.out" "$T/bob.md" "the hub still holds bob's version"
"$SYNC" -d "$M" status > "$T/o" 2>&1
has "$T/o" 'conflicts: 1' "status counts the conflict"
has "$T/o" '  servers/web1.example.com/memory.md' "status names the conflicting file"
# A push that races another push: 409 on the server side, with
# both shas and the diff of the hub's body against the request.
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md -T "$T/alice.md" \
  -H 'If-Match: 0000000000000000000000000000000000000000000000000000000000000000')
[ "$code" = 409 ]; ok $? "a stale If-Match gets 409 (got $code)"
jq -e '.error and .version == 2 and (.sha256 | length) == 64
  and (.yours_sha256 | length) == 64 and (.diff | type) == "string"' \
  "$T/hub.out" > /dev/null 2>&1; ok $? "the 409 body has error, version, sha256, yours_sha256, diff"
if [ "$MODE" = real ]; then
  jq -r '.diff' "$T/hub.out" > "$T/diff"
  has "$T/diff" '-- Tags: prod, web, bob' "the 409 diff removes the hub line"
  has "$T/diff" '+- Tags: prod, web, alice' "the 409 diff adds the pushed line"
  has "$T/diff" '--- a/servers/web1.example.com/memory.md (hub, version 2, bob)' \
    "the 409 diff names the hub version and who wrote it"
  jq -e '.lines_added == 1 and .lines_removed == 1' "$T/hub.out" > /dev/null 2>&1
  ok $? "the 409 body counts the lines"
fi
# Resolve --theirs: the hub version replaces the local file.
"$SYNC" -d "$M" resolve servers/web1.example.com/memory.md.hub --theirs > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "resolve --theirs exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'resolved: servers/web1.example.com/memory.md (hub version taken)' "resolve --theirs confirms"
same "$M/servers/web1.example.com/memory.md" "$T/bob.md" "resolve --theirs installed the hub version"
[ ! -e "$M/servers/web1.example.com/memory.md.hub" ]; ok $? "resolve --theirs removed the .hub file"
"$SYNC" -d "$M" status --short > "$T/o" 2>&1
has "$T/o" '0 to push, 0 to pull, 0 conflicts' "resolved: in sync"
"$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull after resolving exits 0 (got $rc)"
# Resolve --mine: a second conflict, this time alice wins.
cp "$T/alice.md" "$M/servers/web1.example.com/memory.md"
printf '%s\n- Tags: prod, web, bob, again\n' "$(cat "$T/bob.md")" > "$T/bob2.md"
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md -T "$T/bob2.md" \
  -H "If-Match: $(hub_sha servers/web1.example.com/memory.md)")
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "push into a 409 exits 1 (got $rc)"
has "$T/o" 'conflict: servers/web1.example.com/memory.md' "push lists the 409 as a conflict"
has "$T/o" 'lines against yours' "push shows the diff summary"
same "$M/servers/web1.example.com/memory.md.hub" "$T/bob2.md" "the 409 wrote the hub version as .hub"
"$SYNC" -d "$M" resolve --mine servers/web1.example.com/memory.md > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "resolve --mine exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'resolved: servers/web1.example.com/memory.md (yours pushed' "resolve --mine confirms"
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md)
same "$T/hub.out" "$T/alice.md" "the hub holds the local version after resolve --mine"
[ ! -e "$M/servers/web1.example.com/memory.md.hub" ]; ok $? "resolve --mine removed the .hub file"
"$SYNC" -d "$M" status --short > "$T/o" 2>&1
has "$T/o" '0 to push, 0 to pull, 0 conflicts' "resolve --mine: in sync"
"$SYNC" -d "$M" resolve servers/web1.example.com/memory.md > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "resolve without a side exits 2 (got $rc)"
"$SYNC" -d "$M" resolve servers/web1.example.com/notes.txt --mine > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "resolve of a file outside the set exits 2 (got $rc)"
# history: every version with who and when.
"$SYNC" -d "$M" history servers/web1.example.com/memory.md > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "history exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'history: servers/web1.example.com/memory.md' "history names the file"
has "$T/o" '  v4  ' "history lists version 4"
has "$T/o" '  v1  ' "history lists version 1"
grep -q '  v[0-9]*  .*  alice  ' "$T/o"; ok $? "history names alice"
grep -q '  v[0-9]*  .*  bob  ' "$T/o"; ok $? "history names bob"
code=$(hub "$HDR_B" 'files/servers/web1.example.com/memory.md?version=1')
[ "$code" = 200 ]; ok $? "GET ?version=1 answers (got $code)"
hasnt "$T/hub.out" 'Tags: prod, web, alice' "version 1 predates alice's tag"
has "$T/hub.out" '- Owner: alice' "version 1 is the first push"
"$SYNC" -d "$M" history servers/web1.example.com/nothing.md > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "history of a file outside the set exits 2 (got $rc)"

# --- a local change pushes with If-Match ---------------------
printf '%s\n- Note: pushed with If-Match\n' "$(cat "$M/servers/web1.example.com/memory.md")" \
  > "$M/servers/web1.example.com/memory.md"
"$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull with a local-only change exits 0 (got $rc)"
has "$T/o" 'local ahead: servers/web1.example.com/memory.md (push)' "pull names the local change"
same "$M/servers/web1.example.com/memory.md" "$M/servers/web1.example.com/memory.md" "x"
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "push of a local change exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'push: 1 file' "push sends one file"
code=$(hub "$HDR_B" files/servers/web1.example.com/memory.md)
same "$T/hub.out" "$M/servers/web1.example.com/memory.md" "the hub holds the new version"

# --- push <path> ---------------------------------------------
printf -- '- [ ] Vacuum\n' >> "$M/servers/db1.example.com/todo.md"
printf -- '- Link: db1.example.com -> backup.example.com (ssh)\n' >> "$M/network.md"
"$SYNC" -d "$M" push servers/db1.example.com/todo.md > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "push <path> exits 0 (got $rc)"
has "$T/o" 'push: servers/db1.example.com/todo.md' "push <path> sends the named file"
hasnt "$T/o" 'network.md' "push <path> leaves the other file alone"
"$SYNC" -d "$M" push "$M/network.md" > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "push with an absolute path exits 0 (got $rc)"
has "$T/o" 'push: network.md' "push resolves the absolute path"
"$SYNC" -d "$M" push servers/web1.example.com/notes.txt > "$T/o" 2>&1; rc=$?
ok $(( rc != 2 )) "push of a file outside the set exits 2 (got $rc)"

# --- lint blocks push ----------------------------------------
P="gh""p_"
TOK="${P}abcdefghijklmnopqrstuvwxyz0123456789"
cp "$M/servers/db1.example.com/memory.md" "$T/db1.good"
printf -- '- Deploy token: %s\n' "$TOK" >> "$M/servers/db1.example.com/memory.md"
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "push with a secret exits 1 (got $rc)"
has "$T/o" 'ERROR db1.example.com: servers/db1.example.com/memory.md:7' \
  "push prints the lint error with the line"
has "$T/o" 'nothing sent' "push says nothing was sent"
hasnt "$T/o" "$TOK" "push never prints the secret"
code=$(hub "$HDR_B" files/servers/db1.example.com/memory.md)
same "$T/hub.out" "$T/db1.good" "the hub still holds the clean memory.md"
# The hub lints too (Global Constraints): a raw PUT gets 422.
code=$(hub "$HDR_B" files/servers/db1.example.com/memory.md \
  -T "$M/servers/db1.example.com/memory.md")
[ "$code" = 422 ]; ok $? "the hub refuses a secret with 422 (got $code)"
jq -e '.line == 7' "$T/hub.out" > /dev/null 2>&1; ok $? "422 names the line"
cp "$T/db1.good" "$M/servers/db1.example.com/memory.md"

# --- a deleted to-do list ------------------------------------
rm "$M/servers/db1.example.com/todo.md"
"$SYNC" -d "$M" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "push after a local delete exits 0 (got $rc)"
has "$T/o" 'delete: servers/db1.example.com/todo.md' "push deletes on the hub"
code=$(hub "$HDR_B" files/servers/db1.example.com/todo.md)
[ "$code" = 404 ]; ok $? "the hub no longer has todo.md (got $code)"
"$SYNC" -d "$M2" pull > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull without --prune exits 0 (got $rc)"
has "$T/o" 'hub deleted: servers/db1.example.com/todo.md (pull --prune removes it here)' \
  "pull names the file the hub deleted"
[ -f "$M2/servers/db1.example.com/todo.md" ]; ok $? "pull without --prune keeps it"
echo "mine" > "$M2/user.md"
"$SYNC" -d "$M2" pull --prune > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "pull --prune exits 0 (got $rc)"
has "$T/o" 'prune: servers/db1.example.com/todo.md' "pull --prune names the file"
[ ! -e "$M2/servers/db1.example.com/todo.md" ]; ok $? "pull --prune removes it"
[ -f "$M2/user.md" ]; ok $? "pull --prune never touches user.md"
[ -f "$M2/servers/db1.example.com/memory.md" ]; ok $? "pull --prune keeps the host"

# --- a path the hub should not send ---------------------------
# The client validates every manifest path itself. Only the
# fake lets a test plant one; the real hub rejects it with 400.
if [ "$MODE" = fake ]; then
  :
fi
code=$(hub "$HDR_B" 'files/servers/evil"host/memory.md' -T "$T/db1.good")
[ "$code" = 400 ]; ok $? "a hostname with a quote is 400 (got $code)"
code=$(hub "$HDR_B" 'files/servers/../user.md' -T "$T/db1.good")
[ "$code" = 400 ] || [ "$code" = 404 ]; ok $? "a path with .. is refused (got $code)"

# --- roles ---------------------------------------------------
printf 'url=%s\ntoken_file=%s\nworkstation=test-ws\n' "$URL" "$HDR_V" > "$CONF"
"$SYNC" -d "$M2" status --short > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "a viewer can run status (got $rc: $(cat "$T/o"))"
has "$T/o" 'viewer, viewer' "status --short names the viewer role"
printf -- '- Note: viewer\n' >> "$M2/network.md"
"$SYNC" -d "$M2" push > "$T/o" 2>&1; rc=$?
ok $(( rc != 2 )) "a viewer's push exits 2 (got $rc)"
has "$T/o" '403' "a viewer's push names 403"
"$SYNC" -d "$M2" pull > /dev/null 2>&1
cp "$M/network.md" "$M2/network.md"
printf 'url=%s\ntoken_file=%s\nworkstation=test-ws\n' "$URL" "$HDR_A" > "$CONF"

# --- sessions ------------------------------------------------
"$SYNC" -d "$M" session start web1.example.com > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session start exits 0 (got $rc: $(cat "$T/o"))"
[ ! -s "$T/o" ]; ok $? "session start is silent"
[ -s "$HOME/.cache/jolitz/hub-session" ]; ok $? "session id is kept"
SID=$(cat "$HOME/.cache/jolitz/hub-session")
code=$(hub "$HDR_B" sessions)
jq -e --arg id "$SID" '.[] | select((.id | tostring) == $id)
  | select(.workstation == "test-ws" and .host == "web1.example.com"
    and .user == "alice")' "$T/hub.out" > /dev/null 2>&1
ok $? "GET sessions lists alice on web1 from test-ws ($(cat "$T/hub.out"))"
"$SYNC" -d "$M" session beat > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session beat exits 0 (got $rc: $(cat "$T/o"))"
"$SYNC" -d "$M" status > "$T/o" 2>&1
has "$T/o" 'sessions: 1' "status counts the session"
has "$T/o" 'alice@web1.example.com from test-ws (0m)' "status lists the session with its age"
"$SYNC" -d "$M" session end > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session end exits 0 (got $rc: $(cat "$T/o"))"
[ ! -e "$HOME/.cache/jolitz/hub-session" ]; ok $? "session end drops the id"
code=$(hub "$HDR_B" sessions)
n=$(jq 'length' "$T/hub.out" 2>/dev/null)
[ "${n:-x}" = 0 ]; ok $? "GET sessions is empty after end (got $n)"
"$SYNC" -d "$M" session beat > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session beat without a session starts one (got $rc)"
[ -s "$HOME/.cache/jolitz/hub-session" ]; ok $? "beat kept the new id"
"$SYNC" -d "$M" session end > /dev/null 2>&1
"$SYNC" -d "$M" session start 'bad host' > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "session with a bad host exits 1, never 2 (got $rc)"

# --- claims --------------------------------------------------
# Without a session the claim stands on its own.
"$SYNC" -d "$M" claim db1.example.com "migration until 20:00" > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "claim exits 0 (got $rc: $(cat "$T/o"))"
has "$T/o" 'claimed db1.example.com: migration until 20:00' "claim confirms"
hasnt "$T/o" 'released at session end' "a claim outside a session is not tied to one"
has "$T/o" 'handover: db1.example.com' "claim prints the handover"
has "$T/o" 'claim: alice since ' "the handover names the claim"
has "$T/o" '(migration until 20:00)' "the handover carries the note"
has "$T/o" 'open to-dos: 0' "the handover counts the open to-dos (todo.md was deleted above)"
has "$T/o" 'sessions: none' "the handover says nobody is on the host"
code=$(hub "$HDR_B" claims)
jq -e '.[] | select(.host == "db1.example.com" and .user == "alice"
  and .note == "migration until 20:00")' "$T/hub.out" > /dev/null 2>&1
ok $? "GET claims lists the claim ($(cat "$T/hub.out"))"
"$SYNC" -d "$M" status > "$T/o" 2>&1
has "$T/o" 'claims: 1' "status counts the claim"
has "$T/o" 'db1.example.com: alice' "status lists the claim"
"$SYNC" -d "$M" release db1.example.com > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "release exits 0 (got $rc: $(cat "$T/o"))"
has "$T/o" 'released db1.example.com' "release confirms"
code=$(hub "$HDR_B" claims)
n=$(jq 'length' "$T/hub.out" 2>/dev/null)
[ "${n:-x}" = 0 ]; ok $? "GET claims is empty after release (got $n)"
"$SYNC" -d "$M" claim 'x/y' > /dev/null 2>&1; rc=$?
ok $(( rc != 2 )) "claim with a bad host exits 2 (got $rc)"

# --- handover ------------------------------------------------
"$SYNC" -d "$M" handover web1.example.com > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "handover exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
has "$T/o" 'handover: web1.example.com' "handover names the host"
has "$T/o" 'claim: none' "handover: no claim"
has "$T/o" 'open to-dos: 0' "handover: nothing open on web1"
if [ "$MODE" = real ]; then
  has "$T/o" 'last changes: 2' "handover counts the last changes"
  has "$T/o" '  2026-06-13 08:00 [alice as root] quoted "value" and back\slash' "handover lists the newest change first"
  has "$T/o" '    Rollback: none recorded' "handover marks a change without a way back"
  has "$T/o" '    Rollback: re-enable app.service.' "handover prints the way back"
fi
"$SYNC" -d "$M" handover nope.example.com > "$T/o" 2>&1; rc=$?
ok $(( rc != 1 )) "handover of an unknown host exits 1 (got $rc)"
has "$T/o" '404' "handover of an unknown host names 404"
# A running job (a to-do naming ~/.jolitz/jobs/) is listed apart.
cat > "$M/servers/db1.example.com/todo.md" <<'EOF'
# To-do
- [ ] Upgrade PostgreSQL
- [ ] dist-upgrade running in ~/.jolitz/jobs/20261028-1200-upgrade
EOF
"$SYNC" -d "$M" push servers/db1.example.com/todo.md > /dev/null 2>&1
"$SYNC" -d "$M" handover db1.example.com > "$T/o" 2>&1
has "$T/o" 'running jobs: 1' "handover counts the running job"
has "$T/o" '  - [ ] dist-upgrade running in ~/.jolitz/jobs/20261028-1200-upgrade' "handover names the job directory"
has "$T/o" 'open to-dos: 1' "the job is not counted as a to-do"

# --- claims end with the session that made them ---------------
"$SYNC" -d "$M" session start db1.example.com > /dev/null 2>&1
"$SYNC" -d "$M" claim db1.example.com "patching" > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "claim in a session exits 0 (got $rc)"
has "$T/o" '(released at session end; claim --keep keeps it)' "claim says it ends with the session"
grep -q '^claim	db1.example.com$' "$HOME/.cache/jolitz/hub-session"; ok $? "the session file lists the claim"
"$SYNC" -d "$M" claim --keep web1.example.com "stays" > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "claim --keep exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
hasnt "$T/o" 'released at session end' "claim --keep is not tied to the session"
hasnt "$HOME/.cache/jolitz/hub-session" 'web1' "the session file does not list a --keep claim"
"$SYNC" -d "$M" status > "$T/o" 2>&1
has "$T/o" 'alice@db1.example.com from test-ws' "status: the session is open"
"$SYNC" -d "$M" session end > "$T/o" 2>&1; rc=$?
ok $(( rc != 0 )) "session end exits 0 (got $rc: $(cat "$T/o"))"
code=$(hub "$HDR_B" claims)
jq -e '[.[] | select(.host == "db1.example.com")] | length == 0' "$T/hub.out" > /dev/null 2>&1
ok $? "session end released the session's claim ($(cat "$T/hub.out"))"
jq -e '.[] | select(.host == "web1.example.com" and .note == "stays")' "$T/hub.out" > /dev/null 2>&1
ok $? "session end kept the --keep claim"
# release removes the bookkeeping line too.
"$SYNC" -d "$M" session start > /dev/null 2>&1
"$SYNC" -d "$M" claim db1.example.com > /dev/null 2>&1
"$SYNC" -d "$M" release db1.example.com > /dev/null 2>&1
hasnt "$HOME/.cache/jolitz/hub-session" 'claim' "release drops the session's claim line"
"$SYNC" -d "$M" session end > /dev/null 2>&1
"$SYNC" -d "$M" release web1.example.com > /dev/null 2>&1

# --- a UI edit arrives with the next pull ----------------------
# The UI takes the bearer token like the API; the form rewrites
# todo.md as a new version by that user.
if [ "$MODE" = real ]; then
  code=$(curl -sS -o "$T/hub.out" -w '%{http_code}' -H @"$HDR_B" \
    --data-urlencode 'action=toggle' --data-urlencode 'text=Upgrade PostgreSQL' \
    --data-urlencode 'done=1' "$URL/hosts/db1.example.com/todos")
  [ "$code" = 303 ]; ok $? "the UI to-do toggle answers 303 (got $code)"
  code=$(curl -sS -o "$T/hub.out" -w '%{http_code}' -H @"$HDR_B" \
    --data-urlencode 'action=add' --data-urlencode 'text=Vacuum' \
    "$URL/hosts/db1.example.com/todos")
  [ "$code" = 303 ]; ok $? "the UI to-do add answers 303 (got $code)"
  code=$(curl -sS -o "$T/hub.out" -w '%{http_code}' -H @"$HDR_V" \
    --data-urlencode 'action=add' --data-urlencode 'text=nope' \
    "$URL/hosts/db1.example.com/todos")
  [ "$code" = 403 ]; ok $? "a viewer cannot edit in the UI (got $code)"
  "$SYNC" -d "$M" pull > "$T/o" 2>&1; rc=$?
  ok $(( rc != 0 )) "pull after the UI edit exits 0 (got $rc: $(cat "$T/o" | tr '\n' ' '))"
  has "$T/o" 'pull: servers/db1.example.com/todo.md' "pull fetches the edited todo.md"
  cat > "$T/want.todo" <<'EOF'
# To-do
- [x] Upgrade PostgreSQL
- [ ] dist-upgrade running in ~/.jolitz/jobs/20261028-1200-upgrade
- [ ] Vacuum
EOF
  same "$M/servers/db1.example.com/todo.md" "$T/want.todo" "the UI edit is in the local todo.md"
  "$SYNC" -d "$M" history servers/db1.example.com/todo.md > "$T/o" 2>&1
  grep -q '  v[0-9]*  .*  bob  ' "$T/o"; ok $? "the UI edit is a version by bob"
  "$SYNC" -d "$M" status --short > "$T/o" 2>&1
  has "$T/o" '0 to push, 0 to pull, 0 conflicts' "after the UI edit: in sync"
fi

# --- the token stays in its file ------------------------------
grep -nE "Bearer[^\"']*\\\$|-H[ =]['\"]?Authorization" "$SYNC" > "$T/o" || true
[ ! -s "$T/o" ]; ok $? "jolitz-sync never builds an Authorization header ($(cat "$T/o"))"
grep -c -- '-H @"\$TOKEN_FILE"' "$SYNC" | grep -q '^1$'
ok $? "the token file is read by curl with -H @file in one place"

# --- doctor --------------------------------------------------
mkdir -p "$T/bin"
printf '#!/bin/sh\necho "2.1.312 (Claude Code)"\n' > "$T/bin/claude"
chmod 755 "$T/bin/claude"
PATH="$T/bin:$PATH" "$DOCTOR" -d "$M" > "$T/o" 2>&1
has "$T/o" "OK    hub: $URL: hub " "doctor prints the hub line when configured"
has "$T/o" '0 conflicts' "doctor's hub line carries the counts"
rm "$CONF"
PATH="$T/bin:$PATH" "$DOCTOR" -d "$M" > "$T/o" 2>&1
hasnt "$T/o" 'hub:' "doctor prints no hub line without hub.conf"
printf 'url=http://127.0.0.1:1\ntoken_file=%s\n' "$HDR_A" > "$CONF"
PATH="$T/bin:$PATH" "$DOCTOR" -d "$M" > "$T/o" 2>&1
has "$T/o" 'WARN  hub: unreachable' "doctor warns when the hub is down"

echo "sync tests ($MODE hub): $PASS passed, $FAIL failed"
[ "$FAIL" -eq 0 ]

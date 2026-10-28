#!/bin/sh
# Run the hub's Go tests against a throwaway PostgreSQL cluster.
#
#   sh hub/scripts/pg-test.sh [go test arguments]
#
# initdb creates a cluster in a new temporary directory, pg_ctl
# starts it on a free port on 127.0.0.1 (TCP only: a socket path
# under a long TMPDIR exceeds the 103-byte limit on macOS), the
# DSN goes into a mode-600 file, and `go test ./...` runs with JOLITZ_HUB_TEST_PG naming
# it. The cluster is stopped and the directory removed on exit,
# also after a failure. No existing database is touched.
#
# Needs initdb, pg_ctl and psql in PATH (or PG_BIN naming their
# directory) and go. Without arguments the tests run with -count=1.
set -eu

here=$(cd "$(dirname "$0")/.." && pwd)
[ -n "${PG_BIN:-}" ] && PATH="$PG_BIN:$PATH"
for t in initdb pg_ctl psql go; do
  command -v "$t" >/dev/null 2>&1 || { echo "pg-test: $t not found" >&2; exit 2; }
done

tmp=$(mktemp -d "${TMPDIR:-/tmp}/jolitz-hub-pg.XXXXXX")
data="$tmp/data"
started=0
cleanup() {
  if [ "$started" -eq 1 ]; then
    pg_ctl -D "$data" -m immediate -w stop >/dev/null 2>&1 || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT
trap 'exit 130' INT TERM

initdb -D "$data" -U hubtest -A trust -E UTF8 --no-locale >"$tmp/initdb.log" 2>&1 || {
  cat "$tmp/initdb.log" >&2; exit 1; }

# The first port from 54329 on that the server can bind: a port
# in use makes pg_ctl fail, and the next one is tried.
port=54329
while :; do
  if pg_ctl -D "$data" -l "$tmp/server.log" -w -t 30 \
    -o "-p $port -c unix_socket_directories='' -c listen_addresses=127.0.0.1 -c fsync=off" \
    start >/dev/null 2>&1; then
    started=1
    break
  fi
  port=$((port + 1))
  if [ "$port" -ge 54349 ]; then
    cat "$tmp/server.log" >&2
    exit 1
  fi
done
psql -h 127.0.0.1 -p "$port" -U hubtest -d postgres -qAt \
  -c 'CREATE DATABASE hubtest' >/dev/null

(umask 077
 printf 'postgres://hubtest@127.0.0.1:%s/hubtest?sslmode=disable\n' "$port" >"$tmp/dsn")

echo "pg-test: $(postgres --version 2>/dev/null || echo PostgreSQL) on 127.0.0.1:$port"
[ "$#" -eq 0 ] && set -- -count=1 ./...
cd "$here"
JOLITZ_HUB_TEST_PG="$tmp/dsn" go test "$@"

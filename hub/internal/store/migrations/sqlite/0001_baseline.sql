-- The schema of phases 1 and 2. Every statement tolerates a
-- database that already has the table: a hub from before the
-- versioned migrations runs this once and keeps its data. The
-- Go step after it adds the version column to a phase-1 files
-- table and seeds the history (store/migrate.go).
CREATE TABLE IF NOT EXISTS users(
  name TEXT PRIMARY KEY, token_sha256 TEXT NOT NULL UNIQUE,
  role TEXT NOT NULL, created_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS files(
  path TEXT PRIMARY KEY, body BLOB NOT NULL, sha256 TEXT NOT NULL,
  size INTEGER NOT NULL, updated_at TEXT NOT NULL, updated_by TEXT NOT NULL,
  version INTEGER NOT NULL DEFAULT 1);
CREATE TABLE IF NOT EXISTS file_versions(
  path TEXT NOT NULL, version INTEGER NOT NULL, body BLOB NOT NULL,
  sha256 TEXT NOT NULL, size INTEGER NOT NULL, updated_at TEXT NOT NULL,
  updated_by TEXT NOT NULL, PRIMARY KEY(path, version));
CREATE TABLE IF NOT EXISTS hosts(
  name TEXT PRIMARY KEY, os TEXT NOT NULL DEFAULT '', ip TEXT NOT NULL DEFAULT '',
  distro TEXT NOT NULL DEFAULT '', tags TEXT NOT NULL DEFAULT '',
  owner TEXT NOT NULL DEFAULT '', services TEXT NOT NULL DEFAULT '',
  guest TEXT NOT NULL DEFAULT '', ssh_port TEXT NOT NULL DEFAULT '',
  mode TEXT NOT NULL DEFAULT '', last_connected TEXT NOT NULL DEFAULT '',
  product TEXT NOT NULL DEFAULT '', version TEXT NOT NULL DEFAULT '',
  updated_at TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS aliases(alias TEXT PRIMARY KEY, host TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS changes(
  id INTEGER PRIMARY KEY AUTOINCREMENT, host TEXT NOT NULL, date TEXT NOT NULL,
  time TEXT NOT NULL, who TEXT NOT NULL, headline TEXT NOT NULL,
  detail TEXT NOT NULL, rollback TEXT NOT NULL, flags TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS changes_host ON changes(host);
CREATE INDEX IF NOT EXISTS changes_date ON changes(date, time);
CREATE TABLE IF NOT EXISTS todos(host TEXT NOT NULL, text TEXT NOT NULL, open INTEGER NOT NULL);
CREATE TABLE IF NOT EXISTS links(src TEXT NOT NULL, dst TEXT NOT NULL, label TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS audits(snapshot TEXT NOT NULL, host TEXT NOT NULL, key TEXT NOT NULL, value TEXT NOT NULL);
CREATE INDEX IF NOT EXISTS audits_snapshot ON audits(snapshot);
CREATE TABLE IF NOT EXISTS sessions(
  id TEXT PRIMARY KEY, "user" TEXT NOT NULL, host TEXT NOT NULL,
  workstation TEXT NOT NULL, started_at TEXT NOT NULL,
  last_beat TEXT NOT NULL, ended_at TEXT);
CREATE TABLE IF NOT EXISTS claims(host TEXT PRIMARY KEY, "user" TEXT NOT NULL, note TEXT NOT NULL, since TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS guard(id INTEGER PRIMARY KEY AUTOINCREMENT, "user" TEXT NOT NULL, at TEXT NOT NULL, reason TEXT NOT NULL);
CREATE TABLE IF NOT EXISTS eol(product TEXT PRIMARY KEY, fetched TEXT NOT NULL, body BLOB NOT NULL);
CREATE TABLE IF NOT EXISTS jobs(name TEXT PRIMARY KEY, last_run TEXT NOT NULL, ok INTEGER NOT NULL, result TEXT NOT NULL);

-- todos and links get an id, so both backends order them by
-- insertion without SQLite's rowid; the old rowid becomes the
-- id. web_sessions holds the browser logins made through OIDC.
CREATE TABLE todos_new(id INTEGER PRIMARY KEY AUTOINCREMENT, host TEXT NOT NULL, text TEXT NOT NULL, open INTEGER NOT NULL);
INSERT INTO todos_new(id, host, text, open) SELECT rowid, host, text, open FROM todos ORDER BY rowid;
DROP TABLE todos;
ALTER TABLE todos_new RENAME TO todos;
CREATE INDEX todos_host ON todos(host);
CREATE TABLE links_new(id INTEGER PRIMARY KEY AUTOINCREMENT, src TEXT NOT NULL, dst TEXT NOT NULL, label TEXT NOT NULL);
INSERT INTO links_new(id, src, dst, label) SELECT rowid, src, dst, label FROM links ORDER BY rowid;
DROP TABLE links;
ALTER TABLE links_new RENAME TO links;
CREATE TABLE web_sessions(
  token_sha256 TEXT PRIMARY KEY, "user" TEXT NOT NULL,
  created_at TEXT NOT NULL, expires_at TEXT NOT NULL);
CREATE INDEX web_sessions_user ON web_sessions("user");

-- web_sessions holds the browser logins made through OIDC.
CREATE TABLE web_sessions(
  token_sha256 TEXT COLLATE "C" PRIMARY KEY, "user" TEXT COLLATE "C" NOT NULL,
  created_at TEXT COLLATE "C" NOT NULL, expires_at TEXT COLLATE "C" NOT NULL);
CREATE INDEX web_sessions_user ON web_sessions("user");

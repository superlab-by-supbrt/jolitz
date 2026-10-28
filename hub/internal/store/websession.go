package store

import (
	"database/sql"
	"errors"
	"time"
)

// UserByName returns a user; nil when there is none.
func (s *Store) UserByName(name string) (*User, error) {
	var u User
	var at string
	err := s.db.QueryRow(`SELECT name, role, created_at FROM users WHERE name = ?`, name).Scan(&u.Name, &u.Role, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTime(at)
	return &u, nil
}

// StartWebSession opens a browser session for a user who logged
// in through OIDC and returns its secret, the cookie value. The
// hub keeps only its SHA-256, as for tokens. Expired sessions
// are removed on the way.
func (s *Store) StartWebSession(user string, ttl time.Duration) (string, error) {
	secret, hash, err := newToken()
	if err != nil {
		return "", err
	}
	now := s.Now().UTC()
	if _, err := s.db.Exec(`DELETE FROM web_sessions WHERE expires_at <= ?`, now.Format(time.RFC3339)); err != nil {
		return "", err
	}
	_, err = s.db.Exec(`INSERT INTO web_sessions(token_sha256, "user", created_at, expires_at) VALUES(?,?,?,?)`,
		hash, user, now.Format(time.RFC3339), now.Add(ttl).Format(time.RFC3339))
	return secret, err
}

// UserByWebSession resolves a browser session secret; nil when
// it is unknown, expired or its user is gone.
func (s *Store) UserByWebSession(secret string) (*User, error) {
	if secret == "" {
		return nil, nil
	}
	var u User
	var at string
	err := s.db.QueryRow(`SELECT u.name, u.role, u.created_at FROM web_sessions w JOIN users u ON u.name = w."user"
		WHERE w.token_sha256 = ? AND w.expires_at > ?`, hashToken(secret), s.now()).Scan(&u.Name, &u.Role, &at)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	u.CreatedAt = parseTime(at)
	return &u, nil
}

// EndWebSession removes a browser session; an unknown one is no
// error.
func (s *Store) EndWebSession(secret string) error {
	_, err := s.db.Exec(`DELETE FROM web_sessions WHERE token_sha256 = ?`, hashToken(secret))
	return err
}

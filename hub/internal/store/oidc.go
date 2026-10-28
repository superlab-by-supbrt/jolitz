package store

import (
	"database/sql"
	"errors"
	"fmt"
)

// A hub user is bound to one identity at the OIDC provider, the
// issuer and subject (iss, sub) of its first OIDC login. The
// username claim finds the user; the binding makes sure a later
// login is the same account at the provider, whatever name it
// carries by then. Token login does not look at it.

// BindOIDC binds a user to an issuer and subject. An unbound
// user is bound; a user bound to this pair stays as it is. A
// user bound to another pair, or a pair bound to another user,
// is ErrConflict; an unknown user ErrNotFound.
func (s *Store) BindOIDC(user, iss, sub string) error {
	if iss == "" || sub == "" {
		return fmt.Errorf("%w: empty oidc issuer or subject", ErrInvalid)
	}
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	var curIss, curSub sql.NullString
	err = t.QueryRow(`SELECT oidc_issuer, oidc_subject FROM users WHERE name = ?`, user).Scan(&curIss, &curSub)
	if errors.Is(err, sql.ErrNoRows) {
		return ErrNotFound
	}
	if err != nil {
		return err
	}
	if curIss.Valid || curSub.Valid {
		if curIss.String == iss && curSub.String == sub {
			return nil
		}
		return fmt.Errorf("%w: user %s is bound to another oidc identity", ErrConflict, user)
	}
	var other string
	err = t.QueryRow(`SELECT name FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, iss, sub).Scan(&other)
	if err == nil {
		return fmt.Errorf("%w: the oidc identity is bound to another user", ErrConflict)
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return err
	}
	res, err := t.Exec(`UPDATE users SET oidc_issuer = ?, oidc_subject = ?
		WHERE name = ? AND oidc_issuer IS NULL AND oidc_subject IS NULL`, iss, sub, user)
	if err != nil {
		// A login of the same identity as another user at the
		// same moment: the unique index decides.
		t.Rollback()
		if n, _ := s.UserByOIDC(iss, sub); n != "" && n != user {
			return fmt.Errorf("%w: the oidc identity is bound to another user", ErrConflict)
		}
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return fmt.Errorf("%w: user %s was bound meanwhile", ErrConflict, user)
	}
	return t.Commit()
}

// OIDCBinding returns the issuer and subject a user is bound to;
// ok is false for an unbound user, ErrNotFound for an unknown one.
func (s *Store) OIDCBinding(user string) (iss, sub string, ok bool, err error) {
	var i, su sql.NullString
	err = s.db.QueryRow(`SELECT oidc_issuer, oidc_subject FROM users WHERE name = ?`, user).Scan(&i, &su)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", false, ErrNotFound
	}
	if err != nil {
		return "", "", false, err
	}
	if !i.Valid || !su.Valid {
		return "", "", false, nil
	}
	return i.String, su.String, true, nil
}

// UserByOIDC names the user bound to an issuer and subject; ""
// when there is none.
func (s *Store) UserByOIDC(iss, sub string) (string, error) {
	var name string
	err := s.db.QueryRow(`SELECT name FROM users WHERE oidc_issuer = ? AND oidc_subject = ?`, iss, sub).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	return name, err
}

// UnbindOIDC clears the binding of a user and ends its browser
// sessions; the next OIDC login binds it again. An unbound user
// is no error, an unknown one ErrNotFound.
func (s *Store) UnbindOIDC(user string) error {
	t, err := s.db.Begin()
	if err != nil {
		return err
	}
	defer t.Rollback()
	res, err := t.Exec(`UPDATE users SET oidc_issuer = NULL, oidc_subject = NULL WHERE name = ?`, user)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	if _, err := t.Exec(`DELETE FROM web_sessions WHERE "user" = ?`, user); err != nil {
		return err
	}
	return t.Commit()
}

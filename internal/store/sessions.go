package store

import (
	"database/sql"
	"errors"
	"time"

	"github.com/YousefProject/servicedesk/internal/model"
)

func (s *Store) CreateSession(token string, userID int64, csrf string, expires time.Time, ua, ip string) error {
	_, err := s.db.Exec(
		`INSERT INTO sessions (token, user_id, csrf, created_at, expires_at, user_agent, ip)
		 VALUES (?, ?, ?, ?, ?, ?, ?)`,
		token, userID, csrf, dbTime(now()), dbTime(expires), ua, ip,
	)
	return err
}

type Session struct {
	Token     string
	UserID    int64
	CSRF      string
	ExpiresAt time.Time
}

func (s *Store) Session(token string) (*Session, *model.User, error) {
	row := s.db.QueryRow(
		`SELECT s.token, s.user_id, s.csrf, s.expires_at,
		        u.id, u.email, u.name, u.role, u.active, u.created_at
		   FROM sessions s JOIN users u ON u.id = s.user_id
		  WHERE s.token = ?`,
		token,
	)
	sess := &Session{}
	var exp, created string
	var role string
	var active int
	u := &model.User{}
	err := row.Scan(&sess.Token, &sess.UserID, &sess.CSRF, &exp,
		&u.ID, &u.Email, &u.Name, &role, &active, &created)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, nil, ErrNotFound
	}
	if err != nil {
		return nil, nil, err
	}
	sess.ExpiresAt = parseTime(exp)
	u.Role = model.Role(role)
	u.Active = active == 1
	u.CreatedAt = parseTime(created)
	if !u.Active || time.Now().UTC().After(sess.ExpiresAt) {
		_ = s.DeleteSession(token)
		return nil, nil, ErrNotFound
	}
	return sess, u, nil
}

func (s *Store) DeleteSession(token string) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE token = ?`, token)
	return err
}

func (s *Store) DeleteUserSessions(userID int64) error {
	_, err := s.db.Exec(`DELETE FROM sessions WHERE user_id = ?`, userID)
	return err
}

func (s *Store) PurgeExpiredSessions() (int64, error) {
	res, err := s.db.Exec(`DELETE FROM sessions WHERE expires_at < ?`, dbTime(now()))
	if err != nil {
		return 0, err
	}
	return res.RowsAffected()
}

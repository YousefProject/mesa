package store

import (
	"database/sql"
	"errors"
	"fmt"
	"time"
)

var ErrNotFound = errors.New("no encontrado")

const timeLayout = time.RFC3339

func now() time.Time { return time.Now().UTC() }

func dbTime(t time.Time) string { return t.UTC().Format(timeLayout) }

func nullTime(t sql.NullString) *time.Time {
	if !t.Valid || t.String == "" {
		return nil
	}
	parsed, err := time.Parse(timeLayout, t.String)
	if err != nil {
		return nil
	}
	return &parsed
}

func timeVal(t *time.Time) any {
	if t == nil {
		return nil
	}
	return dbTime(*t)
}

func parseTime(s string) time.Time {
	t, err := time.Parse(timeLayout, s)
	if err != nil {
		return time.Time{}
	}
	return t
}

func idVal(id *int64) any {
	if id == nil {
		return nil
	}
	return *id
}

type Store struct {
	db *sql.DB
}

func New(db *sql.DB) *Store { return &Store{db: db} }

func (s *Store) DB() *sql.DB { return s.db }

func (s *Store) tx(fn func(tx *sql.Tx) error) error {
	tx, err := s.db.Begin()
	if err != nil {
		return err
	}
	if err := fn(tx); err != nil {
		_ = tx.Rollback()
		return err
	}
	return tx.Commit()
}

func (s *Store) Audit(actorID *int64, action, entity string, entityID int64, details string) {
	_, err := s.db.Exec(
		`INSERT INTO audit_events (actor_id, action, entity, entity_id, details, created_at)
		 VALUES (?, ?, ?, ?, ?, ?)`,
		idVal(actorID), action, entity, entityID, details, dbTime(now()),
	)
	if err != nil {
		_ = fmt.Errorf("audit: %w", err)
	}
}

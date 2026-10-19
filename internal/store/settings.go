package store

import (
	"database/sql"
	"errors"
	"strings"
)

// GetSetting devuelve el valor de un ajuste o "" si no existe.
func (s *Store) GetSetting(key string) (string, error) {
	var v string
	err := s.db.QueryRow(`SELECT value FROM settings WHERE key = ?`, key).Scan(&v)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return v, nil
}

// SetSetting guarda (upsert) un ajuste.
func (s *Store) SetSetting(key, value string) error {
	_, err := s.db.Exec(
		`INSERT INTO settings (key, value) VALUES (?, ?)
		 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		key, strings.TrimSpace(value),
	)
	return err
}

// Settings devuelve todos los ajustes guardados como mapa clave→valor.
func (s *Store) Settings() (map[string]string, error) {
	rows, err := s.db.Query(`SELECT key, value FROM settings`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[string]string{}
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		out[k] = v
	}
	return out, rows.Err()
}

// SetSettings guarda varios ajustes en una sola transacción.
func (s *Store) SetSettings(m map[string]string) error {
	return s.tx(func(tx *sql.Tx) error {
		stmt, err := tx.Prepare(
			`INSERT INTO settings (key, value) VALUES (?, ?)
			 ON CONFLICT(key) DO UPDATE SET value = excluded.value`,
		)
		if err != nil {
			return err
		}
		defer stmt.Close()
		for k, v := range m {
			if _, err := stmt.Exec(k, strings.TrimSpace(v)); err != nil {
				return err
			}
		}
		return nil
	})
}

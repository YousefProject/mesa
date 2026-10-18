package store

import (
	"database/sql"
	"errors"
	"fmt"
	"strings"

	"github.com/YousefProject/servicedesk/internal/model"
)

type scanner interface{ Scan(dest ...any) error }

const userCols = `id, email, name, role, active, created_at`

func scanUser(sc scanner) (*model.User, error) {
	var u model.User
	var role, created string
	var active int
	if err := sc.Scan(&u.ID, &u.Email, &u.Name, &role, &active, &created); err != nil {
		return nil, err
	}
	u.Role = model.Role(role)
	u.Active = active == 1
	u.CreatedAt = parseTime(created)
	return &u, nil
}

func (s *Store) CreateUser(email, name string, role model.Role, passwordHash string) (*model.User, error) {
	email = strings.ToLower(strings.TrimSpace(email))
	name = strings.TrimSpace(name)
	res, err := s.db.Exec(
		`INSERT INTO users (email, name, role, password_hash, active, created_at) VALUES (?, ?, ?, ?, 1, ?)`,
		email, name, string(role), passwordHash, dbTime(now()),
	)
	if err != nil {
		if strings.Contains(err.Error(), "UNIQUE") {
			return nil, errors.New("ya existe un usuario con ese correo")
		}
		return nil, err
	}
	id, _ := res.LastInsertId()
	return s.UserByID(id)
}

func (s *Store) UserByID(id int64) (*model.User, error) {
	u, err := scanUser(s.db.QueryRow(`SELECT `+userCols+` FROM users WHERE id = ?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return nil, ErrNotFound
	}
	return u, err
}

// UserByEmail devuelve el usuario y su hash de contraseña.
func (s *Store) UserByEmail(email string) (*model.User, string, error) {
	row := s.db.QueryRow(
		`SELECT `+userCols+`, password_hash FROM users WHERE email = ? COLLATE NOCASE`,
		strings.ToLower(strings.TrimSpace(email)),
	)
	var u model.User
	var role, created, hash string
	var active int
	if err := row.Scan(&u.ID, &u.Email, &u.Name, &role, &active, &created, &hash); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, "", ErrNotFound
		}
		return nil, "", err
	}
	u.Role = model.Role(role)
	u.Active = active == 1
	u.CreatedAt = parseTime(created)
	return &u, hash, nil
}

func (s *Store) queryUsers(query string, args ...any) ([]model.User, error) {
	rows, err := s.db.Query(query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []model.User{}
	for rows.Next() {
		u, err := scanUser(rows)
		if err != nil {
			return nil, err
		}
		out = append(out, *u)
	}
	return out, rows.Err()
}

func (s *Store) Users() ([]model.User, error) {
	return s.queryUsers(`SELECT ` + userCols + ` FROM users ORDER BY active DESC, name`)
}

func (s *Store) Staff() ([]model.User, error) {
	return s.queryUsers(`SELECT ` + userCols + ` FROM users WHERE active = 1 AND role IN ('admin','agent') ORDER BY name`)
}

func (s *Store) UpdateUserRole(id int64, role model.Role) error {
	res, err := s.db.Exec(`UPDATE users SET role = ? WHERE id = ?`, string(role), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) SetUserActive(id int64, active bool) error {
	a := 0
	if active {
		a = 1
	}
	res, err := s.db.Exec(`UPDATE users SET active = ? WHERE id = ?`, a, id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return ErrNotFound
	}
	return nil
}

func (s *Store) UpdateUserProfile(id int64, name string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("el nombre no puede estar vacío")
	}
	_, err := s.db.Exec(`UPDATE users SET name = ? WHERE id = ?`, name, id)
	return err
}

func (s *Store) SetPassword(id int64, hash string) error {
	_, err := s.db.Exec(`UPDATE users SET password_hash = ? WHERE id = ?`, hash, id)
	return err
}

func (s *Store) PasswordHash(id int64) (string, error) {
	var hash string
	err := s.db.QueryRow(`SELECT password_hash FROM users WHERE id = ?`, id).Scan(&hash)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrNotFound
	}
	return hash, err
}

func (s *Store) CountUsers() (int, error) {
	var n int
	err := s.db.QueryRow(`SELECT COUNT(*) FROM users`).Scan(&n)
	return n, err
}

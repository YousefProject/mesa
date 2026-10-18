package db

import (
	"database/sql"
	_ "embed"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schema string

func Open(path string) (*sql.DB, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)&_pragma=foreign_keys(1)&_time_format=sqlite", path)
	conn, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	conn.SetConnMaxLifetime(time.Hour)
	if err := conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func Migrate(conn *sql.DB) error {
	if _, err := conn.Exec(schema); err != nil {
		return fmt.Errorf("migración: %w", err)
	}
	return nil
}

package db

import (
	"context"
	"database/sql"
	"fmt"
	"time"

	_ "modernc.org/sqlite"
)

type DB struct{ sql *sql.DB }

func Open(path string) (*DB, error) {
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", path)
	s, err := sql.Open("sqlite", dsn)
	if err != nil { return nil, err }
	s.SetMaxOpenConns(1)
	return &DB{sql: s}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) Exec(ctx context.Context, q string, args ...any) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second); defer cancel()
	_, err := d.sql.ExecContext(c, q, args...)
	return err
}

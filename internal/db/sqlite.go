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
	// WAL allows concurrent readers alongside a single writer; busy_timeout makes
	// writers wait for the lock instead of failing immediately with SQLITE_BUSY.
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", path)
	s, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	// A single connection serializes all access, which avoids SQLITE_BUSY write
	// contention and keeps request state transitions deterministic. SQLite is not
	// the bottleneck for this workload, so the simplicity is worth more than the
	// marginal read concurrency a larger pool would add.
	s.SetMaxOpenConns(1)
	return &DB{sql: s}, nil
}

func (d *DB) Close() error { return d.sql.Close() }

func (d *DB) SQL() *sql.DB { return d.sql }

func (d *DB) Ping(ctx context.Context) error { return d.sql.PingContext(ctx) }

func (d *DB) Exec(ctx context.Context, q string, args ...any) error {
	c, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	_, err := d.sql.ExecContext(c, q, args...)
	return err
}

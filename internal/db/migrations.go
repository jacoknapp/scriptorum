package db

import (
	"context"
	"database/sql"
	"fmt"
)

func (d *DB) Migrate(ctx context.Context) error {
	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS requests (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  updated_at TEXT NOT NULL,
  requester_email TEXT NOT NULL,
  title TEXT NOT NULL,
  authors TEXT,
  isbn10 TEXT,
  isbn13 TEXT,
  format TEXT NOT NULL,
  status TEXT NOT NULL,
  status_reason TEXT,
  approver_email TEXT,
  approved_at TEXT,
  readarr_request TEXT,
  readarr_response TEXT
);`); err != nil {
		return err
	}

	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS audit_events (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  ts TEXT NOT NULL,
  actor_email TEXT NOT NULL,
  event_type TEXT NOT NULL,
  request_id INTEGER,
  details TEXT
);`); err != nil {
		return err
	}

	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS users (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  created_at TEXT NOT NULL,
  username TEXT NOT NULL UNIQUE,
  password_hash TEXT NOT NULL,
  is_admin INTEGER NOT NULL DEFAULT 0
);`); err != nil {
		return err
	}

	// Ensure new columns exist on users table for forward compatibility
	if err := d.ensureUserColumn(ctx, "auto_approve", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	// Readarr caching tables
	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS readarr_cache (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  cache_key TEXT UNIQUE NOT NULL,
  cache_type TEXT NOT NULL,
  data TEXT NOT NULL,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  expires_at DATETIME
);`); err != nil {
		return err
	}

	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS readarr_authors (
  id INTEGER PRIMARY KEY,
  name TEXT NOT NULL,
  readarr_id INTEGER,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
);`); err != nil {
		return err
	}

	if err := d.Exec(ctx, `
CREATE TABLE IF NOT EXISTS readarr_books (
  id INTEGER PRIMARY KEY AUTOINCREMENT,
  title TEXT NOT NULL,
  author_id INTEGER,
  isbn13 TEXT,
  isbn10 TEXT,
  asin TEXT,
  readarr_data TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (author_id) REFERENCES readarr_authors(id)
);`); err != nil {
		return err
	}

	return nil
}

// ensureUserColumn ensures a column exists on the users table; if missing, it adds it.
func (d *DB) ensureUserColumn(ctx context.Context, name, colDef string) error {
	// Check existing columns via PRAGMA table_info(users)
	rows, err := d.sql.QueryContext(ctx, "PRAGMA table_info(users)")
	if err != nil {
		return err
	}
	defer rows.Close()
	exists := false
	for rows.Next() {
		var cid, notnull, pk int
		var cname, ctype string
		var dflt sql.NullString
		if err := rows.Scan(&cid, &cname, &ctype, &notnull, &dflt, &pk); err != nil {
			return err
		}
		if cname == name {
			exists = true
			break
		}
	}
	if exists {
		return nil
	}
	// Add the column
	stmt := fmt.Sprintf("ALTER TABLE users ADD COLUMN %s %s", name, colDef)
	return d.Exec(ctx, stmt)
}

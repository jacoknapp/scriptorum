package db

import (
	"context"
	"database/sql"
	"fmt"
)

const schemaVersion = 3

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
  external_status TEXT,
  matched_readarr_id INTEGER,
  readarr_request TEXT,
  readarr_response TEXT
);`); err != nil {
		return err
	}

	if err := d.ensureRequestColumn(ctx, "external_status", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureRequestColumn(ctx, "matched_readarr_id", "INTEGER"); err != nil {
		return err
	}
	if err := d.ensureRequestColumn(ctx, "cover_url", "TEXT"); err != nil {
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
  source_kind TEXT NOT NULL DEFAULT '',
  readarr_id INTEGER NOT NULL DEFAULT 0,
  title TEXT NOT NULL,
  author_name TEXT,
  author_id INTEGER,
  isbn13 TEXT,
  isbn10 TEXT,
  asin TEXT,
  foreign_book_id TEXT,
  foreign_edition_id TEXT,
  monitored INTEGER NOT NULL DEFAULT 0,
  grabbed INTEGER NOT NULL DEFAULT 0,
  book_file_count INTEGER NOT NULL DEFAULT 0,
  readarr_data TEXT,
  created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
  FOREIGN KEY (author_id) REFERENCES readarr_authors(id)
);`); err != nil {
		return err
	}

	if err := d.ensureReadarrBooksColumn(ctx, "source_kind", "TEXT NOT NULL DEFAULT ''"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "readarr_id", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "author_name", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "foreign_book_id", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "foreign_edition_id", "TEXT"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "monitored", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "grabbed", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}
	if err := d.ensureReadarrBooksColumn(ctx, "book_file_count", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		return err
	}

	if err := d.ensureIndexes(ctx); err != nil {
		return err
	}
	if err := d.setSchemaVersion(ctx, schemaVersion); err != nil {
		return err
	}

	return nil
}

func (d *DB) ensureIndexes(ctx context.Context) error {
	indexes := []string{
		`CREATE INDEX IF NOT EXISTS idx_requests_requester_email_id ON requests(requester_email, id DESC)`,
		`CREATE INDEX IF NOT EXISTS idx_requests_status ON requests(status)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_readarr_id ON readarr_books(source_kind, readarr_id)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_foreign_edition_id ON readarr_books(source_kind, foreign_edition_id)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_foreign_book_id ON readarr_books(source_kind, foreign_book_id)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_isbn13 ON readarr_books(source_kind, isbn13)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_isbn10 ON readarr_books(source_kind, isbn10)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_asin ON readarr_books(source_kind, asin)`,
		`CREATE INDEX IF NOT EXISTS idx_readarr_books_source_kind_title_author ON readarr_books(source_kind, title COLLATE NOCASE, author_name)`,
	}

	for _, stmt := range indexes {
		if err := d.Exec(ctx, stmt); err != nil {
			return err
		}
	}
	return nil
}

// ensureUserColumn ensures a column exists on the users table; if missing, it adds it.
func (d *DB) ensureUserColumn(ctx context.Context, name, colDef string) error {
	return d.ensureTableColumn(ctx, "users", name, colDef)
}

func (d *DB) ensureRequestColumn(ctx context.Context, name, colDef string) error {
	return d.ensureTableColumn(ctx, "requests", name, colDef)
}

func (d *DB) ensureReadarrBooksColumn(ctx context.Context, name, colDef string) error {
	return d.ensureTableColumn(ctx, "readarr_books", name, colDef)
}

func (d *DB) ensureTableColumn(ctx context.Context, table, name, colDef string) error {
	// Check existing columns via PRAGMA table_info(users)
	rows, err := d.sql.QueryContext(ctx, "PRAGMA table_info("+table+")")
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
	stmt := fmt.Sprintf("ALTER TABLE %s ADD COLUMN %s %s", table, name, colDef)
	return d.Exec(ctx, stmt)
}

func (d *DB) setSchemaVersion(ctx context.Context, version int) error {
	return d.Exec(ctx, fmt.Sprintf("PRAGMA user_version = %d", version))
}

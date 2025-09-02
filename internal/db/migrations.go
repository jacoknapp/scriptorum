package db

import "context"

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

	// Local users for login
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

	return nil
}

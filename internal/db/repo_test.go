package db

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateAndCRUD(t *testing.T) {
	tdir := t.TempDir()
	path := filepath.Join(tdir, "scriptorum.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	req := &Request{RequesterEmail: "user@example.com", Title: "Book", Authors: []string{"Alice"}, Format: "ebook", Status: "pending"}
	id, err := db.CreateRequest(context.Background(), req)
	if err != nil || id == 0 {
		t.Fatalf("create: %v %d", err, id)
	}

	items, err := db.ListRequests(context.Background(), "", 10)
	if err != nil || len(items) == 0 {
		t.Fatalf("list: %v %d", err, len(items))
	}

	if err := db.ApproveRequest(context.Background(), id, "admin@example.com"); err != nil {
		t.Fatalf("approve: %v", err)
	}

	if err := db.UpdateRequestStatus(context.Background(), id, "queued", "ok", "admin@example.com", []byte(`{}`), []byte(`{}`)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("db file missing: %v", err)
	}
}

func TestMigrateSetsSchemaVersionAndIndexes(t *testing.T) {
	tdir := t.TempDir()
	path := filepath.Join(tdir, "scriptorum.db")
	db, err := Open(path)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	var version int
	if err := db.SQL().QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("query user_version: %v", err)
	}
	if version != schemaVersion {
		t.Fatalf("expected schema version %d, got %d", schemaVersion, version)
	}

	indexes := []string{
		"idx_requests_requester_email_id",
		"idx_readarr_books_source_kind_isbn13",
		"idx_readarr_books_source_kind_title_author",
	}
	for _, name := range indexes {
		var got string
		err := db.SQL().QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='index' AND name=?`, name).Scan(&got)
		if err != nil {
			if err == sql.ErrNoRows {
				t.Fatalf("expected index %s to exist", name)
			}
			t.Fatalf("query index %s: %v", name, err)
		}
	}
}

func TestAuditEvents(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	reqID := int64(42)
	if err := db.InsertAuditEvent(context.Background(), "Admin@Example.com", "request.approved", &reqID, `{"title":"Book"}`); err != nil {
		t.Fatalf("insert: %v", err)
	}
	if err := db.InsertAuditEvent(context.Background(), "admin@example.com", "user.login", nil, ""); err != nil {
		t.Fatalf("insert without request id: %v", err)
	}

	events, err := db.ListAuditEvents(context.Background(), 10)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	// Most recent first.
	if events[0].EventType != "user.login" || events[0].RequestID != nil {
		t.Fatalf("unexpected newest event: %+v", events[0])
	}
	if events[1].EventType != "request.approved" || events[1].ActorEmail != "admin@example.com" || events[1].RequestID == nil || *events[1].RequestID != reqID {
		t.Fatalf("unexpected oldest event: %+v", events[1])
	}
}

func TestListAuditEventsDefaultLimit(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if err := db.InsertAuditEvent(context.Background(), "admin@example.com", "user.login", nil, ""); err != nil {
		t.Fatalf("insert: %v", err)
	}

	// limit <= 0 should fall back to the default (200) rather than erroring
	// or returning zero rows.
	for _, limit := range []int{0, -5} {
		events, err := db.ListAuditEvents(context.Background(), limit)
		if err != nil {
			t.Fatalf("list with limit=%d: %v", limit, err)
		}
		if len(events) != 1 {
			t.Fatalf("limit=%d: expected 1 event via default limit, got %d", limit, len(events))
		}
	}
}

func TestListAuditEventsQueryError(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	if _, err := db.ListAuditEvents(context.Background(), 10); err == nil {
		t.Fatal("expected error from ListAuditEvents on a closed database")
	}
}

func TestAuditEventRequestIDStr(t *testing.T) {
	withID := int64(7)
	ev := AuditEvent{RequestID: &withID}
	if got := ev.RequestIDStr(); got != "7" {
		t.Fatalf("expected \"7\", got %q", got)
	}

	withoutID := AuditEvent{}
	if got := withoutID.RequestIDStr(); got != "" {
		t.Fatalf("expected empty string, got %q", got)
	}
}

func TestCountPendingRequestsByUserQueryError(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	db.Close()

	if _, err := db.CountPendingRequestsByUser(context.Background(), "user@example.com"); err == nil {
		t.Fatal("expected error from CountPendingRequestsByUser on a closed database")
	}
}

func TestCountPendingRequestsByUser(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	count, err := db.CountPendingRequestsByUser(context.Background(), "user@example.com")
	if err != nil {
		t.Fatalf("count (no requests): %v", err)
	}
	if count != 0 {
		t.Fatalf("expected 0 pending requests, got %d", count)
	}

	if _, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "user@example.com", Title: "Book One", Status: "pending"}); err != nil {
		t.Fatalf("create request 1: %v", err)
	}
	id2, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "USER@example.com", Title: "Book Two", Status: "pending"})
	if err != nil {
		t.Fatalf("create request 2: %v", err)
	}
	// Approved requests should not count toward the pending quota, and the
	// lookup should be case-insensitive on the requester email.
	if _, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "user@example.com", Title: "Book Three", Status: "approved"}); err != nil {
		t.Fatalf("create request 3: %v", err)
	}
	if err := db.ApproveRequest(context.Background(), id2, "admin@example.com"); err != nil {
		t.Fatalf("approve request 2: %v", err)
	}

	count, err = db.CountPendingRequestsByUser(context.Background(), "User@Example.com")
	if err != nil {
		t.Fatalf("count (with requests): %v", err)
	}
	if count != 1 {
		t.Fatalf("expected 1 pending request, got %d", count)
	}
}

func TestListRequestsByStatus(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	if _, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "a@example.com", Title: "Pending One", Status: "pending"}); err != nil {
		t.Fatalf("create 1: %v", err)
	}
	if _, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "b@example.com", Title: "Pending Two", Status: "pending"}); err != nil {
		t.Fatalf("create 2: %v", err)
	}
	if _, err := db.CreateRequest(context.Background(), &Request{RequesterEmail: "c@example.com", Title: "Approved One", Status: "approved"}); err != nil {
		t.Fatalf("create 3: %v", err)
	}

	all, err := db.ListRequestsByStatus(context.Background(), "PENDING", 0)
	if err != nil {
		t.Fatalf("list (no limit): %v", err)
	}
	if len(all) != 2 {
		t.Fatalf("expected 2 pending requests, got %d", len(all))
	}

	limited, err := db.ListRequestsByStatus(context.Background(), "pending", 1)
	if err != nil {
		t.Fatalf("list (limit 1): %v", err)
	}
	if len(limited) != 1 {
		t.Fatalf("expected 1 pending request with limit, got %d", len(limited))
	}
}

func TestPing(t *testing.T) {
	tdir := t.TempDir()
	db, err := Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer db.Close()

	if err := db.Ping(context.Background()); err != nil {
		t.Fatalf("ping: %v", err)
	}

	db.Close()
	if err := db.Ping(context.Background()); err == nil {
		t.Fatal("expected error pinging a closed database")
	}
}

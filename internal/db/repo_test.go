package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateAndCRUD(t *testing.T) {
	tdir := t.TempDir()
	path := filepath.Join(tdir, "scriptorum.db")
	db, err := Open(path)
	if err != nil { t.Fatalf("open: %v", err) }
	defer db.Close()
	if err := db.Migrate(context.Background()); err != nil { t.Fatalf("migrate: %v", err) }

	req := &Request{RequesterEmail:"user@example.com", Title:"Book", Authors:[]string{"Alice"}, Format:"ebook", Status:"pending"}
	id, err := db.CreateRequest(context.Background(), req)
	if err != nil || id == 0 { t.Fatalf("create: %v %d", err, id) }

	items, err := db.ListRequests(context.Background(), "", 10)
	if err != nil || len(items)==0 { t.Fatalf("list: %v %d", err, len(items)) }

	if err := db.ApproveRequest(context.Background(), id, "admin@example.com"); err != nil { t.Fatalf("approve: %v", err) }

	if err := db.UpdateRequestStatus(context.Background(), id, "queued", "ok", "admin@example.com", []byte(`{}`), []byte(`{}`)); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(path); err != nil { t.Fatalf("db file missing: %v", err) }
}

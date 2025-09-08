package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestEnsureFirstRunCreatesConfigAndDB(t *testing.T) {
	tdir := t.TempDir()
	cfg := filepath.Join(tdir, "config.yaml")
	dbp := filepath.Join(tdir, "scriptorum.db")

	c, d, err := EnsureFirstRun(context.Background(), cfg, dbp)
	if err != nil {
		t.Fatalf("EnsureFirstRun: %v", err)
	}
	t.Cleanup(func() { _ = d.Close() })

	if c.DB.Path == "" || c.HTTP.Listen == "" {
		t.Fatalf("config not initialized: %+v", c)
	}
	if _, err := os.Stat(cfg); err != nil {
		t.Fatalf("config not written: %v", err)
	}
	if _, err := os.Stat(dbp); err != nil {
		t.Fatalf("db not created: %v", err)
	}
}

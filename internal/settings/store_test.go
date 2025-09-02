package settings

import (
	"os"
	"path/filepath"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

func TestStoreUpdateAndGet(t *testing.T) {
	tdir := t.TempDir()
	path := filepath.Join(tdir, "config.yaml")
	cfg := &config.Config{}
	s := New(path, cfg)

	cfg.Admins.Emails = []string{"a@example.com"}
	if err := s.Update(cfg); err != nil {
		t.Fatalf("update: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("config not saved: %v", err)
	}

	got := s.Get()
	if len(got.Admins.Emails) != 1 {
		t.Fatalf("get mismatch: %+v", got)
	}
}

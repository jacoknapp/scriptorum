package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveAndLoadRoundtrip(t *testing.T) {
	tdir := t.TempDir()
	path := filepath.Join(tdir, "config.yaml")

	cfg := &Config{}
	cfg.HTTP.Listen = ":9090"
	cfg.DB.Path = filepath.Join(tdir, "db.sqlite")
	cfg.Admins.Usernames = []string{"admin"}
	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = "https://issuer.example"
	cfg.OAuth.ClientID = "id"
	cfg.OAuth.ClientSecret = "secret"
	cfg.OAuth.RedirectURL = "http://localhost:9090/oauth/callback"
	cfg.Setup.Completed = true

	if err := Save(path, cfg); err != nil {
		t.Fatalf("save: %v", err)
	}
	if _, err := os.Stat(path); err != nil {
		t.Fatalf("missing file: %v", err)
	}

	got, err := Load(path)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	if got.HTTP.Listen != cfg.HTTP.Listen || got.DB.Path != cfg.DB.Path {
		t.Fatalf("mismatch after load")
	}
	if len(got.Admins.Usernames) != 1 || got.Admins.Usernames[0] != "admin" {
		t.Fatalf("admins mismatch: %+v", got.Admins)
	}
	if !got.OAuth.Enabled || got.OAuth.Issuer == "" {
		t.Fatalf("oauth mismatch: %+v", got.OAuth)
	}
}

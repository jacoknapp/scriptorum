package bootstrap

import (
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"

	_ "modernc.org/sqlite"
)

// Test that bootstrap creates configuration with correct default port
func TestBootstrapDefaultPort(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "scriptorum.yaml")
	dbPath := filepath.Join(tempDir, "scriptorum.db")

	cfg, database, err := EnsureFirstRun(context.Background(), configPath, dbPath)
	if err != nil {
		t.Fatalf("EnsureFirstRun failed: %v", err)
	}
	// Close database to allow cleanup
	defer func() {
		if database != nil {
			database.Close()
		}
	}()

	// Verify config file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Check that the default port is 8491
	if cfg.HTTP.Listen != ":8491" {
		t.Errorf("Expected default port to be ':8491', got '%s'", cfg.HTTP.Listen)
	}
}

// Test that bootstrap doesn't create automation settings (they were removed)
func TestBootstrapNoAutomationSettings(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "scriptorum.yaml")
	dbPath := filepath.Join(tempDir, "scriptorum.db")

	_, database, err := EnsureFirstRun(context.Background(), configPath, dbPath)
	if err != nil {
		t.Fatalf("EnsureFirstRun failed: %v", err)
	}
	// Close database to allow cleanup
	defer func() {
		if database != nil {
			database.Close()
		}
	}()

	// Read the raw config file content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	configStr := string(content)

	// Check that automation-related fields are not present
	automationFields := []string{
		"automation:",
		"auto_approve:",
		"auto_decline:",
		"rules:",
	}

	for _, field := range automationFields {
		if contains(configStr, field) {
			t.Errorf("Found automation field '%s' in generated config, but automation settings should be removed", field)
		}
	}
}

// Test that essential configuration sections are still present
func TestBootstrapEssentialSections(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "scriptorum.yaml")
	dbPath := filepath.Join(tempDir, "scriptorum.db")

	cfg, database, err := EnsureFirstRun(context.Background(), configPath, dbPath)
	if err != nil {
		t.Fatalf("EnsureFirstRun failed: %v", err)
	}
	// Close database to allow cleanup
	defer func() {
		if database != nil {
			database.Close()
		}
	}()

	// Verify essential sections exist
	if cfg.HTTP.Listen == "" {
		t.Error("HTTP listen configuration is missing")
	}

	if cfg.DB.Path == "" {
		t.Error("Database path configuration is missing")
	}

	// No default admins should exist prior to setup; setup flow will create them
	if len(cfg.Admins.Usernames) != 0 {
		t.Error("Expected no default admin usernames before setup")
	}
}

// Test that Readarr endpoints are not in bootstrap config
func TestBootstrapNoReadarrEndpoints(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "scriptorum.yaml")
	dbPath := filepath.Join(tempDir, "scriptorum.db")

	_, database, err := EnsureFirstRun(context.Background(), configPath, dbPath)
	if err != nil {
		t.Fatalf("EnsureFirstRun failed: %v", err)
	}
	// Close database to allow cleanup
	defer func() {
		if database != nil {
			database.Close()
		}
	}()

	// Read the raw config file content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	configStr := string(content)

	// Check that hardcoded endpoint fields are not present in the config file
	endpointFields := []string{
		"lookup_endpoint:",
		"add_endpoint:",
		"add_method:",
		"add_payload_template:",
	}

	for _, field := range endpointFields {
		if contains(configStr, field) {
			t.Errorf("Found endpoint field '%s' in generated config, but endpoints should be hardcoded", field)
		}
	}
}

// Helper function to check if a string contains a substring (case-insensitive)
func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(substr) == 0 ||
		strings.Contains(strings.ToLower(s), strings.ToLower(substr)))
}

func TestEnsureFirstRunMigratesExistingDatabaseOnStartup(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "scriptorum.yaml")
	dbPath := filepath.Join(tempDir, "scriptorum.db")

	rawDB, err := sql.Open("sqlite", "file:"+dbPath)
	if err != nil {
		t.Fatalf("open raw db: %v", err)
	}
	t.Cleanup(func() { _ = rawDB.Close() })

	if _, err := rawDB.Exec(`
CREATE TABLE requests (
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
)`); err != nil {
		t.Fatalf("seed requests table: %v", err)
	}

	cfg, database, err := EnsureFirstRun(context.Background(), configPath, dbPath)
	if err != nil {
		t.Fatalf("EnsureFirstRun failed: %v", err)
	}
	defer database.Close()

	if cfg.DB.Path != dbPath {
		t.Fatalf("expected db path %q, got %q", dbPath, cfg.DB.Path)
	}

	var version int
	if err := database.SQL().QueryRowContext(context.Background(), `PRAGMA user_version`).Scan(&version); err != nil {
		t.Fatalf("query schema version: %v", err)
	}
	if version == 0 {
		t.Fatal("expected startup migration to set schema version")
	}

	var indexName string
	if err := database.SQL().QueryRowContext(context.Background(), `SELECT name FROM sqlite_master WHERE type='index' AND name='idx_requests_requester_email_id'`).Scan(&indexName); err != nil {
		t.Fatalf("expected startup migration to create requests index: %v", err)
	}
}

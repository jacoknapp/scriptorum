package bootstrap

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
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

	if len(cfg.Admins.Usernames) == 0 {
		t.Error("Admin usernames configuration is missing")
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

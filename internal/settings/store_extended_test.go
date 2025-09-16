package settings

import (
	"os"
	"path/filepath"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
)

// Test store initialization
func TestStoreInitialization(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(configPath, cfg)
	if store == nil {
		t.Fatal("Expected store to be non-nil")
	}

	// Test Get method
	retrievedCfg := store.Get()
	if retrievedCfg == nil {
		t.Fatal("Expected config to be non-nil")
		return
	}

	if retrievedCfg.HTTP.Listen != ":8080" {
		t.Errorf("Expected listen ':8080', got %s", retrievedCfg.HTTP.Listen)
	}
}

// Test updating configuration
func TestUpdateConfiguration(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	initialCfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(configPath, initialCfg)

	// Update configuration
	newCfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":9090"},
	}

	err := store.Update(newCfg)
	if err != nil {
		t.Fatalf("Failed to update configuration: %v", err)
	}

	// Verify update
	retrievedCfg := store.Get()
	if retrievedCfg.HTTP.Listen != ":9090" {
		t.Errorf("Expected updated listen ':9090', got %s", retrievedCfg.HTTP.Listen)
	}

	// Verify file was written
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Configuration file should exist after update")
	}
}

// Test concurrent access
func TestConcurrentAccess(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "concurrent_config.yaml")

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(configPath, cfg)

	done := make(chan bool, 10)
	errors := make(chan error, 10)

	// Run concurrent operations
	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Each goroutine reads configuration
			cfg := store.Get()
			if cfg == nil {
				errors <- &testError{msg: "got nil config", id: id}
				return
			}

			// Each goroutine updates configuration with different port
			port := ":800" + string(rune('0'+id))
			newCfg := &config.Config{
				HTTP: struct {
					Listen string `yaml:"listen"`
				}{Listen: port},
			}

			err := store.Update(newCfg)
			if err != nil {
				errors <- err
				return
			}
		}(i)
	}

	// Wait for all goroutines
	errorCount := 0
	for i := 0; i < 10; i++ {
		select {
		case <-done:
			// Success
		case err := <-errors:
			t.Errorf("Concurrent access error: %v", err)
			errorCount++
		}
	}

	if errorCount > 0 {
		t.Errorf("Had %d concurrent access errors", errorCount)
	}
}

// Test file operations
func TestFileOperations(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "file_ops_config.yaml")

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
		DB: struct {
			Path string `yaml:"path"`
		}{Path: "/path/to/db"},
	}

	store := New(configPath, cfg)

	// Update should create the file
	err := store.Update(cfg)
	if err != nil {
		t.Fatalf("Failed to update configuration: %v", err)
	}

	// File should exist
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Error("Configuration file should exist")
	}

	// Read file content
	content, err := os.ReadFile(configPath)
	if err != nil {
		t.Fatalf("Failed to read config file: %v", err)
	}

	// Should contain YAML content
	if len(content) == 0 {
		t.Error("Configuration file should not be empty")
	}
}

// Test error handling
func TestErrorHandling(t *testing.T) {
	// Test with invalid path (directory that doesn't exist and can't be created)
	invalidPath := "/invalid/path/that/cannot/be/created/config.yaml"

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(invalidPath, cfg)

	// Update should fail
	err := store.Update(cfg)
	if err == nil {
		t.Error("Expected error when writing to invalid path, got nil")
	}

	// Get should still work (returns in-memory config)
	retrievedCfg := store.Get()
	if retrievedCfg == nil {
		t.Error("Get should return in-memory config even if file operations fail")
	}
}

// Test thread safety of Get method
func TestGetThreadSafety(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "thread_safe_config.yaml")

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(configPath, cfg)

	done := make(chan bool, 20)

	// Run many concurrent Get operations
	for i := 0; i < 20; i++ {
		go func() {
			defer func() { done <- true }()

			// Should not panic or return nil
			cfg := store.Get()
			if cfg == nil {
				t.Error("Get returned nil config")
			}
		}()
	}

	// Wait for all goroutines
	for i := 0; i < 20; i++ {
		<-done
	}
}

// Helper type for testing
type testError struct {
	msg string
	id  int
}

func (e *testError) Error() string {
	return e.msg
}

// Benchmark store operations
func BenchmarkStoreGet(b *testing.B) {
	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New("/tmp/bench_config.yaml", cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		store.Get()
	}
}

func BenchmarkStoreUpdate(b *testing.B) {
	tmpDir := b.TempDir()
	configPath := filepath.Join(tmpDir, "bench_config.yaml")

	cfg := &config.Config{
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{Listen: ":8080"},
	}

	store := New(configPath, cfg)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		port := ":800" + string(rune('0'+(i%10)))
		newCfg := &config.Config{
			HTTP: struct {
				Listen string `yaml:"listen"`
			}{Listen: port},
		}
		store.Update(newCfg)
	}
}

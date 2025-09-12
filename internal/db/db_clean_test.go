package db

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// Helper function to create a temporary database for testing
func createTempDB(t *testing.T) (*DB, func()) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	return database, func() {
		database.Close()
		os.Remove(dbPath)
	}
}

// Test basic CRUD operations
func TestBasicCRUDOperations(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test table
	err := database.Exec(ctx, `CREATE TABLE test_table (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Insert test data
	err = database.Exec(ctx, `INSERT INTO test_table (name) VALUES (?)`, "test_name")
	if err != nil {
		t.Fatalf("Failed to insert test data: %v", err)
	}

	// Query test data
	var name string
	err = database.SQL().QueryRowContext(ctx, `SELECT name FROM test_table WHERE id = ?`, 1).Scan(&name)
	if err != nil {
		t.Fatalf("Failed to query test data: %v", err)
	}

	if name != "test_name" {
		t.Errorf("Expected name 'test_name', got %s", name)
	}
}

// Test database connection
func TestDatabaseConnection(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "connection_test.db")

	// Test opening database
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Test that SQL() returns valid connection
	sqlDB := database.SQL()
	if sqlDB == nil {
		t.Error("SQL() returned nil database connection")
	}

	// Test ping
	if err := sqlDB.Ping(); err != nil {
		t.Errorf("Database ping failed: %v", err)
	}
}

// Test error handling
func TestErrorHandling(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Test invalid SQL
	err := database.Exec(ctx, "INVALID SQL STATEMENT")
	if err == nil {
		t.Error("Expected error for invalid SQL, got nil")
	}
}

// Test transaction support through SQL()
func TestTransactionSupport(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test table
	err := database.Exec(ctx, `CREATE TABLE transaction_test (id INTEGER PRIMARY KEY, value TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Begin transaction through SQL()
	tx, err := database.SQL().BeginTx(ctx, nil)
	if err != nil {
		t.Fatalf("Failed to begin transaction: %v", err)
	}

	// Insert data in transaction
	_, err = tx.ExecContext(ctx, `INSERT INTO transaction_test (value) VALUES (?)`, "tx_value")
	if err != nil {
		tx.Rollback()
		t.Fatalf("Failed to insert in transaction: %v", err)
	}

	// Commit transaction
	if err := tx.Commit(); err != nil {
		t.Fatalf("Failed to commit transaction: %v", err)
	}

	// Verify data was committed
	var value string
	err = database.SQL().QueryRowContext(ctx, `SELECT value FROM transaction_test WHERE id = ?`, 1).Scan(&value)
	if err != nil {
		t.Fatalf("Failed to query committed data: %v", err)
	}

	if value != "tx_value" {
		t.Errorf("Expected value 'tx_value', got %s", value)
	}
}

// Test concurrent access
func TestConcurrentAccess(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test table
	err := database.Exec(ctx, `CREATE TABLE concurrent_test (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Run concurrent operations
	done := make(chan bool, 10)
	errors := make(chan error, 10)

	for i := 0; i < 10; i++ {
		go func(id int) {
			defer func() { done <- true }()

			// Each goroutine inserts data
			err := database.Exec(ctx, `INSERT INTO concurrent_test (name) VALUES (?)`,
				"name"+string(rune('0'+id)))
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

// Test database file creation
func TestDatabaseFileCreation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "file_creation_test.db")

	// Database file should not exist initially
	if _, err := os.Stat(dbPath); !os.IsNotExist(err) {
		t.Error("Database file should not exist initially")
	}

	// Open database (should create file)
	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	// Database file should now exist
	if _, err := os.Stat(dbPath); os.IsNotExist(err) {
		t.Error("Database file should exist after opening")
	}
}

// Test database closing
func TestDatabaseClose(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "close_test.db")

	database, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Failed to open database: %v", err)
	}

	// Test that close doesn't error
	err = database.Close()
	if err != nil {
		t.Errorf("Database close failed: %v", err)
	}

	// Test that operations fail after close
	ctx := context.Background()
	err = database.Exec(ctx, "CREATE TABLE test (id INTEGER)")
	if err == nil {
		t.Error("Expected error when using closed database, got nil")
	}
}

// Test prepared statements through SQL()
func TestPreparedStatements(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Create test table
	err := database.Exec(ctx, `CREATE TABLE prepared_test (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		t.Fatalf("Failed to create test table: %v", err)
	}

	// Prepare statement through SQL()
	stmt, err := database.SQL().PrepareContext(ctx, `INSERT INTO prepared_test (name) VALUES (?)`)
	if err != nil {
		t.Fatalf("Failed to prepare statement: %v", err)
	}
	defer stmt.Close()

	// Execute prepared statement
	_, err = stmt.ExecContext(ctx, "prepared_name")
	if err != nil {
		t.Fatalf("Failed to execute prepared statement: %v", err)
	}

	// Verify data
	var name string
	err = database.SQL().QueryRowContext(ctx, `SELECT name FROM prepared_test WHERE id = ?`, 1).Scan(&name)
	if err != nil {
		t.Fatalf("Failed to query data: %v", err)
	}

	if name != "prepared_name" {
		t.Errorf("Expected name 'prepared_name', got %s", name)
	}
}

// Test database context cancellation
func TestContextCancellation(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	// Create cancelled context
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	// Operation should respect cancelled context
	err := database.Exec(ctx, `CREATE TABLE context_test (id INTEGER)`)
	if err == nil {
		t.Error("Expected error with cancelled context, got nil")
	}
}

// Test WAL mode is enabled
func TestWALMode(t *testing.T) {
	database, cleanup := createTempDB(t)
	defer cleanup()

	ctx := context.Background()

	// Query journal mode
	var journalMode string
	err := database.SQL().QueryRowContext(ctx, `PRAGMA journal_mode`).Scan(&journalMode)
	if err != nil {
		t.Fatalf("Failed to query journal mode: %v", err)
	}

	if journalMode != "wal" {
		t.Errorf("Expected WAL mode, got %s", journalMode)
	}
}

// Benchmark database operations
func BenchmarkDatabaseInsert(b *testing.B) {
	tmpDir := b.TempDir()
	dbPath := filepath.Join(tmpDir, "benchmark.db")

	database, err := Open(dbPath)
	if err != nil {
		b.Fatalf("Failed to open database: %v", err)
	}
	defer database.Close()

	ctx := context.Background()

	// Create test table
	err = database.Exec(ctx, `CREATE TABLE benchmark_test (id INTEGER PRIMARY KEY, name TEXT)`)
	if err != nil {
		b.Fatalf("Failed to create test table: %v", err)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		err := database.Exec(ctx, `INSERT INTO benchmark_test (name) VALUES (?)`, "benchmark_name")
		if err != nil {
			b.Fatalf("Failed to insert data: %v", err)
		}
	}
}

package util

import (
	"testing"
)

// Test FirstNonEmpty with all empty strings
func TestFirstNonEmptyAllEmpty(t *testing.T) {
	result := FirstNonEmpty("", "", "")
	if result != "" {
		t.Errorf("Expected empty string, got %s", result)
	}
}

// Test FirstNonEmpty with mixed empty and non-empty strings
func TestFirstNonEmptyMixed(t *testing.T) {
	result := FirstNonEmpty("", "second", "third")
	if result != "second" {
		t.Errorf("Expected 'second', got %s", result)
	}
}

// Test FirstNonEmpty with first string non-empty
func TestFirstNonEmptyFirst(t *testing.T) {
	result := FirstNonEmpty("first", "second", "third")
	if result != "first" {
		t.Errorf("Expected 'first', got %s", result)
	}
}

// Test FirstNonEmpty with no arguments
func TestFirstNonEmptyNoArgs(t *testing.T) {
	result := FirstNonEmpty()
	if result != "" {
		t.Errorf("Expected empty string for no arguments, got %s", result)
	}
}

// Test FirstNonEmpty with whitespace strings
func TestFirstNonEmptyWithWhitespace(t *testing.T) {
	// FirstNonEmpty trims whitespace, so " " is considered empty
	result := FirstNonEmpty("", "   ", "valid")
	if result != "valid" {
		t.Errorf("Expected 'valid', got %s", result)
	}

	// Test string with only spaces
	result = FirstNonEmpty("   ")
	if result != "" {
		t.Errorf("Expected empty string for whitespace-only input, got '%s'", result)
	}
}

// Test ToTitleCase basic functionality
func TestToTitleCaseSimple(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello", "Hello"},
		{"HELLO", "Hello"},
		{"hello world", "Hello World"},
		{"", ""},
	}

	for _, test := range tests {
		result := ToTitleCase(test.input)
		if result != test.expected {
			t.Errorf("ToTitleCase(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

// Test ToTitleCase with special characters
func TestToTitleCaseSpecialChars(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"hello-world", "Hello-World"}, // ToTitleCase capitalizes after hyphens
		{"with spaces", "With Spaces"}, // ToTitleCase capitalizes after spaces
		{"o'reilly", "O'Reilly"},       // ToTitleCase capitalizes after apostrophes
		{"TEST", "Test"},               // Converts to lowercase then title case
		{"test_case", "Test_case"},     // Underscore doesn't trigger capitalization
		{"123abc", "123abc"},           // Numbers don't get capitalized
		{"@#$%", "@#$%"},               // Special chars remain unchanged
	}

	for _, test := range tests {
		result := ToTitleCase(test.input)
		if result != test.expected {
			t.Errorf("ToTitleCase(%q) = %q, expected %q", test.input, result, test.expected)
		}
	}
}

// Test with empty inputs
func TestEmptyInputs(t *testing.T) {
	if FirstNonEmpty("") != "" {
		t.Error("FirstNonEmpty with empty string should return empty")
	}

	if ToTitleCase("") != "" {
		t.Error("ToTitleCase with empty string should return empty")
	}
}

// Test with long strings
func TestLongStrings(t *testing.T) {
	longString := make([]rune, 1000)
	for i := range longString {
		longString[i] = 'a'
	}
	longStr := string(longString)

	// Should not panic
	result := ToTitleCase(longStr)
	if len(result) != 1000 {
		t.Errorf("Expected result length 1000, got %d", len(result))
	}
}

// Benchmark tests
func BenchmarkFirstNonEmpty(b *testing.B) {
	for i := 0; i < b.N; i++ {
		FirstNonEmpty("", "", "", "found")
	}
}

func BenchmarkToTitleCase(b *testing.B) {
	for i := 0; i < b.N; i++ {
		ToTitleCase("hello world this is a test string")
	}
}

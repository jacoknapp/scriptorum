package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"text/template"
)

// Test that hardcoded constants are set correctly
func TestReadarrConstants(t *testing.T) {
	// Test endpoint constants
	if readarrLookupEndpoint != "/api/v1/book/lookup" {
		t.Errorf("Expected readarrLookupEndpoint to be '/api/v1/book/lookup', got %s", readarrLookupEndpoint)
	}

	if readarrAddEndpoint != "/api/v1/book" {
		t.Errorf("Expected readarrAddEndpoint to be '/api/v1/book', got %s", readarrAddEndpoint)
	}

	if readarrAddMethod != "POST" {
		t.Errorf("Expected readarrAddMethod to be 'POST', got %s", readarrAddMethod)
	}
}

// Test that the hardcoded payload template is valid JSON template
func TestReadarrPayloadTemplate(t *testing.T) {
	// Parse the template to ensure it's valid
	tmpl, err := template.New("test").Funcs(template.FuncMap{
		"toJSON": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}).Parse(readarrAddPayloadTemplate)

	if err != nil {
		t.Fatalf("Payload template is not valid: %v", err)
	}

	// Test with sample data to ensure it generates valid JSON
	testData := struct {
		Candidate map[string]interface{}
		Opts      struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}
		Inst struct {
			DefaultQualityProfileID int
		}
	}{
		Candidate: map[string]interface{}{
			"id":               123,
			"title":            "Test Book",
			"authorTitle":      "Test Author",
			"seriesTitle":      "Test Series",
			"disambiguation":   "",
			"overview":         "Test overview",
			"authorId":         456,
			"foreignBookId":    "test-foreign-id",
			"foreignEditionId": "test-edition-id",
			"titleSlug":        "test-book",
			"monitored":        true,
			"anyEditionOk":     true,
			"ratings":          map[string]interface{}{"votes": 10, "value": 4.5},
			"releaseDate":      "2023-01-01T00:00:00Z",
			"pageCount":        300,
			"genres":           []string{"Fiction"},
			"author":           map[string]interface{}{"name": "Test Author"},
			"images":           []interface{}{},
			"links":            []interface{}{},
			"statistics":       map[string]interface{}{"bookFileCount": 1},
			"added":            "2023-01-01T00:00:00Z",
			"addOptions":       map[string]interface{}{"addType": "automatic", "searchForNewBook": true},
			"remoteCover":      "http://example.com/cover.jpg",
			"lastSearchTime":   "2023-01-01T00:00:00Z",
			"editions":         []interface{}{},
		},
		Opts: struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}{
			SearchForMissing: true,
			QualityProfileID: 1,
			RootFolderPath:   "/books",
			Tags:             []int{1, 2},
		},
		Inst: struct {
			DefaultQualityProfileID int
		}{
			DefaultQualityProfileID: 1,
		},
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, testData)
	if err != nil {
		t.Fatalf("Failed to execute template: %v", err)
	}

	// Validate that the result is valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	if err != nil {
		t.Fatalf("Template generated invalid JSON: %v\nJSON: %s", err, buf.String())
	}

	// Check that required fields are present
	requiredFields := []string{"id", "title", "authorTitle", "monitored", "addOptions"}
	for _, field := range requiredFields {
		if _, exists := result[field]; !exists {
			t.Errorf("Required field %s not found in generated JSON", field)
		}
	}
}

// Test that the payload template handles missing addOptions (like ebook payloads)
func TestReadarrPayloadTemplateEbookCase(t *testing.T) {
	// Parse the template to ensure it's valid
	tmpl, err := template.New("test").Funcs(template.FuncMap{
		"toJSON": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}).Parse(readarrAddPayloadTemplate)

	if err != nil {
		t.Fatalf("Payload template is not valid: %v", err)
	}

	// Test with ebook-style data (missing addOptions field)
	testData := struct {
		Candidate map[string]interface{}
		Opts      struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}
		Inst struct {
			DefaultQualityProfileID int
		}
	}{
		Candidate: map[string]interface{}{
			"title":             "Hello Beautiful",
			"titleSlug":         "97397727",
			"author":            map[string]interface{}{"name": "Ann Napolitano"},
			"editions":          []interface{}{map[string]interface{}{"foreignEditionId": "61771675", "monitored": true}},
			"foreignBookId":     "97397727",
			"foreignEditionId":  "61771675",
			"monitored":         true,
			"metadataProfileId": 1,
			// Note: NO addOptions field - this simulates ebook payloads from search.go
		},
		Opts: struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}{
			SearchForMissing: true,
			QualityProfileID: 1,
			RootFolderPath:   "/books",
			Tags:             []int{},
		},
		Inst: struct {
			DefaultQualityProfileID int
		}{
			DefaultQualityProfileID: 1,
		},
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, testData)
	if err != nil {
		t.Fatalf("Failed to execute template with ebook-style payload (no addOptions): %v", err)
	}

	// Validate that the result is valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	if err != nil {
		t.Fatalf("Template generated invalid JSON for ebook payload: %v\nJSON: %s", err, buf.String())
	}

	// Check that addOptions was created with defaults
	addOptions, exists := result["addOptions"].(map[string]interface{})
	if !exists {
		t.Fatalf("addOptions not found in generated JSON")
	}

	// Verify defaults are applied when original payload lacks addOptions
	if addOptions["addType"] != "automatic" {
		t.Errorf("Expected addType to default to 'automatic', got %v", addOptions["addType"])
	}

	if addOptions["searchForNewBook"] != true {
		t.Errorf("Expected searchForNewBook to default to true, got %v", addOptions["searchForNewBook"])
	}

	t.Log("SUCCESS: Template correctly handled ebook payload without addOptions field")
}

// Test Readarr construction with hardcoded endpoints
func TestReadarrConstructor(t *testing.T) {
	instance := ReadarrInstance{
		BaseURL: "http://test-readarr:8787",
		APIKey:  "test-api-key",
	}

	readarr := NewReadarrWithDB(instance, nil)

	if readarr == nil {
		t.Fatal("NewReadarrWithDB returned nil")
	}

	if readarr.inst.BaseURL != "http://test-readarr:8787" {
		t.Errorf("Expected BaseURL to be preserved, got %s", readarr.inst.BaseURL)
	}

	if readarr.inst.APIKey != "test-api-key" {
		t.Errorf("Expected APIKey to be preserved, got %s", readarr.inst.APIKey)
	}
}

// Test PingLookup uses hardcoded lookup endpoint
func TestReadarrPingLookupUsesCorrectEndpoint(t *testing.T) {
	var capturedURL string

	readarr := NewReadarrWithDB(ReadarrInstance{
		BaseURL: "http://test-readarr:8787",
		APIKey:  "test-key",
	}, nil)

	readarr.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.Path
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("[]")),
			Header:     make(http.Header),
		}, nil
	})

	err := readarr.PingLookup(context.Background())
	if err != nil {
		t.Fatalf("PingLookup failed: %v", err)
	}

	if capturedURL != readarrLookupEndpoint {
		t.Errorf("Expected PingLookup to use %s, but used %s", readarrLookupEndpoint, capturedURL)
	}
}

// Test that LookupByTerm uses hardcoded lookup endpoint
func TestReadarrLookupByTermUsesCorrectEndpoint(t *testing.T) {
	var capturedURL string
	var capturedQuery string

	readarr := NewReadarrWithDB(ReadarrInstance{
		BaseURL: "http://test-readarr:8787",
		APIKey:  "test-key",
	}, nil)

	readarr.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.Path
		capturedQuery = req.URL.RawQuery
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("[]")),
			Header:     make(http.Header),
		}, nil
	})

	_, err := readarr.LookupByTerm(context.Background(), "test query")
	if err != nil {
		t.Fatalf("LookupByTerm failed: %v", err)
	}

	if capturedURL != readarrLookupEndpoint {
		t.Errorf("Expected LookupByTerm to use %s, but used %s", readarrLookupEndpoint, capturedURL)
	}

	if !strings.Contains(capturedQuery, "term=test+query") {
		t.Errorf("Expected query to contain search term, got %s", capturedQuery)
	}
}

// Test that AddBook uses hardcoded add endpoint and method
func TestReadarrAddBookUsesCorrectEndpointAndMethod(t *testing.T) {
	var capturedURL string
	var capturedMethod string
	var capturedBody []byte

	readarr := NewReadarrWithDB(ReadarrInstance{
		BaseURL: "http://test-readarr:8787",
		APIKey:  "test-key",
	}, nil)

	readarr.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		capturedURL = req.URL.Path
		capturedMethod = req.Method
		if req.Body != nil {
			capturedBody, _ = io.ReadAll(req.Body)
		}
		return &http.Response{
			StatusCode: 200,
			Body:       io.NopCloser(strings.NewReader("{\"id\": 1}")),
			Header:     make(http.Header),
		}, nil
	})

	candidate := Candidate{
		"id":            123,
		"title":         "Test Book",
		"authorTitle":   "Test Author",
		"foreignBookId": "test-id",
		"authorId":      456,
		"addOptions":    map[string]any{},
	}

	opts := AddOpts{
		QualityProfileID: 1,
		RootFolderPath:   "/books",
		Tags:             []int{},
		SearchForMissing: false,
	}

	_, _, err := readarr.AddBook(context.Background(), candidate, opts)
	if err != nil {
		t.Fatalf("AddBook failed: %v", err)
	}

	if capturedURL != readarrAddEndpoint {
		t.Errorf("Expected AddBook to use %s, but used %s", readarrAddEndpoint, capturedURL)
	}

	if capturedMethod != readarrAddMethod {
		t.Errorf("Expected AddBook to use %s method, but used %s", readarrAddMethod, capturedMethod)
	}

	// Verify the request body contains expected JSON structure
	if len(capturedBody) == 0 {
		t.Error("Expected request body to contain JSON payload")
	} else {
		var payload map[string]interface{}
		if err := json.Unmarshal(capturedBody, &payload); err != nil {
			t.Errorf("Request body is not valid JSON: %v", err)
		} else {
			// Check key fields are present
			if payload["title"] != "Test Book" {
				t.Errorf("Expected title 'Test Book', got %v", payload["title"])
			}
			if payload["authorTitle"] != "Test Author" {
				t.Errorf("Expected authorTitle 'Test Author', got %v", payload["authorTitle"])
			}
		}
	}
}

// Test error handling when endpoints return non-200 status
func TestReadarrErrorHandling(t *testing.T) {
	readarr := NewReadarrWithDB(ReadarrInstance{
		BaseURL: "http://test-readarr:8787",
		APIKey:  "test-key",
	}, nil)

	// Test 404 error
	readarr.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 404,
			Body:       io.NopCloser(strings.NewReader("Not Found")),
			Header:     make(http.Header),
		}, nil
	})

	_, err := readarr.LookupByTerm(context.Background(), "test")
	if err == nil {
		t.Error("Expected error for 404 response")
	}

	// Test 500 error
	readarr.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		return &http.Response{
			StatusCode: 500,
			Body:       io.NopCloser(strings.NewReader("Internal Server Error")),
			Header:     make(http.Header),
		}, nil
	})

	err = readarr.PingLookup(context.Background())
	if err == nil {
		t.Error("Expected error for 500 response")
	}
}

// Test template execution with minimal data
func TestReadarrTemplateWithMinimalData(t *testing.T) {
	tmpl, err := template.New("test").Funcs(template.FuncMap{
		"toJSON": func(v interface{}) string {
			b, _ := json.Marshal(v)
			return string(b)
		},
	}).Parse(readarrAddPayloadTemplate)

	if err != nil {
		t.Fatalf("Payload template is not valid: %v", err)
	}

	// Test with minimal data - should use defaults
	testData := struct {
		Candidate map[string]interface{}
		Opts      struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}
		Inst struct {
			DefaultQualityProfileID int
		}
	}{
		Candidate: map[string]interface{}{
			"title":       "Minimal Book",
			"authorTitle": "Minimal Author",
			"addOptions":  map[string]interface{}{},
		},
		Opts: struct {
			SearchForMissing bool
			QualityProfileID int
			RootFolderPath   string
			Tags             []int
		}{
			SearchForMissing: false,
			QualityProfileID: 0, // Should use default
			RootFolderPath:   "/books",
			Tags:             []int{},
		},
		Inst: struct {
			DefaultQualityProfileID int
		}{
			DefaultQualityProfileID: 2,
		},
	}

	var buf bytes.Buffer
	err = tmpl.Execute(&buf, testData)
	if err != nil {
		t.Fatalf("Failed to execute template with minimal data: %v", err)
	}

	// Validate that the result is valid JSON
	var result map[string]interface{}
	err = json.Unmarshal(buf.Bytes(), &result)
	if err != nil {
		t.Fatalf("Template generated invalid JSON with minimal data: %v\nJSON: %s", err, buf.String())
	}

	// Check that defaults are applied
	if result["monitored"] != true {
		t.Errorf("Expected default monitored=true, got %v", result["monitored"])
	}

	if result["anyEditionOk"] != true {
		t.Errorf("Expected default anyEditionOk=true, got %v", result["anyEditionOk"])
	}

	if result["qualityProfileId"] != float64(2) { // JSON unmarshals numbers as float64
		t.Errorf("Expected default qualityProfileId=2, got %v", result["qualityProfileId"])
	}
}

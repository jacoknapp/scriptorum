package providers

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const apiVersionPrefix = "/api/v1"

type LookupBook struct {
	Title     string         `json:"title"`
	TitleSlug string         `json:"titleSlug"`
	Author    map[string]any `json:"author"`
	// Some Readarr responses include an `authors` array instead of a single `author` object.
	// Decode both so we can find an author id in either location.
	Authors []map[string]any `json:"authors"`
	// Readarr lookup may include authorId and authorTitle instead of author object
	AuthorId    int              `json:"authorId"`
	AuthorTitle string           `json:"authorTitle"`
	Identifiers []map[string]any `json:"identifiers"`
	Editions    []any            `json:"editions"`
}

type ReadarrInstance struct {
	BaseURL                 string
	APIKey                  string
	LookupEndpoint          string
	AddEndpoint             string
	AddMethod               string
	AddPayloadTemplate      string
	DefaultQualityProfileID int
	DefaultRootFolderPath   string
	DefaultTags             []string
}

type Readarr struct {
	inst ReadarrInstance
	cl   *http.Client
	db   *sql.DB // Database connection for caching
}

func NewReadarr(i ReadarrInstance) *Readarr {
	return &Readarr{inst: normalize(i), cl: &http.Client{Timeout: 12 * time.Second}, db: nil}
}

func NewReadarrWithDB(i ReadarrInstance, db *sql.DB) *Readarr {
	r := &Readarr{inst: normalize(i), cl: &http.Client{Timeout: 12 * time.Second}, db: db}
	if db != nil {
		r.initCacheTables()
	}
	return r
}

func normalize(i ReadarrInstance) ReadarrInstance {
	i.BaseURL = strings.TrimRight(i.BaseURL, "/")
	if !strings.Contains(i.LookupEndpoint, apiVersionPrefix) {
		i.LookupEndpoint = apiVersionPrefix + "/book/lookup"
	}
	if !strings.Contains(i.AddEndpoint, apiVersionPrefix) {
		i.AddEndpoint = apiVersionPrefix + "/book"
	}
	if strings.TrimSpace(i.AddMethod) == "" {
		i.AddMethod = http.MethodPost
	}
	return i
}

func (r *Readarr) PingLookup(ctx context.Context) error {
	// Include apikey in query to be resilient to proxies that strip X-Api-Key
	u := r.inst.BaseURL + r.inst.LookupEndpoint + "?term=" + url.QueryEscape("test") + "&apikey=" + url.QueryEscape(r.inst.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := r.cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return fmt.Errorf("HTTP %s: %s", resp.Status, bodyStr)
	}
	return nil
}

type Candidate map[string]any

type AddOpts struct {
	QualityProfileID int
	RootFolderPath   string
	SearchForMissing bool
	Tags             []string
}

func (r *Readarr) AddBook(ctx context.Context, candidate Candidate, opts AddOpts) ([]byte, []byte, error) {
	tpl, err := template.New("payload").Funcs(template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}).Parse(r.inst.AddPayloadTemplate)
	if err != nil {
		return nil, nil, err
	}
	buf := &bytes.Buffer{}
	if err := tpl.Execute(buf, map[string]any{"Candidate": candidate, "Opts": opts}); err != nil {
		return nil, nil, err
	}

	payload := buf.Bytes()
	// Include apikey in query to be resilient to proxies that strip X-Api-Key
	u := r.inst.BaseURL + r.inst.AddEndpoint
	if strings.Contains(u, "?") {
		u += "&apikey=" + url.QueryEscape(r.inst.APIKey)
	} else {
		u += "?apikey=" + url.QueryEscape(r.inst.APIKey)
	}
	req, _ := http.NewRequestWithContext(ctx, r.inst.AddMethod, u, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := r.cl.Do(req)
	if err != nil {
		return payload, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		bodyStr := string(respBody)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		safeURL := redactAPIKey(u)
		return payload, respBody, fmt.Errorf("add book failed (HTTP %s) to %s: %s", resp.Status, safeURL, bodyStr)
	}
	return payload, respBody, nil
}

// ----- Lookup & matching (ISBN13 -> ISBN10 -> ASIN) -----

func (r *Readarr) LookupByTerm(ctx context.Context, term string) ([]LookupBook, error) {
	// Check cache first
	cacheKey := "lookup:" + strings.ToLower(term)
	if cached, found := r.getCachedData(cacheKey, "lookup"); found {
		var books []LookupBook
		if err := json.Unmarshal([]byte(cached), &books); err == nil {
			return books, nil
		}
	}

	// Include apikey in query to be resilient to proxies that strip X-Api-Key
	u := r.inst.BaseURL + r.inst.LookupEndpoint + "?term=" + url.QueryEscape(term) + "&apikey=" + url.QueryEscape(r.inst.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("HTTP %s (ct=%s): %s", resp.Status, resp.Header.Get("Content-Type"), bodyStr)
	}
	// Read the entire response body first
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %v", err)
	}

	// Try to decode as JSON
	var arr []LookupBook
	if err := json.Unmarshal(body, &arr); err != nil {
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		// redact apikey in URL if present
		safeURL := redactAPIKey(u)
		return nil, fmt.Errorf("invalid JSON (ct=%s) (HTTP %s) from %s: %s", resp.Header.Get("Content-Type"), resp.Status, safeURL, bodyStr)
	}

	// Cache the results for 1 hour
	if data, err := json.Marshal(arr); err == nil {
		r.setCachedData(cacheKey, "lookup", string(data), time.Hour)
	}

	// Debug: dump the full JSON response
	fmt.Printf("DEBUG: Full Readarr lookup JSON: %s\n", string(body))
	return arr, nil
}

// FindAuthorIDByName searches Readarr for an author by name. If found, returns the id.
// If no author is found, returns (0, nil). Returns a non-nil error only for transport/parse errors.
func (r *Readarr) FindAuthorIDByName(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}

	// Check cache first
	if cachedID, found := r.getCachedAuthor(name); found {
		return cachedID, nil
	}

	// Use the author lookup endpoint
	u := r.inst.BaseURL + "/api/v1/author/lookup?term=" + url.QueryEscape(name) + "&apikey=" + url.QueryEscape(r.inst.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.cl.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return 0, fmt.Errorf("HTTP %s (ct=%s): %s", resp.Status, resp.Header.Get("Content-Type"), bodyStr)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, err
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return 0, fmt.Errorf("invalid JSON from author lookup: %v", err)
	}

	var foundID int
	for _, a := range arr {
		if nm, _ := a["name"].(string); nm != "" && strings.EqualFold(strings.TrimSpace(nm), name) {
			if idv, ok := a["id"]; ok {
				switch v := idv.(type) {
				case float64:
					foundID = int(v)
				case int:
					foundID = v
				case int64:
					foundID = int(v)
				case string:
					if i, err := strconv.Atoi(v); err == nil {
						foundID = i
					}
				}
				if foundID > 0 {
					r.setCachedAuthor(name, foundID)
					return foundID, nil
				}
			}
		}
	}
	// no exact match found; if we have results, prefer the first with an id
	for _, a := range arr {
		if idv, ok := a["id"]; ok {
			switch v := idv.(type) {
			case float64:
				foundID = int(v)
			case int:
				foundID = v
			case int64:
				foundID = int(v)
			case string:
				if i, err := strconv.Atoi(v); err == nil {
					foundID = i
				}
			}
			if foundID > 0 {
				r.setCachedAuthor(name, foundID)
				return foundID, nil
			}
		}
	}
	return 0, nil
}

// CreateAuthor creates a new author record in Readarr and returns its id.
func (r *Readarr) CreateAuthor(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, fmt.Errorf("empty author name")
	}
	// Check if required defaults are set
	if r.inst.DefaultQualityProfileID == 0 || r.inst.DefaultRootFolderPath == "" {
		return 0, fmt.Errorf("cannot create author: missing default qualityProfileId or rootFolderPath")
	}

	// Build the payload for author creation
	payload := map[string]any{
		"name":              name,
		"qualityProfileId":  r.inst.DefaultQualityProfileID,
		"metadataProfileId": 1, // Default metadata profile ID
		"rootFolderPath":    r.inst.DefaultRootFolderPath,
		"addOptions": map[string]any{
			"searchForMissingBooks": false,
		},
	}

	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		return 0, fmt.Errorf("failed to marshal author payload: %v", err)
	}

	// Debug: log the payload we will send (sanitized)
	fmt.Printf("DEBUG: Readarr create author payload: %s\n", string(payloadBytes))

	// Send POST request to create author
	u := r.inst.BaseURL + "/api/v1/author"
	if strings.Contains(u, "?") {
		u += "&apikey=" + url.QueryEscape(r.inst.APIKey)
	} else {
		u += "?apikey=" + url.QueryEscape(r.inst.APIKey)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(payloadBytes))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.cl.Do(req)
	if err != nil {
		return 0, fmt.Errorf("failed to create author: %v", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		// Try to parse the response body to extract validation errors for clearer debugging
		var parsed map[string]any
		var details string
		if err := json.Unmarshal(respBody, &parsed); err == nil {
			// Common Readarr error shapes include title/message and an `errors` map
			if t, ok := parsed["title"].(string); ok && t != "" {
				details += t
			}
			if m, ok := parsed["message"].(string); ok && m != "" {
				if details != "" {
					details += ": "
				}
				details += m
			}
			if errs, ok := parsed["errors"].(map[string]any); ok {
				// Flatten errors map into a short string
				parts := []string{}
				for k, v := range errs {
					parts = append(parts, fmt.Sprintf("%s=%v", k, v))
				}
				if len(parts) > 0 {
					if details != "" {
						details += "; "
					}
					details += strings.Join(parts, ", ")
				}
			}
		}
		if details == "" {
			// Fallback to the raw body (trimmed)
			bodyStr := strings.TrimSpace(string(respBody))
			if len(bodyStr) > 400 {
				bodyStr = bodyStr[:400] + "..."
			}
			details = bodyStr
		}
		// Log the full parsed response for debugging
		fmt.Printf("DEBUG: Readarr create author validation details: %s\n", details)

		// If validation likely complains about rootFolderPath, try a fallback create without it
		lower := strings.ToLower(details)
		if strings.Contains(lower, "root") || strings.Contains(lower, "rootfolder") || strings.Contains(lower, "rootfolderpath") {
			fallbackPayload := map[string]any{
				"name":              name,
				"qualityProfileId":  r.inst.DefaultQualityProfileID,
				"metadataProfileId": 1,
				"addOptions":        map[string]any{"searchForMissingBooks": false},
			}
			fbBytes, _ := json.Marshal(fallbackPayload)
			fbURL := r.inst.BaseURL + "/api/v1/author"
			if strings.Contains(fbURL, "?") {
				fbURL += "&apikey=" + url.QueryEscape(r.inst.APIKey)
			} else {
				fbURL += "?apikey=" + url.QueryEscape(r.inst.APIKey)
			}
			fbReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, fbURL, bytes.NewReader(fbBytes))
			fbReq.Header.Set("Content-Type", "application/json")
			fbReq.Header.Set("X-Api-Key", r.inst.APIKey)
			fbReq.Header.Set("User-Agent", "Scriptorum/1.0")
			fbReq.Header.Set("Accept", "application/json")

			fbResp, ferr := r.cl.Do(fbReq)
			if ferr == nil {
				fbBody, _ := io.ReadAll(fbResp.Body)
				fbResp.Body.Close()
				if fbResp.StatusCode < 400 {
					var created map[string]any
					if err := json.Unmarshal(fbBody, &created); err == nil {
						if idv, ok := created["id"]; ok {
							switch v := idv.(type) {
							case float64:
								return int(v), nil
							case int:
								return v, nil
							case int64:
								return int(v), nil
							}
						}
					}
					// If fallback succeeded but id not parsed, return raw success
					return 0, nil
				}
				// include fallback response in details for diagnostics
				details += "; fallback_attempt_response: " + strings.TrimSpace(string(fbBody))
			}
		}
		safeURL := redactAPIKey(u)
		return 0, fmt.Errorf("create author failed (HTTP %s) to %s: %s", resp.Status, safeURL, details)
	}

	// Parse the response to get the created author ID
	var createdAuthor map[string]any
	if err := json.Unmarshal(respBody, &createdAuthor); err != nil {
		return 0, fmt.Errorf("failed to parse author creation response: %v", err)
	}

	if idv, ok := createdAuthor["id"]; ok {
		var authorID int
		switch v := idv.(type) {
		case float64:
			authorID = int(v)
		case int:
			authorID = v
		case int64:
			authorID = int(v)
		}
		if authorID > 0 {
			r.setCachedAuthor(name, authorID)
			return authorID, nil
		}
	}

	return 0, fmt.Errorf("author created but no ID returned in response")
}

// redactAPIKey hides apikey query param values from logs/errors
func redactAPIKey(u string) string {
	if u == "" {
		return u
	}
	if strings.Contains(u, "apikey=") {
		// replace value after apikey=
		return regexp.MustCompile(`([?&]apikey=)[^&]+`).ReplaceAllString(u, "$1***")
	}
	return u
}

var nonDigit = regexp.MustCompile(`[^0-9Xx]`)

func cleanISBN(s string) string { return strings.ToUpper(nonDigit.ReplaceAllString(s, "")) }
func cleanASIN(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func hasIdent(b LookupBook, kind, value string) bool {
	v := strings.ToUpper(value)
	for _, id := range b.Identifiers {
		kt, _ := id["identifierType"].(string)
		vv, _ := id["value"].(string)
		if strings.EqualFold(kt, kind) && strings.ToUpper(vv) == v {
			return true
		}
	}
	return false
}

// parseAuthorNameFromTitle extracts author name from authorTitle like "andrews, ilona Burn for Me"
func parseAuthorNameFromTitle(title string) string {
	parts := strings.Split(strings.TrimSpace(title), " ")
	if len(parts) >= 2 {
		// Assume "lastname, firstname ..."
		last := strings.Trim(parts[0], ",")
		first := parts[1]
		return strings.Title(strings.ToLower(first + " " + last))
	}
	return strings.Title(strings.ToLower(strings.TrimSpace(title)))
}

func (r *Readarr) SelectCandidate(list []LookupBook, isbn13, isbn10, asin string) (Candidate, bool) {
	c13 := cleanISBN(isbn13)
	c10 := cleanISBN(isbn10)
	ca := cleanASIN(asin)

	pick := func(test func(LookupBook) bool) (Candidate, bool) {
		for _, b := range list {
			if !test(b) {
				continue
			}

			// Prefer a single `author` object if present, otherwise fall back to the first
			// entry in an `authors` array. If no author object, check AuthorId or AuthorTitle.
			var author map[string]any
			if b.Author != nil {
				author = b.Author
			} else if len(b.Authors) > 0 {
				author = b.Authors[0]
			} else if b.AuthorId > 0 {
				author = map[string]any{"id": b.AuthorId}
			} else if b.AuthorTitle != "" {
				name := parseAuthorNameFromTitle(b.AuthorTitle)
				author = map[string]any{"name": name}
			}
			if author != nil {
				return Candidate{"title": b.Title, "titleSlug": b.TitleSlug, "author": author, "editions": b.Editions}, true
			}
		}
		return nil, false
	}

	if c13 != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ISBN13", c13) }); ok {
			return cand, true
		}
	}
	if c10 != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ISBN10", c10) }); ok {
			return cand, true
		}
	}
	if ca != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ASIN", ca) }); ok {
			return cand, true
		}
	}
	return nil, false
}

// ----- Database caching methods -----

func (r *Readarr) initCacheTables() {
	if r.db == nil {
		return
	}
	// Create cache tables if they don't exist
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cache_key TEXT UNIQUE NOT NULL,
			cache_type TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME
		)
	`)
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_authors (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			readarr_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			author_id INTEGER,
			isbn13 TEXT,
			isbn10 TEXT,
			asin TEXT,
			readarr_data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (author_id) REFERENCES readarr_authors(id)
		)
	`)
}

func (r *Readarr) getCachedData(cacheKey, cacheType string) (string, bool) {
	if r.db == nil {
		return "", false
	}
	var data string
	err := r.db.QueryRow(`
		SELECT data FROM readarr_cache 
		WHERE cache_key = ? AND cache_type = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
	`, cacheKey, cacheType).Scan(&data)
	if err != nil {
		return "", false
	}
	return data, true
}

func (r *Readarr) setCachedData(cacheKey, cacheType, data string, ttl time.Duration) {
	if r.db == nil {
		return
	}
	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}
	r.db.Exec(`
		INSERT OR REPLACE INTO readarr_cache (cache_key, cache_type, data, expires_at)
		VALUES (?, ?, ?, ?)
	`, cacheKey, cacheType, data, expiresAt)
}

func (r *Readarr) getCachedAuthor(name string) (int, bool) {
	if r.db == nil {
		return 0, false
	}
	var readarrID int
	err := r.db.QueryRow(`
		SELECT readarr_id FROM readarr_authors 
		WHERE name = ? AND readarr_id IS NOT NULL
	`, strings.ToLower(name)).Scan(&readarrID)
	if err != nil {
		return 0, false
	}
	return readarrID, true
}

func (r *Readarr) setCachedAuthor(name string, readarrID int) {
	if r.db == nil {
		return
	}
	r.db.Exec(`
		INSERT OR REPLACE INTO readarr_authors (name, readarr_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, strings.ToLower(name), readarrID)
}

func (r *Readarr) getCachedBook(isbn13, isbn10, asin string) (*LookupBook, bool) {
	if r.db == nil {
		return nil, false
	}
	var data string
	query := `SELECT readarr_data FROM readarr_books WHERE `
	args := []any{}

	if isbn13 != "" {
		query += `isbn13 = ?`
		args = append(args, isbn13)
	} else if isbn10 != "" {
		query += `isbn10 = ?`
		args = append(args, isbn10)
	} else if asin != "" {
		query += `asin = ?`
		args = append(args, asin)
	} else {
		return nil, false
	}

	err := r.db.QueryRow(query, args...).Scan(&data)
	if err != nil {
		return nil, false
	}

	var book LookupBook
	if err := json.Unmarshal([]byte(data), &book); err != nil {
		return nil, false
	}
	return &book, true
}

func (r *Readarr) setCachedBook(book *LookupBook) {
	if r.db == nil || book == nil {
		return
	}
	data, _ := json.Marshal(book)
	r.db.Exec(`
		INSERT OR REPLACE INTO readarr_books (title, author_id, isbn13, isbn10, asin, readarr_data, updated_at)
		VALUES (?, ?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
	`, book.Title, getAuthorIDFromBook(book), book.Identifiers, "", "", string(data))
}

func getAuthorIDFromBook(book *LookupBook) *int {
	if book.Author != nil {
		if id, ok := book.Author["id"].(float64); ok {
			i := int(id)
			return &i
		}
	}
	if book.AuthorId > 0 {
		i := book.AuthorId
		return &i
	}
	return nil
}

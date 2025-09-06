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

	"gitea.knapp/jacoknapp/scriptorum/internal/util"
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
	AuthorId         int              `json:"authorId"`
	AuthorTitle      string           `json:"authorTitle"`
	ForeignBookId    string           `json:"foreignBookId"`
	ForeignEditionId string           `json:"foreignEditionId"`
	Identifiers      []map[string]any `json:"identifiers"`
	Editions         []any            `json:"editions"`
	RemoteCover      string           `json:"remoteCover"`
	RemotePoster     string           `json:"remotePoster"`
	CoverUrl         string           `json:"coverUrl"`
	Images           []struct {
		CoverType string `json:"coverType"`
		Url       string `json:"url"`
		RemoteUrl string `json:"remoteUrl"`
	} `json:"images"`
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

// sanitizeAndEnrichPayload applies defaults, shapes editions, normalizes authorId, and enriches the nested author object.
func (r *Readarr) sanitizeAndEnrichPayload(ctx context.Context, pmap map[string]any, opts AddOpts) map[string]any {
	if pmap == nil {
		return pmap
	}
	// Defaults
	if pmap["qualityProfileId"] == nil || fmt.Sprint(pmap["qualityProfileId"]) == "" || fmt.Sprint(pmap["qualityProfileId"]) == "0" {
		if opts.QualityProfileID != 0 {
			pmap["qualityProfileId"] = opts.QualityProfileID
		} else if qid := r.getValidQualityProfileID(ctx); qid != 0 {
			pmap["qualityProfileId"] = qid
		} else if r.inst.DefaultQualityProfileID != 0 {
			pmap["qualityProfileId"] = r.inst.DefaultQualityProfileID
		}
	}
	if pmap["metadataProfileId"] == nil || fmt.Sprint(pmap["metadataProfileId"]) == "" || fmt.Sprint(pmap["metadataProfileId"]) == "0" {
		pmap["metadataProfileId"] = 1
	}
	if pmap["rootFolderPath"] == nil || fmt.Sprint(pmap["rootFolderPath"]) == "" {
		rp := r.getValidRootFolderPath(ctx, opts.RootFolderPath)
		if rp == "" {
			rp = r.getValidRootFolderPath(ctx, "")
		}
		if rp == "" {
			rp = r.inst.DefaultRootFolderPath
		}
		if rp != "" {
			pmap["rootFolderPath"] = rp
		}
	}
	if _, ok := pmap["monitored"]; !ok {
		pmap["monitored"] = true
	}
	if _, ok := pmap["addOptions"]; !ok {
		pmap["addOptions"] = map[string]any{"addType": "automatic", "monitor": "all", "monitored": true, "booksToMonitor": []any{}, "searchForMissingBooks": true, "searchForNewBook": true}
	}
	if pmap["tags"] == nil && len(r.inst.DefaultTags) > 0 {
		pmap["tags"] = r.inst.DefaultTags
	}

	// Editions shape
	if _, ok := pmap["editions"]; !ok || pmap["editions"] == nil {
		pmap["editions"] = []any{}
	}
	if eds, ok := pmap["editions"].([]any); ok && len(eds) == 0 {
		if fe := strings.TrimSpace(fmt.Sprint(pmap["foreignEditionId"])); fe != "" {
			pmap["editions"] = []any{map[string]any{"foreignEditionId": fe, "monitored": true}}
		}
	}

	// Nested author enrichment
	if av, ok := pmap["author"]; ok {
		if am, ok2 := av.(map[string]any); ok2 {
			if am["rootFolderPath"] == nil || am["rootFolderPath"] == "" {
				rp := r.getValidRootFolderPath(ctx, opts.RootFolderPath)
				if rp == "" {
					rp = r.getValidRootFolderPath(ctx, "")
				}
				if rp != "" {
					am["rootFolderPath"] = rp
				}
			}
			if am["foreignAuthorId"] == nil || am["foreignAuthorId"] == "" {
				if nm, _ := am["name"].(string); nm != "" {
					if fid := r.LookupForeignAuthorIDString(ctx, nm); fid != "" {
						am["foreignAuthorId"] = fid
					} else {
						// try to import by cleaned name as fallback
						if id, err := r.ImportAuthor(ctx, strings.ReplaceAll(strings.TrimSpace(nm), " ", "-")); err == nil && id != 0 {
							am["foreignAuthorId"] = strings.ReplaceAll(strings.TrimSpace(nm), " ", "-")
						}
					}
				} else if idv, ok := am["id"]; ok {
					// No name; fetch details by id
					var aid int
					switch t := idv.(type) {
					case float64:
						aid = int(t)
					case int:
						aid = t
					case string:
						if i, e := strconv.Atoi(strings.TrimSpace(t)); e == nil {
							aid = i
						}
					}
					if aid > 0 {
						if details, err := r.GetAuthorByID(ctx, aid); err == nil && details != nil {
							if fid, _ := details["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
								am["foreignAuthorId"] = fid
							}
							if nm2, _ := details["name"].(string); nm2 != "" {
								am["name"] = nm2
							}
						}
					}
				}
			}
			pmap["author"] = am
		}
	}

	// Normalize authorId
	if av, ok := pmap["authorId"]; ok {
		remove := false
		switch v := av.(type) {
		case nil:
			remove = true
		case float64:
			pmap["authorId"] = int(v)
		case int:
		case string:
			s := strings.TrimSpace(v)
			if s == "" || strings.EqualFold(s, "null") {
				remove = true
			} else if i, e := strconv.Atoi(s); e == nil {
				pmap["authorId"] = i
			} else {
				remove = true
			}
		default:
			remove = true
		}
		if remove {
			delete(pmap, "authorId")
		}
	}

	// Inject author from authorId if missing
	if _, hasAuthor := pmap["author"]; !hasAuthor {
		if aid, ok := pmap["authorId"].(int); ok && aid > 0 {
			am := map[string]any{"id": aid}
			if details, err := r.GetAuthorByID(ctx, aid); err == nil && details != nil {
				if fid, _ := details["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
					am["foreignAuthorId"] = fid
				}
				if nm, _ := details["name"].(string); nm != "" {
					am["name"] = nm
				}
			}
			if qid := r.getValidQualityProfileID(ctx); qid != 0 {
				am["qualityProfileId"] = qid
			} else if r.inst.DefaultQualityProfileID != 0 {
				am["qualityProfileId"] = r.inst.DefaultQualityProfileID
			}
			am["metadataProfileId"] = 1
			if rp := r.getValidRootFolderPath(ctx, ""); rp != "" {
				am["rootFolderPath"] = rp
			}
			pmap["author"] = am
		}
	}

	return pmap
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
	Tags             any
}

// LookupForeignAuthorID queries Readarr author lookup endpoint and returns the foreignAuthorId as int (0 when not found)
func (r *Readarr) LookupForeignAuthorID(ctx context.Context, name string) int {
	// Use FindAuthorIDByName which queries the Readarr author lookup endpoint.
	// Legacy helper lookupForeignAuthorID was removed — keep behavior by returning
	// the found author id (0 when not found).
	id, _ := r.FindAuthorIDByName(ctx, name)
	return id
}

// LookupForeignAuthorIDString queries Readarr author lookup endpoint and returns the foreignAuthorId string (empty when not found)
func (r *Readarr) LookupForeignAuthorIDString(ctx context.Context, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	u := r.inst.BaseURL + "/api/v1/author/lookup?term=" + url.QueryEscape(name) + "&apikey=" + url.QueryEscape(r.inst.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")
	resp, err := r.cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return ""
	}
	for _, a := range arr {
		if nm, _ := a["name"].(string); nm != "" && strings.EqualFold(strings.TrimSpace(nm), name) {
			if fid, _ := a["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
				return fid
			}
		}
	}
	// fallback to first with foreignAuthorId
	for _, a := range arr {
		if fid, _ := a["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
			return fid
		}
	}
	return ""
}

// GetAuthorByID fetches an author object from Readarr and returns it as a map
func (r *Readarr) GetAuthorByID(ctx context.Context, id int) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid author id")
	}
	u := fmt.Sprintf("%s/api/v1/author/%d?apikey=%s", r.inst.BaseURL, id, url.QueryEscape(r.inst.APIKey))
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
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

// ImportAuthor attempts to create/import an author by foreignAuthorId and returns the newly created readarr id or an error.
func (r *Readarr) ImportAuthor(ctx context.Context, foreignID string) (int, error) {
	if strings.TrimSpace(foreignID) == "" {
		return 0, fmt.Errorf("empty foreign author id")
	}
	// Build a minimal create payload. This may fail if the server doesn't accept this foreign id.
	payload := map[string]any{
		"authorName":      foreignID,
		"foreignAuthorId": foreignID,
		// Use a validated root folder path when possible
		"rootFolderPath": r.getValidRootFolderPath(ctx, ""),
	}
	b, _ := json.Marshal(payload)
	u := r.inst.BaseURL + "/api/v1/author"
	if strings.Contains(u, "?") {
		u += "&apikey=" + url.QueryEscape(r.inst.APIKey)
	} else {
		u += "?apikey=" + url.QueryEscape(r.inst.APIKey)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, u, bytes.NewReader(b))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	resp, err := r.cl.Do(req)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return 0, fmt.Errorf("create author failed (HTTP %s): %s", resp.Status, string(body))
	}
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return 0, err
	}
	if idv, ok := out["id"]; ok {
		switch v := idv.(type) {
		case float64:
			return int(v), nil
		case int:
			return v, nil
		}
	}
	return 0, fmt.Errorf("author create succeeded but no id returned")
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

	// Parse, sanitize, and enrich JSON payload consistently
	var pmap map[string]any
	if err := json.Unmarshal(payload, &pmap); err == nil {
		pmap = r.sanitizeAndEnrichPayload(context.Background(), pmap, opts)
		if b, err := json.Marshal(pmap); err == nil {
			payload = b
		}
	}
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

// AddBookRaw accepts a raw JSON payload (full Readarr book schema), performs
// the same sanitization as AddBook (notably removing null authorId), and
// POSTs it to the configured AddEndpoint. Returns the sent payload and the
// Readarr response body.
func (r *Readarr) AddBookRaw(ctx context.Context, raw json.RawMessage) ([]byte, []byte, error) {
	// Sanitize authorId like AddBook
	var pmap map[string]any
	payload := raw
	if err := json.Unmarshal(raw, &pmap); err == nil {
		pmap = r.sanitizeAndEnrichPayload(context.Background(), pmap, AddOpts{})
		if b, err := json.Marshal(pmap); err == nil {
			payload = b
		}
	}

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
		return payload, respBody, fmt.Errorf("add book (raw) failed (HTTP %s) to %s: %s", resp.Status, safeURL, bodyStr)
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
	// Build the payload for author creation. Include defaults only when set.
	payload := map[string]any{
		"name": name,
		"addOptions": map[string]any{
			"searchForMissingBooks": false,
		},
	}
	// Determine a valid quality profile id to use (prefer configured value)
	if qid := r.getValidQualityProfileID(ctx); qid != 0 {
		payload["qualityProfileId"] = qid
	} else if r.inst.DefaultQualityProfileID != 0 {
		payload["qualityProfileId"] = r.inst.DefaultQualityProfileID
	}
	// keep a sane default for metadataProfileId when available
	payload["metadataProfileId"] = 1
	// Use a validated root folder path when possible
	if rp := r.getValidRootFolderPath(ctx, ""); rp != "" {
		payload["rootFolderPath"] = rp
	}
	// Also include authorName and a non-empty foreignAuthorId when possible
	payload["authorName"] = name
	if _, ok := payload["foreignAuthorId"]; !ok {
		payload["foreignAuthorId"] = strings.ReplaceAll(strings.TrimSpace(name), " ", "-")
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

		// If validation likely complains about rootFolderPath or QualityProfile, try fallback creates
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
		// If details include QualityProfile or an internal exception, try a minimal create without qualityProfileId
		if strings.Contains(lower, "quality") || strings.Contains(lower, "object reference not set") || strings.Contains(lower, "nullreferenceexception") {
			minPayload := map[string]any{
				"name":              name,
				"metadataProfileId": 1,
				"addOptions":        map[string]any{"searchForMissingBooks": false},
			}
			mpBytes, _ := json.Marshal(minPayload)
			mpURL := r.inst.BaseURL + "/api/v1/author"
			if strings.Contains(mpURL, "?") {
				mpURL += "&apikey=" + url.QueryEscape(r.inst.APIKey)
			} else {
				mpURL += "?apikey=" + url.QueryEscape(r.inst.APIKey)
			}
			mpReq, _ := http.NewRequestWithContext(ctx, http.MethodPost, mpURL, bytes.NewReader(mpBytes))
			mpReq.Header.Set("Content-Type", "application/json")
			mpReq.Header.Set("X-Api-Key", r.inst.APIKey)
			mpReq.Header.Set("User-Agent", "Scriptorum/1.0")
			mpReq.Header.Set("Accept", "application/json")
			mpResp, merr := r.cl.Do(mpReq)
			if merr == nil {
				mpBody, _ := io.ReadAll(mpResp.Body)
				mpResp.Body.Close()
				if mpResp.StatusCode < 400 {
					var created map[string]any
					if err := json.Unmarshal(mpBody, &created); err == nil {
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
					return 0, nil
				}
				// append diagnostic
				details += "; minimal_fallback_response: " + strings.TrimSpace(string(mpBody))
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

// fetchQualityProfiles queries Readarr for quality profiles and returns a map[id->name]
func (r *Readarr) fetchQualityProfiles(ctx context.Context) (map[int]string, error) {
	u := r.inst.BaseURL + "/api/v1/qualityprofile" + "?apikey=" + url.QueryEscape(r.inst.APIKey)
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
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	out := make(map[int]string)
	for _, p := range arr {
		if idv, ok := p["id"]; ok {
			var id int
			switch v := idv.(type) {
			case float64:
				id = int(v)
			case int:
				id = v
			case int64:
				id = int(v)
			case string:
				if i, err := strconv.Atoi(v); err == nil {
					id = i
				}
			}
			if id == 0 {
				continue
			}
			name, _ := p["name"].(string)
			out[id] = name
		}
	}
	return out, nil
}

// fetchRootFolders queries Readarr for root folders and returns a slice of paths
func (r *Readarr) fetchRootFolders(ctx context.Context) ([]string, error) {
	u := r.inst.BaseURL + "/api/v1/rootfolder" + "?apikey=" + url.QueryEscape(r.inst.APIKey)
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
		return nil, fmt.Errorf("HTTP %s: %s", resp.Status, string(body))
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	out := []string{}
	for _, rj := range arr {
		if p, ok := rj["path"].(string); ok && p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// getValidQualityProfileID returns a quality profile id to use: prefer configured DefaultQualityProfileID if present on server, otherwise return first available id
func (r *Readarr) getValidQualityProfileID(ctx context.Context) int {
	// prefer configured
	if r.inst.DefaultQualityProfileID != 0 {
		// verify it exists
		if qps, err := r.fetchQualityProfiles(ctx); err == nil {
			if _, ok := qps[r.inst.DefaultQualityProfileID]; ok {
				return r.inst.DefaultQualityProfileID
			}
			// fallback to first available
			for id := range qps {
				return id
			}
		}
	} else {
		if qps, err := r.fetchQualityProfiles(ctx); err == nil {
			for id := range qps {
				return id
			}
		}
	}
	return 0
}

// getValidRootFolderPath returns a root folder path to use: prefer provided override, otherwise configured DefaultRootFolderPath if it exists on server, otherwise first available
func (r *Readarr) getValidRootFolderPath(ctx context.Context, override string) string {
	if override != "" {
		// verify override exists
		if rfs, err := r.fetchRootFolders(ctx); err == nil {
			for _, p := range rfs {
				if p == override {
					return override
				}
			}
		}
	}
	if r.inst.DefaultRootFolderPath != "" {
		if rfs, err := r.fetchRootFolders(ctx); err == nil {
			for _, p := range rfs {
				if p == r.inst.DefaultRootFolderPath {
					return r.inst.DefaultRootFolderPath
				}
			}
		}
	}
	if rfs, err := r.fetchRootFolders(ctx); err == nil {
		if len(rfs) > 0 {
			return rfs[0]
		}
	}
	return ""
}

// GetQualityProfiles is an exported wrapper for fetching quality profiles.
func (r *Readarr) GetQualityProfiles(ctx context.Context) (map[int]string, error) {
	return r.fetchQualityProfiles(ctx)
}

// GetRootFolders is an exported wrapper for fetching root folders.
func (r *Readarr) GetRootFolders(ctx context.Context) ([]string, error) {
	return r.fetchRootFolders(ctx)
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

// use util.ParseAuthorNameFromTitle from util package

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
				name := util.ParseAuthorNameFromTitle(b.AuthorTitle)
				author = map[string]any{"name": name}
			}
			// Always return the candidate when the identifier test passes. The
			// author may be nil for identifier-only matches; the caller can
			// enrich the author later if needed.
			return Candidate{"title": b.Title, "titleSlug": b.TitleSlug, "author": author, "editions": b.Editions}, true
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

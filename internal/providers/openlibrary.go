package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"math/rand"
	"net/http"
	"net/url"
	"os"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

type OpenLibrary struct {
	cl      *http.Client
	baseURL string
}

// olRateLimiter is a global token-bucket rate limiter shared across all
// OpenLibrary client instances. OpenLibrary allows ~3 req/s for identified
// User-Agents; we target 2 req/s with no burst to stay safely under the limit.
var olRateLimiter = newTokenBucket(2, 1) // 2 tokens/sec, no burst

var openLibraryClientFactoryMu sync.RWMutex

func defaultOpenLibraryHTTPClient() *http.Client {
	return &http.Client{Timeout: 10 * time.Second}
}

var openLibraryHTTPClientFactory = defaultOpenLibraryHTTPClient

// TestDisableOLRateLimiter replaces the global rate limiter with a very
// fast one so that tests using mocked HTTP transport are not throttled.
// Call the returned function to restore the original limiter.
func TestDisableOLRateLimiter() func() {
	orig := olRateLimiter
	olRateLimiter = newTokenBucket(100000, 10000)
	return func() { olRateLimiter = orig }
}

// TestSetOpenLibraryHTTPClientFactory overrides the OpenLibrary HTTP client
// factory and returns a restore function for tests.
func TestSetOpenLibraryHTTPClientFactory(factory func() *http.Client) func() {
	openLibraryClientFactoryMu.Lock()
	prev := openLibraryHTTPClientFactory
	if factory == nil {
		factory = defaultOpenLibraryHTTPClient
	}
	openLibraryHTTPClientFactory = factory
	openLibraryClientFactoryMu.Unlock()
	return func() {
		openLibraryClientFactoryMu.Lock()
		openLibraryHTTPClientFactory = prev
		openLibraryClientFactoryMu.Unlock()
	}
}

func NewOpenLibrary() *OpenLibrary {
	openLibraryClientFactoryMu.RLock()
	clientFactory := openLibraryHTTPClientFactory
	openLibraryClientFactoryMu.RUnlock()
	return &OpenLibrary{
		cl:      clientFactory(),
		baseURL: "https://openlibrary.org",
	}
}

type OLDoc struct {
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	ISBN             []string `json:"isbn"` // OL returns mixed ISBN-10 and ISBN-13 values in a single field
	Language         []string `json:"language"`
	CoverId          int      `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
}
type OLResp struct {
	Docs []OLDoc `json:"docs"`
}

type OLTrendingWork struct {
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	CoverID          int      `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
}

type OLTrendingResp struct {
	Works []OLTrendingWork `json:"works"`
}

type OLSubjectAuthor struct {
	Name string `json:"name"`
}

type OLSubjectWork struct {
	Title            string            `json:"title"`
	Authors          []OLSubjectAuthor `json:"authors"`
	CoverID          int               `json:"cover_id"`
	CoverEditionKey  string            `json:"cover_edition_key"`
	Key              string            `json:"key"`
	FirstPublishYear int               `json:"first_publish_year"`
}

type OLSubjectResp struct {
	Works []OLSubjectWork `json:"works"`
}

type openLibraryTextValue struct {
	Value string `json:"value"`
}

type OLWorkDetailsResp struct {
	Key              string          `json:"key"`
	Title            string          `json:"title"`
	Description      json.RawMessage `json:"description"`
	Subjects         []string        `json:"subjects"`
	Covers           []int           `json:"covers"`
	FirstPublishDate string          `json:"first_publish_date"`
}

type OpenLibraryWorkDetails struct {
	Key              string
	Title            string
	Description      string
	Subjects         []string
	FirstPublishDate string
	CoverMedium      string
}

type BookItem struct {
	ASIN                  string
	Title                 string
	Authors               []string
	ISBN10                string
	ISBN13                string
	FirstPublishYear      int
	Description           string
	OpenLibraryWorkKey    string
	OpenLibraryEditionKey string
	CoverSmall            string
	CoverMedium           string
}

func (ol *OpenLibrary) Search(ctx context.Context, q string, limit, page int) ([]BookItem, error) {
	return ol.SearchWithLanguages(ctx, q, limit, page, nil)
}

func (ol *OpenLibrary) SearchWithLanguages(ctx context.Context, q string, limit, page int, languageCodes []string) ([]BookItem, error) {
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if page <= 0 {
		page = 1
	}
	u := ol.apiURL("/search.json?q=" + url.QueryEscape(q) + "&limit=" + strconv.Itoa(limit))
	if page > 1 {
		u += "&page=" + strconv.Itoa(page)
	}
	languageCodes = normalizeOpenLibraryLanguageCodes(languageCodes)
	if len(languageCodes) == 0 {
		var out OLResp
		if err := ol.getJSON(ctx, u, "search", &out); err != nil {
			return nil, err
		}
		return olDocsToBookItems(out.Docs, nil), nil
	}

	// OR semantics across selected languages: query each language separately
	// and merge unique results. Sending repeated language params in one request
	// may be interpreted as overly strict by upstream search.
	merged := make([]BookItem, 0, limit*len(languageCodes))
	seen := make(map[string]struct{}, limit*len(languageCodes))
	for _, code := range languageCodes {
		var out OLResp
		lu := u + "&language=" + url.QueryEscape(code)
		if err := ol.getJSON(ctx, lu, "search", &out); err != nil {
			return nil, err
		}
		for _, item := range olDocsToBookItems(out.Docs, languageCodes) {
			key := strings.TrimSpace(item.OpenLibraryWorkKey)
			if key == "" {
				if len(item.Authors) > 0 {
					key = strings.ToLower(strings.TrimSpace(item.Title)) + "::" + strings.ToLower(strings.TrimSpace(item.Authors[0]))
				} else {
					key = strings.ToLower(strings.TrimSpace(item.Title))
				}
			}
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			merged = append(merged, item)
		}
	}
	return merged, nil
}

func olDocsToBookItems(docs []OLDoc, languageCodes []string) []BookItem {
	items := make([]BookItem, 0, len(docs))
	for _, d := range docs {
		if !openLibraryDocLanguageAllowed(d.Language, languageCodes) {
			continue
		}
		// OL search returns all ISBNs in a single "isbn" field; split by length
		i10, i13 := splitISBNs(d.ISBN)
		cover := openLibraryCoverURL(d.CoverId, d.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 d.Title,
			Authors:               d.AuthorName,
			ISBN10:                i10,
			ISBN13:                i13,
			FirstPublishYear:      d.FirstPublishYear,
			OpenLibraryWorkKey:    d.Key,
			OpenLibraryEditionKey: d.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
	}
	return items
}

func normalizeOpenLibraryLanguageCodes(languageCodes []string) []string {
	if len(languageCodes) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(languageCodes))
	out := make([]string, 0, len(languageCodes))
	for _, raw := range languageCodes {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if _, ok := seen[code]; ok {
			continue
		}
		seen[code] = struct{}{}
		out = append(out, code)
	}
	sort.Strings(out)
	if len(out) == 0 {
		return nil
	}
	return out
}

func openLibraryDocLanguageAllowed(docLanguages, allowed []string) bool {
	if len(allowed) == 0 {
		return true
	}
	if len(docLanguages) == 0 {
		// Keep items with unknown metadata instead of dropping discovery quality too aggressively.
		return true
	}
	allowedSet := make(map[string]struct{}, len(allowed))
	for _, code := range allowed {
		allowedSet[strings.ToLower(strings.TrimSpace(code))] = struct{}{}
	}
	for _, raw := range docLanguages {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		if _, ok := allowedSet[code]; ok {
			return true
		}
	}
	return false
}

func (ol *OpenLibrary) TrendingWorks(ctx context.Context, period string, limit int) ([]BookItem, error) {
	period = strings.ToLower(strings.TrimSpace(period))
	switch period {
	case "daily", "weekly", "monthly", "yearly", "forever":
	default:
		period = "weekly"
	}
	if limit <= 0 {
		limit = 10
	}
	u := ol.apiURL("/trending/" + url.PathEscape(period) + ".json?limit=" + strconv.Itoa(limit))
	var out OLTrendingResp
	if err := ol.getJSON(ctx, u, "trending", &out); err != nil {
		return nil, err
	}
	items := make([]BookItem, 0, len(out.Works))
	for _, w := range out.Works {
		cover := openLibraryCoverURL(w.CoverID, w.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 w.Title,
			Authors:               w.AuthorName,
			FirstPublishYear:      w.FirstPublishYear,
			OpenLibraryWorkKey:    w.Key,
			OpenLibraryEditionKey: w.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
	}
	return items, nil
}

func (ol *OpenLibrary) SubjectWorks(ctx context.Context, subject string, limit int) ([]BookItem, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	u := ol.apiURL("/subjects/" + url.PathEscape(subject) + ".json?limit=" + strconv.Itoa(limit))
	var out OLSubjectResp
	if err := ol.getJSON(ctx, u, "subject", &out); err != nil {
		return nil, err
	}
	items := make([]BookItem, 0, len(out.Works))
	for _, w := range out.Works {
		authors := make([]string, 0, len(w.Authors))
		for _, author := range w.Authors {
			if strings.TrimSpace(author.Name) != "" {
				authors = append(authors, author.Name)
			}
		}
		cover := openLibraryCoverURL(w.CoverID, w.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 w.Title,
			Authors:               authors,
			FirstPublishYear:      w.FirstPublishYear,
			OpenLibraryWorkKey:    w.Key,
			OpenLibraryEditionKey: w.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
	}
	return items, nil
}

func (ol *OpenLibrary) WorkDetails(ctx context.Context, workKey string) (*OpenLibraryWorkDetails, error) {
	workKey = strings.TrimSpace(workKey)
	if workKey == "" {
		return nil, nil
	}
	if !strings.HasPrefix(workKey, "/") {
		workKey = "/" + workKey
	}
	u := ol.apiURL(workKey + ".json")
	var out OLWorkDetailsResp
	if err := ol.getJSON(ctx, u, "work details", &out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Key) == "" && strings.TrimSpace(out.Title) == "" && len(out.Subjects) == 0 && len(out.Covers) == 0 && len(out.Description) == 0 {
		return nil, nil
	}
	details := &OpenLibraryWorkDetails{
		Key:              out.Key,
		Title:            out.Title,
		Description:      parseOpenLibraryText(out.Description),
		Subjects:         out.Subjects,
		FirstPublishDate: out.FirstPublishDate,
	}
	if len(out.Covers) > 0 {
		details.CoverMedium = openLibraryCoverURL(out.Covers[0], "")
	}
	return details, nil
}

func openLibraryCoverURL(coverID int, coverEditionKey string) string {
	if coverID != 0 {
		return fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", coverID)
	}
	normalizedEditionKey := normalizeOpenLibraryEditionKey(coverEditionKey)
	if normalizedEditionKey != "" {
		return "https://covers.openlibrary.org/b/olid/" + url.PathEscape(normalizedEditionKey) + "-M.jpg"
	}
	return ""
}

func normalizeOpenLibraryEditionKey(coverEditionKey string) string {
	key := strings.TrimSpace(coverEditionKey)
	if key == "" {
		return ""
	}
	key = strings.TrimPrefix(key, "/")
	key = strings.TrimPrefix(key, "books/")
	if slash := strings.IndexRune(key, '/'); slash >= 0 {
		key = key[:slash]
	}
	return strings.TrimSpace(key)
}

func parseOpenLibraryText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var wrapped openLibraryTextValue
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		return strings.TrimSpace(wrapped.Value)
	}
	return ""
}

// splitISBNs separates a mixed list of ISBNs (as returned by OpenLibrary's
// search API "isbn" field) into the first ISBN-10 and first ISBN-13 found.
func splitISBNs(isbns []string) (isbn10, isbn13 string) {
	for _, raw := range isbns {
		v := strings.TrimSpace(raw)
		if v == "" {
			continue
		}
		switch len(v) {
		case 10:
			if isbn10 == "" {
				isbn10 = v
			}
		case 13:
			if isbn13 == "" {
				isbn13 = v
			}
		}
		if isbn10 != "" && isbn13 != "" {
			break
		}
	}
	return
}

func (ol *OpenLibrary) apiURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(ol.baseURL), "/")
	if base == "" {
		base = "https://openlibrary.org"
	}
	return base + path
}

// olMaxRetries is the maximum number of retries on 429 Too Many Requests.
const olMaxRetries = 3

func (ol *OpenLibrary) getJSON(ctx context.Context, endpointURL, endpointName string, out any) error {
	for attempt := 0; attempt <= olMaxRetries; attempt++ {
		// Wait for a rate-limit token before sending the request.
		if err := olRateLimiter.Wait(ctx); err != nil {
			return fmt.Errorf("openlibrary %s rate-limit wait: %w", endpointName, err)
		}

		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpointURL, nil)
		req.Header.Set("User-Agent", openLibraryUserAgent())
		req.Header.Set("Accept", "application/json")

		resp, err := ol.cl.Do(req)
		if err != nil {
			return err
		}

		// On 429 Too Many Requests, back off with exponential delay + jitter.
		if resp.StatusCode == http.StatusTooManyRequests {
			resp.Body.Close()
			if attempt == olMaxRetries {
				return fmt.Errorf("openlibrary %s HTTP 429 Too Many Requests (after %d retries)", endpointName, olMaxRetries)
			}
			backoff := olBackoff(attempt)
			select {
			case <-time.After(backoff):
				continue
			case <-ctx.Done():
				return ctx.Err()
			}
		}

		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			body, _ := io.ReadAll(resp.Body)
			msg := strings.TrimSpace(string(body))
			if len(msg) > 200 {
				msg = msg[:200] + "..."
			}
			return fmt.Errorf("openlibrary %s HTTP %s: %s", endpointName, resp.Status, msg)
		}

		if err := json.NewDecoder(resp.Body).Decode(out); err != nil {
			return err
		}
		return nil
	}
	return fmt.Errorf("openlibrary %s: exhausted retries", endpointName)
}

// olBackoff returns the backoff duration for the given retry attempt using
// exponential backoff with jitter: base * 2^attempt + random(0, base).
func olBackoff(attempt int) time.Duration {
	base := 2 * time.Second
	delay := base * time.Duration(1<<uint(attempt)) // 2s, 4s, 8s
	jitter := time.Duration(rand.Int63n(int64(base)))
	return delay + jitter
}

func openLibraryUserAgent() string {
	if ua := strings.TrimSpace(os.Getenv("OPENLIBRARY_USER_AGENT")); ua != "" {
		return ua
	}
	// OL grants higher rate limits (3 req/s) to User-Agents with contact info.
	// Format: "AppName/Version (mailto:contact@domain; +homepage)"
	if email := strings.TrimSpace(os.Getenv("OPENLIBRARY_CONTACT_EMAIL")); email != "" {
		return "Scriptorum/1.0 (mailto:" + email + "; +https://openlibrary.org)"
	}
	return "Scriptorum/1.0 (+https://openlibrary.org)"
}

// tokenBucket implements a simple token-bucket rate limiter that is safe
// for concurrent use. Tokens refill at a fixed rate up to a maximum burst.
type tokenBucket struct {
	mu       sync.Mutex
	tokens   float64
	max      float64
	rate     float64 // tokens per second
	lastFill time.Time
}

func newTokenBucket(ratePerSec, burst float64) *tokenBucket {
	return &tokenBucket{
		tokens:   burst,
		max:      burst,
		rate:     ratePerSec,
		lastFill: time.Now(),
	}
}

// Wait blocks until a token is available or ctx is cancelled.
func (tb *tokenBucket) Wait(ctx context.Context) error {
	for {
		tb.mu.Lock()
		now := time.Now()
		elapsed := now.Sub(tb.lastFill).Seconds()
		tb.tokens += elapsed * tb.rate
		if tb.tokens > tb.max {
			tb.tokens = tb.max
		}
		tb.lastFill = now

		if tb.tokens >= 1.0 {
			tb.tokens -= 1.0
			tb.mu.Unlock()
			return nil
		}

		// Calculate how long until the next token is available.
		wait := time.Duration((1.0-tb.tokens)/tb.rate*1000) * time.Millisecond
		tb.mu.Unlock()

		select {
		case <-time.After(wait):
			// Try again after waiting.
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}

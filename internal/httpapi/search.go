package httpapi

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"gitea.knapp/jacoknapp/scriptorum/internal/util"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSearch(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON":    func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"urlquery":  url.QueryEscape,
		"csrfToken": func(r *http.Request) string { return s.getCSRFToken(r) },
	}
	u := &searchUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Get("/ui/search", u.handleSearch(s))
	// Readarr cover proxy (fetch fresh each call). Search UI will link images here
	r.Get("/ui/readarr-cover", s.requireLogin(s.serveReadarrCover()))
}

type searchUI struct{ tpl *template.Template }

type searchItem struct {
	providers.BookItem
	Provider                 string
	ProviderPayload          string
	ProviderEbookPayload     string
	ProviderAudiobookPayload string
	DetailsPayload           string
	DiscoveryLabel           string
	EbookState               string
	AudiobookState           string
}

type discoveryCategory struct {
	Name        string
	Description string
	Items       []searchItem
}

type discoveryQuery struct {
	Name        string
	Description string
	Queries     []string
	MinYear     int
}

const (
	discoveryCategorySize = 8
	discoveryTrendingSize = 8
	discoveryCacheTTL     = 15 * time.Minute
)

var defaultDiscoveryQueries = []discoveryQuery{
	{
		Name:        "Fantasy Hits",
		Description: "Romantasy, dragons, and high-stakes series readers are tearing through right now.",
		Queries:     []string{"romantasy", "dragon fantasy", "epic fantasy bestseller", "fantasy 2024"},
		MinYear:     2018,
	},
	{
		Name:        "Thriller Buzz",
		Description: "Fast, twisty page-turners with recent momentum and bingeable energy.",
		Queries:     []string{"psychological thriller", "freida mcfadden", "thriller bestseller", "domestic thriller"},
		MinYear:     2020,
	},
	{
		Name:        "Rom-Com Favorites",
		Description: "Smart contemporary romance picks with banter, chemistry, and recent release heat.",
		Queries:     []string{"emily henry", "ali hazelwood", "contemporary romance bestseller", "rom com 2024"},
		MinYear:     2020,
	},
	{
		Name:        "Sci-Fi Series Hits",
		Description: "Big-concept modern science fiction with sequel energy and strong fan followings.",
		Queries:     []string{"murderbot", "space opera", "science fiction bestseller", "sci fi 2024"},
		MinYear:     2017,
	},
}

func (u *searchUI) handleSearch(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		page := 1
		if p := strings.TrimSpace(r.URL.Query().Get("page")); p != "" {
			if n, err := strconv.Atoi(p); err == nil && n > 0 {
				page = n
			}
		}
		limit := 20
		if l := strings.TrimSpace(r.URL.Query().Get("limit")); l != "" {
			if n, err := strconv.Atoi(l); err == nil && n > 0 && n <= 50 {
				limit = n
			}
		}
		if q == "" {
			data := s.cachedDiscoverySearchData(r.Context(), u)
			w.Header().Set("Content-Type", "text/html; charset=utf-8")
			_ = u.tpl.ExecuteTemplate(w, "search_partial.html", data)
			return
		}

		items := []searchItem{}
		// Index by dedupe key to merge ebook/audiobook payloads for the same work
		idx := map[string]int{}

		// Helper to upsert item by key and attach payloads
		upsert := func(si searchItem, ebook bool, payload string) {
			k := dedupeKey(si.BookItem)
			if k == "" {
				return
			}
			if i, ok := idx[k]; ok {
				// attach payload
				if ebook {
					if payload != "" {
						items[i].ProviderEbookPayload = payload
					}
				} else {
					if payload != "" {
						items[i].ProviderAudiobookPayload = payload
					}
				}
				if items[i].DetailsPayload == "" && si.DetailsPayload != "" {
					items[i].DetailsPayload = si.DetailsPayload
				}
				// do not record or surface which instance produced the result
				// Prefer cover from provider payload when available. Use helper
				// to decide whether to overwrite.
				items[i].BookItem.CoverMedium = mergeCover(items[i].BookItem.CoverMedium, si.BookItem.CoverMedium)
				items[i].BookItem.CoverSmall = mergeCover(items[i].BookItem.CoverSmall, si.BookItem.CoverSmall)
				return
			}
			if ebook {
				si.ProviderEbookPayload = payload
			} else {
				si.ProviderAudiobookPayload = payload
			}
			// Do not set Provider label so UI won't display source instance
			si.Provider = ""
			idx[k] = len(items)
			items = append(items, si)
		}

		asin := providers.ExtractASINFromInput(q)
		cfg := s.settings.Get()
		// Build instances
		var instE, instA providers.ReadarrInstance
		if cfg != nil {
			if strings.TrimSpace(cfg.Readarr.Ebooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Ebooks.APIKey) != "" {
				instE = providers.ReadarrInstance{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKey: cfg.Readarr.Ebooks.APIKey, DefaultQualityProfileID: cfg.Readarr.Ebooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Ebooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Ebooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Ebooks.InsecureSkipVerify}
			}
			if strings.TrimSpace(cfg.Readarr.Audiobooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Audiobooks.APIKey) != "" {
				instA = providers.ReadarrInstance{BaseURL: cfg.Readarr.Audiobooks.BaseURL, APIKey: cfg.Readarr.Audiobooks.APIKey, DefaultQualityProfileID: cfg.Readarr.Audiobooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Audiobooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Audiobooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Audiobooks.InsecureSkipVerify}
			}
		}

		// Query Readarr ebooks
		if strings.TrimSpace(instE.BaseURL) != "" && strings.TrimSpace(instE.APIKey) != "" && (asin != "" || q != "") {
			ra := providers.NewReadarrWithDB(instE, s.db.SQL())
			term := q
			if asin != "" {
				term = asin
			}
			if list, err := ra.LookupByTerm(r.Context(), term); err == nil {
				for _, b := range list {
					var author map[string]any
					if b.Author != nil {
						author = b.Author
					} else if len(b.Authors) > 0 {
						author = b.Authors[0]
					} else if b.AuthorId > 0 {
						author = map[string]any{"id": b.AuthorId}
					} else if b.AuthorTitle != "" {
						author = map[string]any{"name": parseAuthorNameFromTitle(b.AuthorTitle)}
					}
					// Build canonical Readarr Book schema candidate. Include a single monitored edition to pin to this foreignEditionId.
					cand := map[string]any{
						"title":            b.Title,
						"titleSlug":        b.TitleSlug,
						"author":           author,
						"editions":         []any{map[string]any{"foreignEditionId": b.ForeignEditionId, "monitored": true}},
						"foreignBookId":    b.ForeignBookId,
						"foreignEditionId": b.ForeignEditionId,
						// provider will backfill these defaults if missing
						"monitored":         true,
						"metadataProfileId": 1,
					}
					cjson, _ := json.Marshal(cand)
					dispAuthor := ""
					if author != nil {
						if n, _ := author["name"].(string); n != "" {
							dispAuthor = n
						}
					}
					var authors []string
					if dispAuthor != "" {
						authors = []string{dispAuthor}
					}
					// Derive cover URL if available. Prefer remote/absolute URLs so
					// the browser can reliably fetch images. If Readarr returned a
					// proxy-relative path (e.g. /MediaCover/...), convert it to an
					// absolute URL using the instance BaseURL.
					cover := util.FirstNonEmpty(b.RemoteCover, b.RemotePoster, b.CoverUrl)
					if cover == "" && len(b.Images) > 0 {
						for _, im := range b.Images {
							if strings.EqualFold(im.CoverType, "cover") || strings.EqualFold(im.CoverType, "poster") {
								// prefer remoteUrl when available
								cover = util.FirstNonEmpty(im.RemoteUrl, im.Url)
								if cover != "" {
									break
								}
							}
						}
					}
					// If the cover is a proxy-relative path, make it absolute
					if strings.HasPrefix(cover, "/") && strings.TrimSpace(instE.BaseURL) != "" {
						cover = strings.TrimRight(instE.BaseURL, "/") + cover
					}
					// If this cover comes from the configured Readarr instance, route
					// it through our local proxy so we can cache/stabilize the image URL.
					if cover != "" && strings.HasPrefix(strings.TrimSpace(instE.BaseURL), "http") {
						if u, err := url.Parse(cover); err == nil {
							if strings.EqualFold(u.Host, urlHost(instE.BaseURL)) {
								cover = "/ui/readarr-cover?u=" + url.QueryEscape(cover)
							}
						}
					}
					upsert(searchItem{BookItem: providers.BookItem{Title: b.Title, Authors: authors, CoverSmall: cover, CoverMedium: cover}, Provider: "readarr-ebook"}, true, string(cjson))
				}
			}
		}
		// Query Readarr audiobooks
		if strings.TrimSpace(instA.BaseURL) != "" && strings.TrimSpace(instA.APIKey) != "" && (asin != "" || q != "") {
			ra := providers.NewReadarrWithDB(instA, s.db.SQL())
			term := q
			if asin != "" {
				term = asin
			}
			if list, err := ra.LookupByTerm(r.Context(), term); err == nil {
				for _, b := range list {
					var author map[string]any
					if b.Author != nil {
						author = b.Author
					} else if len(b.Authors) > 0 {
						author = b.Authors[0]
					} else if b.AuthorId > 0 {
						author = map[string]any{"id": b.AuthorId}
					} else if b.AuthorTitle != "" {
						author = map[string]any{"name": parseAuthorNameFromTitle(b.AuthorTitle)}
					}
					// Build canonical Readarr Book schema candidate for audiobooks
					cand := map[string]any{
						"title":             b.Title,
						"titleSlug":         b.TitleSlug,
						"author":            author,
						"editions":          []any{map[string]any{"foreignEditionId": b.ForeignEditionId, "monitored": true}},
						"foreignBookId":     b.ForeignBookId,
						"foreignEditionId":  b.ForeignEditionId,
						"monitored":         true,
						"metadataProfileId": 1,
					}
					cjson, _ := json.Marshal(cand)
					dispAuthor := ""
					if author != nil {
						if n, _ := author["name"].(string); n != "" {
							dispAuthor = n
						}
					}
					var authors []string
					if dispAuthor != "" {
						authors = []string{dispAuthor}
					}
					// Derive cover URL if available. Prefer remote/absolute URLs so
					// the browser can reliably fetch images. If Readarr returned a
					// proxy-relative path (e.g. /MediaCover/...), convert it to an
					// absolute URL using the instance BaseURL.
					cover := util.FirstNonEmpty(b.RemoteCover, b.RemotePoster, b.CoverUrl)
					if cover == "" && len(b.Images) > 0 {
						for _, im := range b.Images {
							if strings.EqualFold(im.CoverType, "cover") || strings.EqualFold(im.CoverType, "poster") {
								// prefer remoteUrl when available
								cover = util.FirstNonEmpty(im.RemoteUrl, im.Url)
								if cover != "" {
									break
								}
							}
						}
					}
					// If the cover is a proxy-relative path, make it absolute
					if strings.HasPrefix(cover, "/") && strings.TrimSpace(instA.BaseURL) != "" {
						cover = strings.TrimRight(instA.BaseURL, "/") + cover
					}
					// If this cover comes from the configured Readarr instance, route
					// it through our local proxy so we can cache/stabilize the image URL.
					if cover != "" && strings.HasPrefix(strings.TrimSpace(instA.BaseURL), "http") {
						if u, err := url.Parse(cover); err == nil {
							if strings.EqualFold(u.Host, urlHost(instA.BaseURL)) {
								cover = "/ui/readarr-cover?u=" + url.QueryEscape(cover)
							}
						}
					}
					upsert(searchItem{BookItem: providers.BookItem{Title: b.Title, Authors: authors, CoverSmall: cover, CoverMedium: cover}, Provider: "readarr-audiobook"}, false, string(cjson))
				}
			}
		}

		// If neither provider is configured, fallback to public sources
		if len(items) == 0 {
			// Fallback to public sources if provider not configured
			ap := providers.NewAmazonPublic("www.amazon.com")
			if asin != "" {
				if book, err := ap.GetByASIN(r.Context(), asin); err == nil && book != nil {
					items = append(items, searchItem{BookItem: providers.BookItem{ASIN: book.ASIN, Title: book.Title, Authors: book.Authors, ISBN10: book.ISBN10, ISBN13: book.ISBN13, CoverSmall: book.Image, CoverMedium: book.Image}})
				}
			} else if q != "" {
				if pubItems, err := ap.SearchBooks(r.Context(), q, page, limit); err == nil {
					for _, b := range pubItems {
						items = append(items, searchItem{BookItem: providers.BookItem{ASIN: b.ASIN, Title: b.Title, Authors: b.Authors, CoverSmall: b.Image, CoverMedium: b.Image}})
					}
				}
			}

			if q != "" {
				ol := providers.NewOpenLibrary()
				if olItems, err := ol.Search(r.Context(), q, limit, page); err == nil {
					for _, b := range olItems {
						items = append(items, openLibrarySearchItem(b, ""))
					}
				}
			}
		}

		data := map[string]any{"Query": q, "Items": items}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		decorateSearchItems(s, items)
		_ = u.tpl.ExecuteTemplate(w, "search_partial.html", data)
	}
}

func buildDiscoverySearchData(ctx context.Context, s *Server, u *searchUI) map[string]any {
	var trending []searchItem
	var categories []discoveryCategory
	var wg sync.WaitGroup
	wg.Add(2)
	go func() {
		defer wg.Done()
		trending = u.loadTrendingBooks(ctx)
	}()
	go func() {
		defer wg.Done()
		categories = u.loadDiscoveryCategories(ctx)
	}()
	wg.Wait()

	decorateSearchItems(s, trending)
	for i := range categories {
		decorateSearchItems(s, categories[i].Items)
	}
	return map[string]any{
		"IsDiscovery":         true,
		"TrendingNow":         trending,
		"DiscoveryCategories": categories,
	}
}

func (s *Server) cachedDiscoverySearchData(ctx context.Context, u *searchUI) map[string]any {
	if cached := s.loadDiscoverySearchData(); cached != nil {
		return cached
	}

	s.discoveryCacheMu.Lock()
	defer s.discoveryCacheMu.Unlock()

	if s.discoveryCache != nil && time.Since(time.Unix(s.discoveryCacheAt, 0)) < discoveryCacheTTL {
		return s.discoveryCache
	}

	fresh := buildDiscoverySearchData(ctx, s, u)
	s.discoveryCache = fresh
	s.discoveryCacheAt = time.Now().Unix()
	return fresh
}

func (s *Server) loadDiscoverySearchData() map[string]any {
	s.discoveryCacheMu.RLock()
	defer s.discoveryCacheMu.RUnlock()

	if s.discoveryCache == nil {
		return nil
	}
	if time.Since(time.Unix(s.discoveryCacheAt, 0)) >= discoveryCacheTTL {
		return nil
	}
	return s.discoveryCache
}

func (u *searchUI) loadTrendingBooks(ctx context.Context) []searchItem {
	ol := providers.NewOpenLibrary()
	books, err := ol.TrendingWorks(ctx, "weekly", 24)
	if err != nil || len(books) == 0 {
		return nil
	}
	books = pickDiscoveryBooks(books, 2010, discoveryTrendingSize)
	items := make([]searchItem, 0, len(books))
	for _, book := range books {
		items = append(items, openLibrarySearchItem(book, "Trending pick"))
	}
	return items
}

func (u *searchUI) loadDiscoveryCategories(ctx context.Context) []discoveryCategory {
	ol := providers.NewOpenLibrary()
	results := make([]discoveryCategory, len(defaultDiscoveryQueries))
	var wg sync.WaitGroup
	wg.Add(len(defaultDiscoveryQueries))
	for i, query := range defaultDiscoveryQueries {
		go func(i int, query discoveryQuery) {
			defer wg.Done()
			books := gatherDiscoveryCategoryBooks(ctx, ol, query)
			if len(books) == 0 {
				return
			}
			items := make([]searchItem, 0, len(books))
			for _, book := range books {
				items = append(items, openLibrarySearchItem(book, "Shelf pick"))
			}
			results[i] = discoveryCategory{
				Name:        query.Name,
				Description: query.Description,
				Items:       items,
			}
		}(i, query)
	}
	wg.Wait()

	categories := make([]discoveryCategory, 0, len(results))
	for _, category := range results {
		if len(category.Items) == 0 {
			continue
		}
		categories = append(categories, category)
	}
	return categories
}

func openLibrarySearchItem(book providers.BookItem, discoveryLabel string) searchItem {
	return searchItem{
		BookItem:       book,
		DetailsPayload: buildOpenLibraryDetailsPayload(book),
		DiscoveryLabel: discoveryLabel,
	}
}

func buildOpenLibraryDetailsPayload(book providers.BookItem) string {
	payload := map[string]any{}
	if title := strings.TrimSpace(book.Title); title != "" {
		payload["title"] = title
	}
	if len(book.Authors) > 0 {
		payload["authors"] = book.Authors
	}
	if isbn10 := strings.TrimSpace(book.ISBN10); isbn10 != "" {
		payload["isbn10"] = isbn10
	}
	if isbn13 := strings.TrimSpace(book.ISBN13); isbn13 != "" {
		payload["isbn13"] = isbn13
	}
	if asin := strings.TrimSpace(book.ASIN); asin != "" {
		payload["asin"] = asin
	}
	if desc := strings.TrimSpace(book.Description); desc != "" {
		payload["description"] = desc
	}
	if workKey := strings.TrimSpace(book.OpenLibraryWorkKey); workKey != "" {
		payload["open_library_work_key"] = workKey
	}
	if editionKey := strings.TrimSpace(book.OpenLibraryEditionKey); editionKey != "" {
		payload["open_library_edition_key"] = editionKey
	}
	if year := book.FirstPublishYear; year > 0 {
		payload["first_publish_year"] = year
	}
	if cover := util.FirstNonEmpty(strings.TrimSpace(book.CoverMedium), strings.TrimSpace(book.CoverSmall)); cover != "" {
		payload["cover"] = cover
	}
	if len(payload) == 0 {
		return ""
	}
	raw, _ := json.Marshal(payload)
	return string(raw)
}

func gatherDiscoveryCategoryBooks(ctx context.Context, ol *providers.OpenLibrary, query discoveryQuery) []providers.BookItem {
	candidates := make([]providers.BookItem, 0, discoveryCategorySize*3)
	seen := make(map[string]struct{}, discoveryCategorySize*3)
	appendCandidates := func(books []providers.BookItem) {
		for _, book := range books {
			key := discoveryBookKey(book)
			if key == "" {
				continue
			}
			if _, ok := seen[key]; ok {
				continue
			}
			seen[key] = struct{}{}
			candidates = append(candidates, book)
		}
	}

	for _, term := range query.Queries {
		for page := 1; page <= 2; page++ {
			books, err := ol.Search(ctx, term, 18, page)
			if err != nil || len(books) == 0 {
				break
			}
			appendCandidates(books)
			if selected := pickDiscoveryBooks(candidates, query.MinYear, discoveryCategorySize); len(selected) >= discoveryCategorySize {
				return selected
			}
		}
	}

	return pickDiscoveryBooks(candidates, query.MinYear, discoveryCategorySize)
}

func pickDiscoveryBooks(books []providers.BookItem, minYear, limit int) []providers.BookItem {
	if limit <= 0 {
		limit = discoveryCategorySize
	}
	selected := make([]providers.BookItem, 0, limit)
	seen := make(map[string]struct{}, limit)
	appendIfEligible := func(book providers.BookItem) bool {
		key := discoveryBookKey(book)
		if key == "" {
			return false
		}
		if _, ok := seen[key]; ok {
			return false
		}
		if !isDiscoveryCandidate(book) {
			return false
		}
		seen[key] = struct{}{}
		selected = append(selected, book)
		return len(selected) >= limit
	}

	for _, book := range books {
		if book.FirstPublishYear >= minYear && appendIfEligible(book) {
			return selected
		}
	}
	for _, book := range books {
		if appendIfEligible(book) {
			return selected
		}
	}
	return selected
}

func discoveryBookKey(book providers.BookItem) string {
	title := strings.ToLower(strings.TrimSpace(book.Title))
	if title == "" {
		return ""
	}
	if len(book.Authors) > 0 {
		author := strings.ToLower(strings.TrimSpace(book.Authors[0]))
		if author != "" {
			return title + "::" + author
		}
	}
	return title
}

func isDiscoveryCandidate(book providers.BookItem) bool {
	title := strings.ToLower(strings.TrimSpace(book.Title))
	if title == "" {
		return false
	}
	blockedSnippets := []string{
		"summary",
		"study guide",
		"biography",
		"workbook",
		"collection set",
		"box set",
		"journal",
		"review",
		"analysis",
	}
	for _, snippet := range blockedSnippets {
		if strings.Contains(title, snippet) {
			return false
		}
	}
	return true
}

func decorateSearchItems(s *Server, items []searchItem) {
	stateCache := make(map[string]string, len(items)*2)
	for i := range items {
		if items[i].ProviderPayload == "" {
			items[i].ProviderPayload = mergeProviderPayloads(items[i].ProviderEbookPayload, items[i].ProviderAudiobookPayload)
		}
		items[i].EbookState = cachedCatalogState(stateCache, s, "ebook", items[i].Title, items[i].Authors, items[i].ISBN10, items[i].ISBN13, items[i].ASIN, items[i].ProviderEbookPayload)
		items[i].AudiobookState = cachedCatalogState(stateCache, s, "audiobook", items[i].Title, items[i].Authors, items[i].ISBN10, items[i].ISBN13, items[i].ASIN, items[i].ProviderAudiobookPayload)
	}
}

func cachedCatalogState(stateCache map[string]string, s *Server, kind, title string, authors []string, isbn10, isbn13, asin, payload string) string {
	query := buildCatalogMatchQuery(kind, title, authors, isbn10, isbn13, asin, []byte(payload))
	key := "state|" + catalogMatchCacheKey(query)
	if state, ok := stateCache[key]; ok {
		return state
	}
	state := s.loadCatalogState(kind, title, authors, isbn10, isbn13, asin, payload)
	stateCache[key] = state
	return state
}

// serveReadarrCover returns a handler that fetches a remote image and streams
// it directly to the client on every request (no on-disk caching). Query
// param: u=<image-absolute-url>
func (s *Server) serveReadarrCover() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		remote := strings.TrimSpace(r.URL.Query().Get("u"))
		if remote == "" {
			http.Error(w, "missing url", http.StatusBadRequest)
			return
		}
		// validate URL
		ru, err := url.Parse(remote)
		if err != nil || !(ru.Scheme == "http" || ru.Scheme == "https") {
			http.Error(w, "invalid url", http.StatusBadRequest)
			return
		}
		// Restrict this proxy endpoint to configured Readarr hosts only to
		// avoid acting as an open proxy. Also determine whether we should
		// skip TLS verification for that configured host.
		cfg := s.settings.Get()
		if cfg == nil {
			http.Error(w, "cover proxy disabled", http.StatusForbidden)
			return
		}
		ebookHost := urlHost(cfg.Readarr.Ebooks.BaseURL)
		audioHost := urlHost(cfg.Readarr.Audiobooks.BaseURL)

		// Only allow fetching from one of the configured Readarr hosts
		if !strings.EqualFold(ru.Host, ebookHost) && !strings.EqualFold(ru.Host, audioHost) {
			http.Error(w, "host not permitted", http.StatusForbidden)
			return
		}

		// Build HTTP client; if this URL targets a configured Readarr host that has
		// InsecureSkipVerify enabled, use a client that skips TLS verification.
		client := &http.Client{Timeout: 12 * time.Second}
		wantsInsecure := false
		if strings.EqualFold(ru.Host, ebookHost) && cfg.Readarr.Ebooks.InsecureSkipVerify {
			wantsInsecure = true
		}
		if strings.EqualFold(ru.Host, audioHost) && cfg.Readarr.Audiobooks.InsecureSkipVerify {
			wantsInsecure = true
		}
		if wantsInsecure && ru.Scheme == "https" {
			tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
			client = &http.Client{Timeout: 12 * time.Second, Transport: tr}
		}
		// fetch remote fresh on every call
		resp, err := client.Get(remote)
		if err != nil {
			http.Error(w, "fetch error", http.StatusBadGateway)
			return
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			http.Error(w, "remote not ok", http.StatusBadGateway)
			return
		}
		// Copy useful metadata through so browsers can reuse successful cover fetches.
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		if etag := resp.Header.Get("ETag"); etag != "" {
			w.Header().Set("ETag", etag)
		}
		if modified := resp.Header.Get("Last-Modified"); modified != "" {
			w.Header().Set("Last-Modified", modified)
		}
		w.Header().Set("Cache-Control", "private, max-age=3600")
		// stream body with a reasonable size limit to avoid resource abuse
		const maxCoverBytes = int64(5 * 1024 * 1024) // 5 MB
		lr := io.LimitReader(resp.Body, maxCoverBytes)
		_, _ = io.Copy(w, lr)
	}
}

// urlHost extracts host from a base URL string, tolerant of trailing slashes.
func urlHost(base string) string {
	if base == "" {
		return ""
	}
	if u, err := url.Parse(strings.TrimSpace(base)); err == nil {
		return u.Host
	}
	// fallback: strip schema
	base = strings.TrimPrefix(base, "https://")
	base = strings.TrimPrefix(base, "http://")
	base = strings.TrimRight(base, "/")
	return base
}

// presence checks removed

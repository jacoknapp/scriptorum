package httpapi

import (
	"crypto/tls"
	"encoding/json"
	"html/template"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"gitea.knapp/jacoknapp/scriptorum/internal/util"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSearch(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON":   func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"urlquery": url.QueryEscape,
	}
	u := &searchUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Get("/ui/search", u.handleSearch(s))
	// Readarr cover proxy (fetch fresh each call). Search UI will link images here
	r.Get("/ui/readarr-cover", s.serveReadarrCover())
}

type searchUI struct{ tpl *template.Template }

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
		// Prefer provider search: query both Readarr Ebook and Audiobook instances when available
		type SearchItem struct {
			providers.BookItem
			Provider                 string // primary provider used for display
			ProviderPayload          string // generic payload (when only one provider exists)
			ProviderEbookPayload     string // exact rendition for ebooks
			ProviderAudiobookPayload string // exact rendition for audiobooks
		}
		items := []SearchItem{}
		// Index by dedupe key to merge ebook/audiobook payloads for the same work
		idx := map[string]int{}

		// Helper to upsert item by key and attach payloads
		upsert := func(si SearchItem, ebook bool, payload string) {
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
				instE = providers.ReadarrInstance{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKey: cfg.Readarr.Ebooks.APIKey, LookupEndpoint: cfg.Readarr.Ebooks.LookupEndpoint, AddEndpoint: cfg.Readarr.Ebooks.AddEndpoint, AddMethod: cfg.Readarr.Ebooks.AddMethod, AddPayloadTemplate: cfg.Readarr.Ebooks.AddPayloadTemplate, DefaultQualityProfileID: cfg.Readarr.Ebooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Ebooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Ebooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Ebooks.InsecureSkipVerify}
			}
			if strings.TrimSpace(cfg.Readarr.Audiobooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Audiobooks.APIKey) != "" {
				instA = providers.ReadarrInstance{BaseURL: cfg.Readarr.Audiobooks.BaseURL, APIKey: cfg.Readarr.Audiobooks.APIKey, LookupEndpoint: cfg.Readarr.Audiobooks.LookupEndpoint, AddEndpoint: cfg.Readarr.Audiobooks.AddEndpoint, AddMethod: cfg.Readarr.Audiobooks.AddMethod, AddPayloadTemplate: cfg.Readarr.Audiobooks.AddPayloadTemplate, DefaultQualityProfileID: cfg.Readarr.Audiobooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Audiobooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Audiobooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Audiobooks.InsecureSkipVerify}
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
					upsert(SearchItem{BookItem: providers.BookItem{Title: b.Title, Authors: []string{dispAuthor}, CoverSmall: cover, CoverMedium: cover}, Provider: "readarr-ebook"}, true, string(cjson))
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
					upsert(SearchItem{BookItem: providers.BookItem{Title: b.Title, Authors: []string{dispAuthor}, CoverSmall: cover, CoverMedium: cover}, Provider: "readarr-audiobook"}, false, string(cjson))
				}
			}
		}

		// If neither provider is configured, fallback to public sources
		if len(items) == 0 {
			// Fallback to public sources if provider not configured
			ap := providers.NewAmazonPublic("www.amazon.com")
			if asin != "" {
				if book, err := ap.GetByASIN(r.Context(), asin); err == nil && book != nil {
					items = append(items, SearchItem{BookItem: providers.BookItem{ASIN: book.ASIN, Title: book.Title, Authors: book.Authors, ISBN10: book.ISBN10, ISBN13: book.ISBN13, CoverSmall: book.Image, CoverMedium: book.Image}})
				}
			} else if q != "" {
				if pubItems, err := ap.SearchBooks(r.Context(), q, page, limit); err == nil {
					for _, b := range pubItems {
						items = append(items, SearchItem{BookItem: providers.BookItem{ASIN: b.ASIN, Title: b.Title, Authors: b.Authors, CoverSmall: b.Image, CoverMedium: b.Image}})
					}
				}
			}

			if q != "" {
				ol := providers.NewOpenLibrary()
				if olItems, err := ol.Search(r.Context(), q, limit, page); err == nil {
					for _, b := range olItems {
						items = append(items, SearchItem{BookItem: b})
					}
				}
			}
		}

		data := map[string]any{"Query": q, "Items": items}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		// Ensure provider_payload is populated by merging ebook/audiobook renditions
		for i := range items {
			if items[i].ProviderPayload == "" {
				items[i].ProviderPayload = mergeProviderPayloads(items[i].ProviderEbookPayload, items[i].ProviderAudiobookPayload)
			}
		}
		_ = u.tpl.ExecuteTemplate(w, "search_partial.html", data)
	}
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
		// Build HTTP client; if this URL targets a configured Readarr host that has
		// InsecureSkipVerify enabled, use a client that skips TLS verification.
		client := &http.Client{Timeout: 12 * time.Second}
		cfg := s.settings.Get()
		wantsInsecure := false
		remoteHost := ru.Host
		if cfg != nil {
			if strings.EqualFold(remoteHost, urlHost(cfg.Readarr.Ebooks.BaseURL)) && cfg.Readarr.Ebooks.InsecureSkipVerify {
				wantsInsecure = true
			}
			if strings.EqualFold(remoteHost, urlHost(cfg.Readarr.Audiobooks.BaseURL)) && cfg.Readarr.Audiobooks.InsecureSkipVerify {
				wantsInsecure = true
			}
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
		// copy some headers and force no-cache so browser will fetch fresh
		if ct := resp.Header.Get("Content-Type"); ct != "" {
			w.Header().Set("Content-Type", ct)
		}
		if cl := resp.Header.Get("Content-Length"); cl != "" {
			w.Header().Set("Content-Length", cl)
		}
		w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate")
		w.Header().Set("Pragma", "no-cache")
		// stream body
		_, _ = io.Copy(w, resp.Body)
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

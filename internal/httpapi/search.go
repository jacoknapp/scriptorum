package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSearch(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON":   func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"urlquery": url.QueryEscape,
	}
	u := &searchUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Get("/ui/search", u.handleSearch(s))
	r.Get("/ui/presence", u.handlePresence(s))
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
					if items[i].ProviderEbookPayload == "" {
						items[i].ProviderEbookPayload = payload
					}
				} else {
					if items[i].ProviderAudiobookPayload == "" {
						items[i].ProviderAudiobookPayload = payload
					}
				}
				// prefer provider label if empty
				if items[i].Provider == "" {
					items[i].Provider = si.Provider
				}
				// fill cover if missing
				if items[i].BookItem.CoverMedium == "" && si.BookItem.CoverMedium != "" {
					items[i].BookItem.CoverMedium = si.BookItem.CoverMedium
				}
				if items[i].BookItem.CoverSmall == "" && si.BookItem.CoverSmall != "" {
					items[i].BookItem.CoverSmall = si.BookItem.CoverSmall
				}
				return
			}
			if ebook {
				si.ProviderEbookPayload = payload
			} else {
				si.ProviderAudiobookPayload = payload
			}
			idx[k] = len(items)
			items = append(items, si)
		}

		asin := providers.ExtractASINFromInput(q)
		cfg := s.settings.Get()
		// Build instances
		var instE, instA providers.ReadarrInstance
		if cfg != nil {
			if strings.TrimSpace(cfg.Readarr.Ebooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Ebooks.APIKey) != "" {
				instE = providers.ReadarrInstance{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKey: cfg.Readarr.Ebooks.APIKey, LookupEndpoint: cfg.Readarr.Ebooks.LookupEndpoint, AddEndpoint: cfg.Readarr.Ebooks.AddEndpoint, AddMethod: cfg.Readarr.Ebooks.AddMethod, AddPayloadTemplate: cfg.Readarr.Ebooks.AddPayloadTemplate, DefaultQualityProfileID: cfg.Readarr.Ebooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Ebooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Ebooks.DefaultTags}
			}
			if strings.TrimSpace(cfg.Readarr.Audiobooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Audiobooks.APIKey) != "" {
				instA = providers.ReadarrInstance{BaseURL: cfg.Readarr.Audiobooks.BaseURL, APIKey: cfg.Readarr.Audiobooks.APIKey, LookupEndpoint: cfg.Readarr.Audiobooks.LookupEndpoint, AddEndpoint: cfg.Readarr.Audiobooks.AddEndpoint, AddMethod: cfg.Readarr.Audiobooks.AddMethod, AddPayloadTemplate: cfg.Readarr.Audiobooks.AddPayloadTemplate, DefaultQualityProfileID: cfg.Readarr.Audiobooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Audiobooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Audiobooks.DefaultTags}
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
					// Send empty editions so the add step pins to this foreignEditionId only
					cand := map[string]any{"title": b.Title, "titleSlug": b.TitleSlug, "author": author, "editions": []any{}, "foreignBookId": b.ForeignBookId, "foreignEditionId": b.ForeignEditionId}
					cjson, _ := json.Marshal(cand)
					dispAuthor := ""
					if author != nil {
						if n, _ := author["name"].(string); n != "" {
							dispAuthor = n
						}
					}
					// Derive cover URL if available
					cover := firstNonEmpty(b.CoverUrl, b.RemotePoster, b.RemoteCover)
					if cover == "" && len(b.Images) > 0 {
						for _, im := range b.Images {
							if strings.EqualFold(im.CoverType, "cover") || strings.EqualFold(im.CoverType, "poster") {
								cover = firstNonEmpty(im.Url, im.RemoteUrl)
								if cover != "" {
									break
								}
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
					// Send empty editions so the add step pins to this foreignEditionId only
					cand := map[string]any{"title": b.Title, "titleSlug": b.TitleSlug, "author": author, "editions": []any{}, "foreignBookId": b.ForeignBookId, "foreignEditionId": b.ForeignEditionId}
					cjson, _ := json.Marshal(cand)
					dispAuthor := ""
					if author != nil {
						if n, _ := author["name"].(string); n != "" {
							dispAuthor = n
						}
					}
					// Derive cover URL if available
					cover := firstNonEmpty(b.CoverUrl, b.RemotePoster, b.RemoteCover)
					if cover == "" && len(b.Images) > 0 {
						for _, im := range b.Images {
							if strings.EqualFold(im.CoverType, "cover") || strings.EqualFold(im.CoverType, "poster") {
								cover = firstNonEmpty(im.Url, im.RemoteUrl)
								if cover != "" {
									break
								}
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
		_ = u.tpl.ExecuteTemplate(w, "search_partial.html", data)
	}
}

func (u *searchUI) handlePresence(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		title := r.URL.Query().Get("q")
		// Avoid repeated client-side retries; set short cache, and handle empty input quickly
		w.Header().Set("Cache-Control", "max-age=30")
		inABS := false
		if strings.TrimSpace(title) != "" && s.cfg.Audiobookshelf.BaseURL != "" {
			abs := providers.NewABS(s.cfg.Audiobookshelf.BaseURL, s.cfg.Audiobookshelf.Token, s.cfg.Audiobookshelf.SearchEndpoint)
			inABS, _ = abs.HasTitle(r.Context(), title)
		}
		badge := `<div class="flex flex-wrap gap-2">`
		if inABS {
			badge += `<span class="px-2 py-0.5 text-xs rounded bg-royal-100 text-royal-800">In Audiobookshelf</span>`
		} else {
			badge += `<span class="px-2 py-0.5 rounded bg-slate-100 text-slate-500">Not found</span>`
		}
		badge += `</div>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(badge))
	}
}

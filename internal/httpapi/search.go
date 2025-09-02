package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSearch(r chi.Router) {
	funcMap := template.FuncMap{"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) }}
	u := &searchUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Get("/ui/search", u.handleSearch(s))
	r.Get("/ui/presence", u.handlePresence(s))
}

type searchUI struct{ tpl *template.Template }

func (u *searchUI) handleSearch(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		q := strings.TrimSpace(r.URL.Query().Get("q"))
		asin := providers.ExtractASINFromInput(q)

		var merged []providers.BookItem
		seen := make(map[string]struct{})
		add := func(b providers.BookItem) {
			k := dedupeKey(b)
			if k == "" {
				return
			}
			if _, ok := seen[k]; ok {
				return
			}
			seen[k] = struct{}{}
			merged = append(merged, b)
		}

		ap := providers.NewAmazonPublic("www.amazon.com")
		if asin != "" {
			if book, err := ap.GetByASIN(r.Context(), asin); err == nil && book != nil {
				add(providers.BookItem{ASIN: book.ASIN, Title: book.Title, Authors: book.Authors, ISBN10: book.ISBN10, ISBN13: book.ISBN13, CoverSmall: book.Image, CoverMedium: book.Image})
			}
		} else if q != "" {
			if pubItems, err := ap.SearchBooks(r.Context(), q, 10); err == nil {
				for _, b := range pubItems {
					add(providers.BookItem{ASIN: b.ASIN, Title: b.Title, Authors: b.Authors, CoverSmall: b.Image, CoverMedium: b.Image})
				}
			}
		}

		if q != "" {
			ol := providers.NewOpenLibrary()
			if olItems, err := ol.Search(r.Context(), q); err == nil {
				for _, b := range olItems {
					add(b)
				}
			}
		}

		data := map[string]any{"Query": q, "Items": merged}
		_ = u.tpl.ExecuteTemplate(w, "search_partial.html", data)
	}
}

func (u *searchUI) handlePresence(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		title := r.URL.Query().Get("q")
		inABS := false
		if s.cfg.Audiobookshelf.BaseURL != "" {
			abs := providers.NewABS(s.cfg.Audiobookshelf.BaseURL, s.cfg.Audiobookshelf.Token, s.cfg.Audiobookshelf.SearchEndpoint)
			inABS, _ = abs.HasTitle(r.Context(), title)
		}
		badge := `<div class="flex flex-wrap gap-2">`
		if inABS {
			badge += `<span class="px-2 py-0.5 text-xs rounded bg-royal-100 text-royal-800">In Audiobookshelf</span>`
		}
		badge += `</div>`
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.Write([]byte(badge))
	}
}

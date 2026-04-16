package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

type readarrSyncSummary struct {
	Kind            string `json:"kind"`
	Imported        int    `json:"imported"`
	Reconciled      int    `json:"reconciled"`
	MatchedRequests int    `json:"matchedRequests"`
}

func (s *Server) apiReadarrSync() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		summaries, err := s.syncReadarrCatalog(r.Context(), kind)
		if err != nil {
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: readarr sync failed: %v\n", err)
			}
			http.Error(w, "readarr sync failed; check configuration and server logs", http.StatusBadGateway)
			return
		}
		writeJSON(w, summaries, http.StatusOK)
	}
}

func (s *Server) syncReadarrCatalog(ctx context.Context, requestedKind string) ([]readarrSyncSummary, error) {
	kinds := []string{"ebook", "audiobook"}
	if requestedKind != "" && requestedKind != "all" {
		kinds = []string{normalizeSyncKind(requestedKind)}
	}
	var summaries []readarrSyncSummary
	for _, kind := range kinds {
		inst, ok := s.readarrInstanceForFormat(kind)
		if !ok {
			continue
		}
		ra := providers.NewReadarrWithDB(inst, s.db.SQL())
		list, err := ra.ListBooks(ctx)
		if err != nil {
			return nil, fmt.Errorf("%s sync failed: %w", kind, err)
		}
		books := make([]db.ReadarrBook, 0, len(list))
		for _, book := range list {
			raw, _ := json.Marshal(book)
			isbn10, isbn13, asin := extractIdentifiers(book.LookupBook)
			books = append(books, db.ReadarrBook{
				SourceKind:       kind,
				ReadarrID:        int64(book.ID),
				Title:            strings.TrimSpace(book.Title),
				AuthorName:       authorNameFromLookupBook(book.LookupBook),
				ISBN10:           isbn10,
				ISBN13:           isbn13,
				ASIN:             asin,
				ForeignBookID:    strings.TrimSpace(book.ForeignBookId),
				ForeignEditionID: strings.TrimSpace(book.ForeignEditionId),
				Monitored:        book.Monitored,
				Grabbed:          book.Grabbed,
				BookFileCount:    book.Statistics.BookFileCount,
				ReadarrData:      raw,
			})
		}
		if err := s.db.ReplaceReadarrBooks(ctx, kind, books); err != nil {
			return nil, fmt.Errorf("%s import failed: %w", kind, err)
		}
		reconciled, matched, err := s.reconcileRequestsAgainstCatalog(ctx, kind)
		if err != nil {
			return nil, fmt.Errorf("%s reconcile failed: %w", kind, err)
		}
		summaries = append(summaries, readarrSyncSummary{
			Kind:            kind,
			Imported:        len(books),
			Reconciled:      reconciled,
			MatchedRequests: matched,
		})
	}
	return summaries, nil
}

func (s *Server) reconcileRequestsAgainstCatalog(ctx context.Context, kind string) (int, int, error) {
	requests, err := s.db.ListRequests(ctx, "", 1000)
	if err != nil {
		return 0, 0, err
	}
	reconciled := 0
	matched := 0
	for _, req := range requests {
		if normalizeSyncKind(req.Format) != kind {
			continue
		}
		match, err := s.findCatalogMatch(ctx, kind, req.Title, req.Authors, req.ISBN10, req.ISBN13, "", req.ReadarrReq)
		if err == nil && match != nil {
			matched++
			reconciled++
			reason := fmt.Sprintf("Readarr sync: %s in %s library", match.Availability(), kind)
			if err := s.db.UpdateRequestExternalStatus(ctx, req.ID, match.Availability(), match.ReadarrID, reason); err != nil {
				return reconciled, matched, err
			}
			continue
		}
		if err != nil && err != sql.ErrNoRows {
			return reconciled, matched, err
		}
		reconciled++
		if err := s.db.UpdateRequestExternalStatus(ctx, req.ID, "", 0, ""); err != nil {
			return reconciled, matched, err
		}
	}
	return reconciled, matched, nil
}

func (s *Server) findCatalogMatch(ctx context.Context, kind, title string, authors []string, isbn10, isbn13, asin string, providerPayload []byte) (*db.ReadarrBook, error) {
	query := db.ReadarrMatchQuery{
		SourceKind: normalizeSyncKind(kind),
		Title:      strings.TrimSpace(title),
		Authors:    authors,
		ISBN10:     strings.TrimSpace(isbn10),
		ISBN13:     strings.TrimSpace(isbn13),
		ASIN:       strings.TrimSpace(asin),
	}
	if len(providerPayload) > 0 {
		var raw map[string]any
		if json.Unmarshal(providerPayload, &raw) == nil {
			query.ForeignBookID = strings.TrimSpace(fmt.Sprint(raw["foreignBookId"]))
			query.ForeignEditionID = strings.TrimSpace(fmt.Sprint(raw["foreignEditionId"]))
			if query.Title == "" {
				query.Title = strings.TrimSpace(fmt.Sprint(raw["title"]))
			}
			if len(query.Authors) == 0 {
				if author, ok := raw["author"].(map[string]any); ok {
					if name, _ := author["name"].(string); name != "" {
						query.Authors = []string{name}
					}
				}
			}
		}
	}
	return s.db.FindReadarrBookMatch(ctxOrBackground(ctx), query)
}

func (s *Server) findCatalogMatchForPayload(kind string, p RequestPayload) (*db.ReadarrBook, error) {
	return s.findCatalogMatch(context.Background(), kind, p.Title, p.Authors, p.ISBN10, p.ISBN13, p.ASIN, []byte(p.ProviderPayload))
}

func (s *Server) readarrInstanceForFormat(format string) (providers.ReadarrInstance, bool) {
	cfg := s.settings.Get()
	switch normalizeSyncKind(format) {
	case "audiobook":
		c := cfg.Readarr.Audiobooks
		if strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(c.APIKey) == "" {
			return providers.ReadarrInstance{}, false
		}
		return providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}, true
	default:
		c := cfg.Readarr.Ebooks
		if strings.TrimSpace(c.BaseURL) == "" || strings.TrimSpace(c.APIKey) == "" {
			return providers.ReadarrInstance{}, false
		}
		return providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}, true
	}
}

func normalizeSyncKind(kind string) string {
	switch strings.ToLower(strings.TrimSpace(kind)) {
	case "audiobook", "audiobooks":
		return "audiobook"
	default:
		return "ebook"
	}
}

func authorNameFromLookupBook(book providers.LookupBook) string {
	if book.Author != nil {
		if name, _ := book.Author["name"].(string); name != "" {
			return name
		}
	}
	if len(book.Authors) > 0 {
		if name, _ := book.Authors[0]["name"].(string); name != "" {
			return name
		}
	}
	if book.AuthorTitle != "" {
		return parseAuthorNameFromTitle(book.AuthorTitle)
	}
	return ""
}

func extractIdentifiers(book providers.LookupBook) (string, string, string) {
	var isbn10, isbn13, asin string
	for _, id := range book.Identifiers {
		if id == nil {
			continue
		}
		typ, _ := id["type"].(string)
		val, _ := id["value"].(string)
		switch strings.ToLower(strings.TrimSpace(typ)) {
		case "isbn10", "isbn_10", "isbn-10":
			if isbn10 == "" {
				isbn10 = val
			}
		case "isbn13", "isbn_13", "isbn-13":
			if isbn13 == "" {
				isbn13 = val
			}
		case "asin":
			if asin == "" {
				asin = val
			}
		}
		if isbn10 == "" {
			if v, _ := id["isbn10"].(string); v != "" {
				isbn10 = v
			}
		}
		if isbn13 == "" {
			if v, _ := id["isbn13"].(string); v != "" {
				isbn13 = v
			}
		}
		if asin == "" {
			if v, _ := id["asin"].(string); v != "" {
				asin = v
			}
		}
	}
	return isbn10, isbn13, asin
}

func ctxOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}
	return ctx
}

func (s *Server) loadCatalogState(kind, title string, authors []string, isbn10, isbn13, asin string, payload string) string {
	sctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	match, err := s.findCatalogMatch(sctx, kind, title, authors, isbn10, isbn13, asin, []byte(payload))
	if err != nil || match == nil {
		return ""
	}
	return match.Availability()
}

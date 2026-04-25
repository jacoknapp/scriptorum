package httpapi

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
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

const (
	readarrAutoSyncInterval     = 15 * time.Minute
	readarrAutoSyncStartupDelay = 45 * time.Second
	readarrAutoSyncTimeout      = 10 * time.Minute
)

var errReadarrSyncInProgress = errors.New("readarr sync already in progress")

const catalogMatchCacheTTL = 5 * time.Minute

type readarrSyncRuntimeState struct {
	Running         bool
	Trigger         string
	LastStartedAt   time.Time
	LastCompletedAt time.Time
	LastError       string
	LastSummaries   []readarrSyncSummary
}

type readarrSyncViewData struct {
	AutoInterval    string
	ScheduleNote    string
	LastRunLabel    string
	LastResultLabel string
	LastResultClass string
	Running         bool
}

func (s *Server) StartBackgroundTasks(ctx context.Context) {
	if ctx == nil {
		ctx = context.Background()
	}
	s.backgroundTasks.Do(func() {
		go s.recoverProcessingApprovals(ctx)
		go s.runReadarrSyncLoop(ctx, readarrAutoSyncStartupDelay, readarrAutoSyncInterval)
	})
}

func (s *Server) runReadarrSyncLoop(ctx context.Context, initialDelay, interval time.Duration) {
	if ctx == nil {
		ctx = context.Background()
	}
	if initialDelay < 0 {
		initialDelay = 0
	}
	if interval <= 0 {
		interval = readarrAutoSyncInterval
	}
	timer := time.NewTimer(initialDelay)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
			s.runAutomaticReadarrSync(ctx)
			timer.Reset(interval)
		}
	}
}

func (s *Server) runAutomaticReadarrSync(parent context.Context) {
	if s.needsSetup() {
		return
	}
	ctx, cancel := context.WithTimeout(ctxOrBackground(parent), readarrAutoSyncTimeout)
	defer cancel()
	if _, err := s.runReadarrSync(ctx, "all", "automatic", "system"); err != nil {
		if errors.Is(err, errReadarrSyncInProgress) || errors.Is(err, context.Canceled) {
			return
		}
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: automatic readarr sync failed: %v\n", err)
		}
	}
}

func (s *Server) runReadarrSync(ctx context.Context, requestedKind, trigger, actor string) ([]readarrSyncSummary, error) {
	if !s.readarrSyncMu.TryLock() {
		return nil, errReadarrSyncInProgress
	}
	s.markReadarrSyncStart(trigger)
	defer s.readarrSyncMu.Unlock()

	summaries, err := s.syncReadarrCatalog(ctxOrBackground(ctx), requestedKind, actor)
	s.markReadarrSyncComplete(trigger, summaries, err)
	return summaries, err
}

func (s *Server) markReadarrSyncStart(trigger string) {
	s.readarrSyncStateMu.Lock()
	defer s.readarrSyncStateMu.Unlock()
	s.readarrSyncState.Running = true
	s.readarrSyncState.Trigger = strings.TrimSpace(trigger)
	s.readarrSyncState.LastStartedAt = time.Now()
}

func (s *Server) markReadarrSyncComplete(trigger string, summaries []readarrSyncSummary, err error) {
	s.readarrSyncStateMu.Lock()
	defer s.readarrSyncStateMu.Unlock()
	s.readarrSyncState.Running = false
	s.readarrSyncState.Trigger = strings.TrimSpace(trigger)
	s.readarrSyncState.LastCompletedAt = time.Now()
	s.readarrSyncState.LastSummaries = append([]readarrSyncSummary(nil), summaries...)
	if err != nil {
		s.readarrSyncState.LastError = err.Error()
		return
	}
	s.readarrSyncState.LastError = ""
}

func (s *Server) readarrSyncSnapshot() readarrSyncRuntimeState {
	s.readarrSyncStateMu.RLock()
	defer s.readarrSyncStateMu.RUnlock()
	state := s.readarrSyncState
	state.LastSummaries = append([]readarrSyncSummary(nil), s.readarrSyncState.LastSummaries...)
	return state
}

func (s *Server) readarrSyncView() readarrSyncViewData {
	state := s.readarrSyncSnapshot()
	view := readarrSyncViewData{
		AutoInterval:    "Auto 30m",
		ScheduleNote:    "Automatic sync runs every 15 minutes and shortly after startup.",
		LastRunLabel:    "No sync has run yet.",
		LastResultLabel: "Manual sync is available any time.",
		LastResultClass: "text-slate-400",
		Running:         state.Running,
	}
	if state.Running {
		if !state.LastStartedAt.IsZero() {
			view.LastRunLabel = "Sync in progress since " + state.LastStartedAt.Local().Format("Jan 2, 2006 3:04 PM")
		} else {
			view.LastRunLabel = "Sync in progress."
		}
		view.LastResultLabel = "A sync is running right now. Manual refresh is locked until it finishes."
		view.LastResultClass = "text-blue-300"
		return view
	}
	if !state.LastCompletedAt.IsZero() {
		view.LastRunLabel = fmt.Sprintf("Last %s sync: %s", syncTriggerLabel(state.Trigger), state.LastCompletedAt.Local().Format("Jan 2, 2006 3:04 PM"))
		if state.LastError != "" {
			view.LastResultLabel = "Last sync failed: " + state.LastError
			view.LastResultClass = "text-rose-300"
			return view
		}
		if len(state.LastSummaries) == 0 {
			view.LastResultLabel = "No configured Readarr libraries were available to sync."
			return view
		}
		view.LastResultLabel = formatReadarrSyncSummary(state.LastSummaries)
		view.LastResultClass = "text-emerald-300"
	}
	return view
}

func (s *Server) apiReadarrSync() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := strings.TrimSpace(r.URL.Query().Get("kind"))
		actor := "system"
		if u, ok := r.Context().Value(ctxUser).(*session); ok && strings.TrimSpace(u.Username) != "" {
			actor = u.Username
		}
		summaries, err := s.runReadarrSync(r.Context(), kind, "manual", actor)
		if err != nil {
			if errors.Is(err, errReadarrSyncInProgress) {
				http.Error(w, "readarr sync already in progress", http.StatusConflict)
				return
			}
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: readarr sync failed: %v\n", err)
			}
			http.Error(w, "readarr sync failed; check configuration and server logs", http.StatusBadGateway)
			return
		}
		writeJSON(w, summaries, http.StatusOK)
	}
}

func (s *Server) syncReadarrCatalog(ctx context.Context, requestedKind, actor string) ([]readarrSyncSummary, error) {
	if strings.TrimSpace(actor) == "" {
		actor = "system"
	}
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
		s.clearCatalogMatchCache()
		reconciled, matched, err := s.reconcileRequestsAgainstCatalog(ctx, kind, inst, actor)
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

func syncTriggerLabel(trigger string) string {
	switch strings.ToLower(strings.TrimSpace(trigger)) {
	case "automatic":
		return "automatic"
	default:
		return "manual"
	}
}

func syncKindDisplay(kind string) string {
	switch normalizeSyncKind(kind) {
	case "audiobook":
		return "Audiobooks"
	default:
		return "eBooks"
	}
}

func formatReadarrSyncSummary(summaries []readarrSyncSummary) string {
	if len(summaries) == 0 {
		return "No configured Readarr libraries were available to sync."
	}
	parts := make([]string, 0, len(summaries))
	for _, item := range summaries {
		parts = append(parts, fmt.Sprintf("%s: %d imported, %d matched", syncKindDisplay(item.Kind), item.Imported, item.MatchedRequests))
	}
	return strings.Join(parts, " | ")
}

func formatReadarrSyncMatchReason(kind, availability string) string {
	availability = strings.TrimSpace(availability)
	if availability == "" {
		return fmt.Sprintf("Readarr sync: found in %s library", kind)
	}
	return fmt.Sprintf("Readarr sync: %s in %s library", availability, kind)
}

func (s *Server) reconcileRequestsAgainstCatalog(ctx context.Context, kind string, inst providers.ReadarrInstance, actor string) (int, int, error) {
	requests, err := s.db.ListRequests(ctx, "", 1000)
	if err != nil {
		return 0, 0, err
	}
	if strings.TrimSpace(actor) == "" {
		actor = "system"
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
			if strings.ToLower(strings.TrimSpace(req.Status)) != "declined" {
				if err := s.completeRequestFromCatalogMatch(ctx, &req, match, inst, actor, "", false); err != nil {
					return reconciled, matched, err
				}
				continue
			}
			reason := formatReadarrSyncMatchReason(kind, match.Availability())
			if err := s.db.UpdateRequestExternalStatus(ctx, req.ID, match.Availability(), match.ReadarrID, reason); err != nil {
				return reconciled, matched, err
			}
			if cover := s.requestCoverFromPayload(req.Format, match.ReadarrData); cover != "" {
				_ = s.db.UpdateRequestCover(ctx, req.ID, cover)
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
	query := buildCatalogMatchQuery(kind, title, authors, isbn10, isbn13, asin, providerPayload)
	return s.findCatalogMatchQuery(ctx, query)
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

func buildCatalogMatchQuery(kind, title string, authors []string, isbn10, isbn13, asin string, providerPayload []byte) db.ReadarrMatchQuery {
	query := db.ReadarrMatchQuery{
		SourceKind: normalizeSyncKind(kind),
		Title:      strings.TrimSpace(title),
		Authors:    authors,
		ISBN10:     strings.TrimSpace(isbn10),
		ISBN13:     strings.TrimSpace(isbn13),
		ASIN:       strings.TrimSpace(asin),
	}
	if len(providerPayload) == 0 {
		return query
	}

	var raw map[string]any
	if json.Unmarshal(providerPayload, &raw) != nil {
		return query
	}
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
	return query
}

func (s *Server) findCatalogMatchQuery(ctx context.Context, query db.ReadarrMatchQuery) (*db.ReadarrBook, error) {
	key := catalogMatchCacheKey(query)
	if key != "" {
		if match, ok, found := s.catalogMatchCacheLookup(key); ok {
			if !found {
				return nil, sql.ErrNoRows
			}
			return match, nil
		}
	}

	match, err := s.db.FindReadarrBookMatch(ctxOrBackground(ctx), query)
	if key != "" {
		switch err {
		case nil:
			s.catalogMatchCacheStore(key, match, true)
		case sql.ErrNoRows:
			s.catalogMatchCacheStore(key, nil, false)
		}
	}
	return match, err
}

func catalogMatchCacheKey(query db.ReadarrMatchQuery) string {
	parts := []string{
		strings.TrimSpace(query.SourceKind),
		strings.ToLower(strings.TrimSpace(query.Title)),
		strings.TrimSpace(query.ISBN13),
		strings.TrimSpace(query.ISBN10),
		strings.TrimSpace(query.ASIN),
		strings.TrimSpace(query.ForeignBookID),
		strings.TrimSpace(query.ForeignEditionID),
	}
	if len(query.Authors) > 0 {
		parts = append(parts, strings.ToLower(strings.TrimSpace(query.Authors[0])))
	}
	return strings.Join(parts, "|")
}

func (s *Server) catalogMatchCacheLookup(key string) (*db.ReadarrBook, bool, bool) {
	s.catalogMatchCacheMu.RLock()
	entry, ok := s.catalogMatchCache[key]
	s.catalogMatchCacheMu.RUnlock()
	if !ok {
		return nil, false, false
	}
	if time.Now().After(entry.exp) {
		s.catalogMatchCacheMu.Lock()
		delete(s.catalogMatchCache, key)
		s.catalogMatchCacheMu.Unlock()
		return nil, false, false
	}
	if entry.notFound {
		return nil, true, false
	}
	if entry.match == nil {
		return nil, false, false
	}
	copy := *entry.match
	return &copy, true, true
}

func (s *Server) catalogMatchCacheStore(key string, match *db.ReadarrBook, found bool) {
	entry := catalogMatchCacheEntry{
		exp:      time.Now().Add(catalogMatchCacheTTL),
		notFound: !found,
	}
	if match != nil {
		copy := *match
		entry.match = &copy
	}
	s.catalogMatchCacheMu.Lock()
	s.catalogMatchCache[key] = entry
	s.catalogMatchCacheMu.Unlock()
}

func (s *Server) clearCatalogMatchCache() {
	s.catalogMatchCacheMu.Lock()
	s.catalogMatchCache = make(map[string]catalogMatchCacheEntry)
	s.catalogMatchCacheMu.Unlock()
}

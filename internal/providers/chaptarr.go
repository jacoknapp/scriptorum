package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/bookidentity"
)

// ChaptarrCapabilities is the small, format-aware portion of the Chaptarr API
// that Scriptorum needs during onboarding and configuration validation.
type ChaptarrCapabilities struct {
	Version          string                    `json:"version"`
	QualityProfiles  []ChaptarrProfile         `json:"qualityProfiles"`
	MetadataProfiles []ChaptarrMetadataProfile `json:"metadataProfiles"`
	RootFolders      []ChaptarrRootFolder      `json:"rootFolders"`
}

type ChaptarrProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ProfileType string `json:"profileType"`
}

type ChaptarrMetadataProfile struct {
	ID          int    `json:"id"`
	Name        string `json:"name"`
	ProfileType int    `json:"profileType"`
}

type ChaptarrRootFolder struct {
	ID                          int    `json:"id"`
	Name                        string `json:"name"`
	Path                        string `json:"path"`
	Accessible                  *bool  `json:"accessible"`
	FolderType                  int    `json:"folderType"`
	IsEffectiveDefaultEbook     bool   `json:"isEffectiveDefaultEbook"`
	IsEffectiveDefaultAudiobook bool   `json:"isEffectiveDefaultAudiobook"`
}

func (r *Readarr) isChaptarr() bool {
	return strings.EqualFold(strings.TrimSpace(r.inst.Backend), "chaptarr")
}

func (r *Readarr) chaptarrJSON(ctx context.Context, method, path string, query url.Values, input any, output any) ([]byte, error) {
	var body io.Reader
	if input != nil {
		encoded, err := json.Marshal(input)
		if err != nil {
			return nil, err
		}
		body = bytes.NewReader(encoded)
	}
	req, u, err := r.newRequest(ctx, method, path, query, body)
	if err != nil {
		return nil, err
	}
	if input != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	raw, readErr := io.ReadAll(resp.Body)
	if readErr != nil {
		return nil, fmt.Errorf("failed to read Chaptarr response: %w", readErr)
	}
	if resp.StatusCode >= 400 {
		return raw, readarrHTTPError("Chaptarr request failed", u, r.inst.APIKey, resp, raw)
	}
	if output != nil && len(raw) > 0 {
		if err := json.Unmarshal(raw, output); err != nil {
			return raw, fmt.Errorf("invalid JSON from Chaptarr %s: %w", path, err)
		}
	}
	return raw, nil
}

func (r *Readarr) ChaptarrCapabilities(ctx context.Context) (*ChaptarrCapabilities, error) {
	var status struct {
		AppName string `json:"appName"`
		Version string `json:"version"`
	}
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/system/status", nil, nil, &status); err != nil {
		return nil, err
	}
	if !strings.EqualFold(strings.TrimSpace(status.AppName), "Chaptarr") || strings.TrimSpace(status.Version) == "" {
		return nil, fmt.Errorf("configured server is not Chaptarr")
	}
	out := &ChaptarrCapabilities{Version: status.Version}
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/qualityprofile", nil, nil, &out.QualityProfiles); err != nil {
		return nil, err
	}
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/metadataprofile", nil, nil, &out.MetadataProfiles); err != nil {
		return nil, err
	}
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/rootfolder", nil, nil, &out.RootFolders); err != nil {
		return nil, err
	}
	if err := r.validateChaptarrSelections(out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Readarr) validateChaptarrSelections(c *ChaptarrCapabilities) error {
	quality := map[string]map[int]bool{"ebook": {}, "audiobook": {}}
	for _, profile := range c.QualityProfiles {
		kind := strings.ToLower(strings.TrimSpace(profile.ProfileType))
		if _, ok := quality[kind]; ok {
			quality[kind][profile.ID] = true
		}
	}
	metadata := map[int]map[int]bool{1: {}, 2: {}}
	for _, profile := range c.MetadataProfiles {
		if _, ok := metadata[profile.ProfileType]; ok {
			metadata[profile.ProfileType][profile.ID] = true
		}
	}
	paths := make(map[string]bool)
	for _, root := range c.RootFolders {
		if root.Accessible == nil || *root.Accessible {
			paths[strings.TrimSpace(root.Path)] = true
		}
	}
	checks := []struct {
		label string
		id    int
		set   map[int]bool
	}{
		{"ebook quality profile", r.inst.EbookQualityProfileID, quality["ebook"]},
		{"audiobook quality profile", r.inst.AudiobookQualityProfileID, quality["audiobook"]},
		{"ebook metadata profile", r.inst.EbookMetadataProfileID, metadata[2]},
		{"audiobook metadata profile", r.inst.AudiobookMetadataProfileID, metadata[1]},
	}
	for _, check := range checks {
		if check.id != 0 && !check.set[check.id] {
			return fmt.Errorf("chaptarr %s %d does not exist or has the wrong media type", check.label, check.id)
		}
	}
	for label, path := range map[string]string{
		"ebook root folder":     r.inst.EbookRootFolderPath,
		"audiobook root folder": r.inst.AudiobookRootFolderPath,
	} {
		if strings.TrimSpace(path) != "" && !paths[strings.TrimSpace(path)] {
			return fmt.Errorf("chaptarr %s %q is missing or inaccessible", label, path)
		}
	}
	return nil
}

func (r *Readarr) chaptarrFormat() string {
	if strings.EqualFold(strings.TrimSpace(r.inst.MediaType), "audiobook") {
		return "audiobook"
	}
	return "ebook"
}

func (r *Readarr) chaptarrRoot(format string) string {
	if format == "audiobook" {
		return r.inst.AudiobookRootFolderPath
	}
	return r.inst.EbookRootFolderPath
}

func (r *Readarr) validateChaptarrRequestConfig() error {
	checks := []struct {
		label string
		set   bool
	}{
		{"ebook quality profile", r.inst.EbookQualityProfileID > 0},
		{"audiobook quality profile", r.inst.AudiobookQualityProfileID > 0},
		{"ebook metadata profile", r.inst.EbookMetadataProfileID > 0},
		{"audiobook metadata profile", r.inst.AudiobookMetadataProfileID > 0},
		{"ebook root folder", strings.TrimSpace(r.inst.EbookRootFolderPath) != ""},
		{"audiobook root folder", strings.TrimSpace(r.inst.AudiobookRootFolderPath) != ""},
	}
	for _, check := range checks {
		if !check.set {
			return fmt.Errorf("chaptarr %s is not configured", check.label)
		}
	}
	return nil
}

func stringFromMap(m map[string]any, keys ...string) string {
	for _, key := range keys {
		if value, ok := m[key]; ok && value != nil {
			if s := strings.TrimSpace(fmt.Sprint(value)); s != "" && s != "<nil>" {
				return s
			}
		}
	}
	return ""
}

func positiveInt(v any) int {
	switch n := v.(type) {
	case int:
		return n
	case int64:
		return int(n)
	case float64:
		return int(n)
	case json.Number:
		i, _ := n.Int64()
		return int(i)
	case string:
		i, _ := strconv.Atoi(strings.TrimSpace(n))
		return i
	default:
		return 0
	}
}

func normalizeBookTitle(s string) string {
	return bookidentity.NormalizeText(s)
}

func (r *Readarr) buildChaptarrAddPayload(raw json.RawMessage, author map[string]any) (map[string]any, error) {
	var selected map[string]any
	if err := json.Unmarshal(raw, &selected); err != nil {
		return nil, err
	}
	format := r.chaptarrFormat()
	selectedAuthor, _ := selected["author"].(map[string]any)
	if selectedAuthor == nil {
		selectedAuthor = map[string]any{}
	}
	authorName := stringFromMap(selectedAuthor, "authorName", "name")
	foreignAuthorID := stringFromMap(selectedAuthor, "foreignAuthorId")
	if author != nil {
		if authorName == "" {
			authorName = stringFromMap(author, "authorName", "name")
		}
		if foreignAuthorID == "" {
			foreignAuthorID = stringFromMap(author, "foreignAuthorId")
		}
	}
	if strings.TrimSpace(stringFromMap(selected, "title")) == "" || authorName == "" {
		return nil, fmt.Errorf("chaptarr selection is missing title or author identity")
	}
	root := r.chaptarrRoot(format)
	authorPayload := map[string]any{
		"authorName": authorName, "foreignAuthorId": foreignAuthorID,
		"ebookQualityProfileId":      r.inst.EbookQualityProfileID,
		"audiobookQualityProfileId":  r.inst.AudiobookQualityProfileID,
		"ebookMetadataProfileId":     r.inst.EbookMetadataProfileID,
		"audiobookMetadataProfileId": r.inst.AudiobookMetadataProfileID,
		"rootFolderPath":             root, "ebookRootFolderPath": r.inst.EbookRootFolderPath,
		"audiobookRootFolderPath": r.inst.AudiobookRootFolderPath,
		"ebookMonitorFuture":      format == "ebook", "audiobookMonitorFuture": format == "audiobook",
		"monitored": true, "monitorNewItems": "none",
		"addOptions": map[string]any{"monitor": "none", "searchForMissingBooks": false},
	}
	if id := positiveInt(author["id"]); id > 0 {
		authorPayload["id"] = id
	}
	// Chaptarr's add validation rejects the whole POST when any edition lacks a
	// Title ("Cannot add book: one or more editions are missing Title"), and
	// search payloads routinely carry id-only edition stubs. Send only fully
	// hydrated editions; with none, Chaptarr hydrates the edition list from its
	// metadata server itself and prepareChaptarrBook pins the right one later.
	rawEditions, _ := selected["editions"].([]any)
	editions := make([]any, 0, len(rawEditions))
	for _, e := range rawEditions {
		if em, ok := e.(map[string]any); ok && strings.TrimSpace(stringFromMap(em, "title")) != "" {
			editions = append(editions, em)
		}
	}
	payload := map[string]any{
		"title": stringFromMap(selected, "title"), "foreignBookId": stringFromMap(selected, "foreignBookId"),
		"mediaType": format, "monitored": false, "ebookMonitored": false, "audiobookMonitored": false,
		"rootFolderPath": root, "ebookQualityProfileId": r.inst.EbookQualityProfileID,
		"audiobookQualityProfileId":  r.inst.AudiobookQualityProfileID,
		"ebookMetadataProfileId":     r.inst.EbookMetadataProfileID,
		"audiobookMetadataProfileId": r.inst.AudiobookMetadataProfileID,
		"author":                     authorPayload, "addOptions": map[string]any{"searchForNewBook": false},
		"editions": editions,
	}
	if id := positiveInt(author["id"]); id > 0 {
		payload["authorId"] = id
	}
	return payload, nil
}

func (r *Readarr) findChaptarrAuthor(ctx context.Context, selected map[string]any) (map[string]any, error) {
	a, _ := selected["author"].(map[string]any)
	foreignID := stringFromMap(a, "foreignAuthorId")
	name := normalizeBookTitle(stringFromMap(a, "authorName", "name"))
	// Chaptarr lookup results carry the local author id. Validate that specific
	// author directly instead of downloading the entire author catalog, which
	// can consume the request's three-minute deadline on large libraries.
	if id := positiveInt(a["id"]); id > 0 {
		var author map[string]any
		if _, err := r.chaptarrJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/author/%d", id), nil, nil, &author); err != nil {
			return nil, err
		}
		actualForeignID := stringFromMap(author, "foreignAuthorId")
		actualName := normalizeBookTitle(stringFromMap(author, "authorName", "name"))
		foreignMatches := foreignID != "" && strings.EqualFold(actualForeignID, foreignID)
		nameMatches := name != "" && bookidentity.AuthorNamesMatch(name, actualName)
		// Chaptarr canonicalizes provider ids (for example Goodreads to
		// Hardcover) during metadata sync. The stable local id plus either the
		// same author name or the same provider id is sufficient identity.
		if !foreignMatches && !nameMatches {
			return nil, fmt.Errorf("chaptarr author id %d is %q, not %q", id, stringFromMap(author, "authorName", "name"), stringFromMap(a, "authorName", "name"))
		}
		return author, nil
	}
	var authors []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/author", nil, nil, &authors); err != nil {
		return nil, err
	}
	var nameMatches []map[string]any
	for _, author := range authors {
		if foreignID != "" && strings.EqualFold(stringFromMap(author, "foreignAuthorId"), foreignID) {
			return author, nil
		}
		if name != "" && bookidentity.AuthorNamesMatch(name, stringFromMap(author, "authorName", "name")) {
			nameMatches = append(nameMatches, author)
		}
	}
	if len(nameMatches) == 1 {
		return nameMatches[0], nil
	}
	return nil, nil
}

func (r *Readarr) chaptarrBooksForAuthor(ctx context.Context, authorID int) ([]map[string]any, error) {
	var books []map[string]any
	_, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/book", url.Values{"authorId": {fmt.Sprint(authorID)}}, nil, &books)
	return books, err
}

// chaptarrBookIDMatches reports whether the requested provider id identifies
// this local book row. Chaptarr's metadata pipeline canonicalizes
// foreignBookId to whichever source (Goodreads, Hardcover, ...) it trusts most
// once the full author bibliography syncs, but it keeps the original Goodreads
// work/book ids alongside -- so a Goodreads id from search must be compared
// against all three.
func chaptarrBookIDMatches(book map[string]any, foreignID string) bool {
	if foreignID == "" {
		return false
	}
	for _, key := range []string{"foreignBookId", "goodreadsWorkId", "goodreadsBookId"} {
		if strings.EqualFold(stringFromMap(book, key), foreignID) {
			return true
		}
	}
	return false
}

// findChaptarrBook matches by provider identifier or normalized title, then
// prefers (but does not require) the requested media type.
//
// Identifier equality alone qualifies a candidate and outranks any title-only
// match: after Chaptarr's bibliography sync, the canonical title can differ
// completely from the one the search result carried (e.g. Goodreads "Mage
// Tank: Book One (Mage Tank, #1)" vs Hardcover "Mage Tank: A LitRPG
// Adventure"), so requiring a title match meant the request polled until the
// deadline and the book stayed unmonitored.
//
// mediaType is a preference used only to rank otherwise-equal candidates:
// Chaptarr's metadata models most titles as ebook/physical editions only, and
// requiring mediaType equality made every audiobook request fail the same way.
func (r *Readarr) findChaptarrBook(books []map[string]any, title, foreignID, format string) map[string]any {
	bestRank := 0
	var best map[string]any
	ambiguous := false
	for _, book := range books {
		titleScore := bookidentity.TitleScore(title, stringFromMap(book, "title"))
		idMatch := chaptarrBookIDMatches(book, foreignID)
		if titleScore == 0 && !idMatch {
			continue
		}
		rank := titleScore * 10
		if idMatch {
			rank += 40
		}
		if strings.EqualFold(stringFromMap(book, "mediaType"), format) {
			rank += 2
		}
		if rank > bestRank {
			best, bestRank, ambiguous = book, rank, false
		} else if rank == bestRank {
			// Equal-confidence local rows are distinct works until an identifier
			// proves otherwise. Waiting safely is preferable to monitoring one at
			// random based on API or database ordering.
			if positiveInt(best["id"]) != positiveInt(book["id"]) {
				ambiguous = true
			}
		}
	}
	if bestRank == 0 || ambiguous {
		return nil
	}
	return best
}

func chaptarrBookResolved(book map[string]any) bool {
	foreignEdition := stringFromMap(book, "foreignEditionId")
	images, _ := book["images"].([]any)
	return positiveInt(book["id"]) > 0 && stringFromMap(book, "releaseDate") != "" && len(images) > 0 && foreignEdition != "" && !strings.HasPrefix(strings.ToLower(foreignEdition), "default-")
}

func (r *Readarr) waitForChaptarrBook(ctx context.Context, authorID int, title, foreignID, format string) (map[string]any, error) {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		books, err := r.chaptarrBooksForAuthor(ctx, authorID)
		if err != nil {
			return nil, err
		}
		if book := r.findChaptarrBook(books, title, foreignID, format); book != nil && chaptarrBookResolved(book) {
			return book, nil
		}
		select {
		case <-ctx.Done():
			return nil, fmt.Errorf("chaptarr metadata did not resolve the selected %s before the deadline: %w", format, ctx.Err())
		case <-ticker.C:
		}
	}
}

func (r *Readarr) enableChaptarrAuthorFormat(ctx context.Context, authorID int) error {
	var author map[string]any
	path := fmt.Sprintf("/api/v1/author/%d", authorID)
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, path, nil, nil, &author); err != nil {
		return err
	}
	flag := "ebookMonitorFuture"
	if r.chaptarrFormat() == "audiobook" {
		flag = "audiobookMonitorFuture"
	}
	if enabled, _ := author[flag].(bool); enabled {
		return nil
	}
	author[flag] = true
	author["monitored"] = true
	if _, err := r.chaptarrJSON(ctx, http.MethodPut, path, nil, author, nil); err != nil {
		return err
	}
	var verified map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, path, nil, nil, &verified); err != nil {
		return err
	}
	if enabled, _ := verified[flag].(bool); !enabled {
		return fmt.Errorf("chaptarr did not retain the %s author monitor setting", r.chaptarrFormat())
	}
	return nil
}

// pickChaptarrEdition ranks the format-matching editions and returns the index
// of the best one (-1 when no edition of the format exists). Chaptarr's edition
// list is in no useful order — a popular work can expose dozens of editions
// with translations and low-vote variants listed before the canonical one — so
// taking the first format match risks pinning a German narration of an English
// book. Rank: preferred language first (from discovery.languages; "und" beats
// a wrong language but loses to a right one), then rating votes as a
// popularity proxy, then a real duration as a tiebreak for hydrated metadata.
func pickChaptarrEdition(editions []map[string]any, format string, preferredLanguages []string) int {
	preferred := make(map[string]bool, len(preferredLanguages))
	for _, lang := range preferredLanguages {
		if lang = strings.ToLower(strings.TrimSpace(lang)); lang != "" {
			preferred[lang] = true
		}
	}
	if len(preferred) == 0 {
		preferred["eng"] = true
	}
	langRank := func(e map[string]any) int {
		lang := strings.ToLower(stringFromMap(e, "language"))
		switch {
		case preferred[lang]:
			return 2
		case lang == "" || lang == "und":
			return 1
		default:
			return 0
		}
	}
	votes := func(e map[string]any) int {
		ratings, _ := e["ratings"].(map[string]any)
		return positiveInt(ratings["votes"])
	}
	best := -1
	for i, edition := range editions {
		if !strings.EqualFold(stringFromMap(edition, "format"), format) {
			continue
		}
		if best < 0 {
			best = i
			continue
		}
		if lr, br := langRank(edition), langRank(editions[best]); lr != br {
			if lr > br {
				best = i
			}
			continue
		}
		if ev, bv := votes(edition), votes(editions[best]); ev != bv {
			if ev > bv {
				best = i
			}
			continue
		}
		if positiveInt(edition["durationSeconds"]) > positiveInt(editions[best]["durationSeconds"]) {
			best = i
		}
	}
	return best
}

// prepareChaptarrBook pins the single edition of the requested format so
// Chaptarr searches for exactly that. Its second return value reports whether a
// format-specific edition was pinned. When Chaptarr's metadata exposes no
// edition of the requested format (routine for audiobooks, whose metadata is
// usually ebook/physical only), it cannot pin one; rather than erroring it
// leaves the book editable-any and lets the caller monitor the book at book
// level so the request still completes and a search still runs.
func (r *Readarr) prepareChaptarrBook(ctx context.Context, bookID int) (map[string]any, bool, error) {
	var book map[string]any
	bookPath := fmt.Sprintf("/api/v1/book/%d", bookID)
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, bookPath, nil, nil, &book); err != nil {
		return nil, false, err
	}
	authorID := positiveInt(book["authorId"])
	if authorID <= 0 {
		return nil, false, fmt.Errorf("chaptarr book %d has no author id", bookID)
	}
	if err := r.enableChaptarrAuthorFormat(ctx, authorID); err != nil {
		return nil, false, err
	}
	var editions []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/edition", url.Values{"bookId": {fmt.Sprint(bookID)}}, nil, &editions); err != nil {
		return nil, false, err
	}
	format := r.chaptarrFormat()
	chosen := pickChaptarrEdition(editions, format, r.inst.PreferredLanguages)
	if chosen < 0 {
		// No edition of the requested format exists. Monitor the book at book
		// level with anyEditionOk so Chaptarr can grab whatever edition it can
		// find, instead of failing the request.
		book["anyEditionOk"] = true
		book["monitored"] = true
		if _, err := r.chaptarrJSON(ctx, http.MethodPut, bookPath, nil, book, nil); err != nil {
			return nil, false, err
		}
		return book, false, nil
	}
	chosenID := positiveInt(editions[chosen]["id"])
	chosenForeignID := stringFromMap(editions[chosen], "foreignEditionId")
	for i := range editions {
		editions[i]["monitored"] = i == chosen
		editions[i]["manualAdd"] = i == chosen
	}
	book["anyEditionOk"] = false
	book["editions"] = editions
	if _, err := r.chaptarrJSON(ctx, http.MethodPut, bookPath, nil, book, nil); err != nil {
		return nil, false, err
	}
	var verifiedEditions []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/edition", url.Values{"bookId": {fmt.Sprint(bookID)}}, nil, &verifiedEditions); err != nil {
		return nil, false, err
	}
	monitoredCount := 0
	selectedPersisted := false
	for _, edition := range verifiedEditions {
		isMonitored, _ := edition["monitored"].(bool)
		if !isMonitored {
			continue
		}
		monitoredCount++
		if strings.EqualFold(stringFromMap(edition, "format"), format) {
			selectedPersisted = (chosenID > 0 && positiveInt(edition["id"]) == chosenID) ||
				(chosenForeignID != "" && strings.EqualFold(stringFromMap(edition, "foreignEditionId"), chosenForeignID))
		}
	}
	if monitoredCount != 1 || !selectedPersisted {
		return nil, false, fmt.Errorf("chaptarr did not retain exactly one selected %s edition for %q", format, stringFromMap(book, "title"))
	}
	return book, true, nil
}

func (r *Readarr) requestChaptarrBook(ctx context.Context, raw json.RawMessage) ([]byte, []byte, error) {
	if err := r.validateChaptarrRequestConfig(); err != nil {
		return raw, nil, err
	}
	if _, err := r.ChaptarrCapabilities(ctx); err != nil {
		return raw, nil, err
	}
	var selected map[string]any
	if err := json.Unmarshal(raw, &selected); err != nil {
		return raw, nil, err
	}
	author, err := r.findChaptarrAuthor(ctx, selected)
	if err != nil {
		return raw, nil, err
	}
	payloadMap, err := r.buildChaptarrAddPayload(raw, author)
	if err != nil {
		return raw, nil, err
	}
	payload, _ := json.Marshal(payloadMap)
	format := r.chaptarrFormat()
	title := stringFromMap(selected, "title")
	foreignID := stringFromMap(selected, "foreignBookId")
	authorID := positiveInt(author["id"])
	var target map[string]any
	if authorID > 0 {
		books, listErr := r.chaptarrBooksForAuthor(ctx, authorID)
		if listErr != nil {
			return payload, nil, listErr
		}
		target = r.findChaptarrBook(books, title, foreignID, format)
	}
	var postResponse []byte
	if target == nil {
		var posted map[string]any
		postResponse, err = r.chaptarrJSON(ctx, http.MethodPost, "/api/v1/book", nil, payloadMap, &posted)
		if err != nil {
			return payload, postResponse, err
		}
		if authorID <= 0 {
			authorID = positiveInt(posted["authorId"])
			if authorID <= 0 {
				if nested, ok := posted["author"].(map[string]any); ok {
					authorID = positiveInt(nested["id"])
				}
			}
		}
	}
	if authorID <= 0 {
		author, err = r.findChaptarrAuthor(ctx, selected)
		if err != nil || author == nil {
			return payload, postResponse, fmt.Errorf("chaptarr accepted the book but did not return a resolvable author")
		}
		authorID = positiveInt(author["id"])
	}
	if target == nil || !chaptarrBookResolved(target) {
		target, err = r.waitForChaptarrBook(ctx, authorID, title, foreignID, format)
		if err != nil {
			return payload, postResponse, err
		}
	}
	bookID := positiveInt(target["id"])
	_, pinnedFormatEdition, err := r.prepareChaptarrBook(ctx, bookID)
	if err != nil {
		return payload, postResponse, err
	}
	monitorBody := map[string]any{"bookIds": []int{bookID}, "monitored": true}
	if _, err := r.chaptarrJSON(ctx, http.MethodPut, "/api/v1/book/monitor", nil, monitorBody, nil); err != nil {
		return payload, postResponse, err
	}
	var verified map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, fmt.Sprintf("/api/v1/book/%d", bookID), nil, nil, &verified); err != nil {
		return payload, postResponse, err
	}
	monitored, _ := verified["monitored"].(bool)
	if !monitored {
		return payload, postResponse, fmt.Errorf("chaptarr did not retain monitoring for %q", title)
	}
	// When a format-specific edition was pinned, also require the per-format
	// monitor flag to have stuck. When none existed (e.g. an audiobook with no
	// audiobook edition), book-level monitoring is all Chaptarr can offer, so
	// don't demand the per-format flag -- the book is monitored and searchable.
	if pinnedFormatEdition {
		if formatMonitored, _ := verified[format+"Monitored"].(bool); !formatMonitored {
			return payload, postResponse, fmt.Errorf("chaptarr did not retain %s monitoring for %q", format, title)
		}
	}
	command := map[string]any{"name": "BookSearch", "bookIds": []int{bookID}}
	if _, err := r.chaptarrJSON(ctx, http.MethodPost, "/api/v1/command", nil, command, nil); err != nil {
		return payload, postResponse, err
	}
	response, _ := json.Marshal(verified)
	return payload, response, nil
}

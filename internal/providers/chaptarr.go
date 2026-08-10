package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"
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
	return strings.Join(strings.Fields(strings.ToLower(strings.TrimSpace(s))), " ")
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
	editions, _ := selected["editions"].([]any)
	if len(editions) == 0 {
		if fe := strings.TrimSpace(stringFromMap(selected, "foreignEditionId")); fe != "" {
			editions = []any{map[string]any{"foreignEditionId": fe, "monitored": true}}
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
	var authors []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/author", nil, nil, &authors); err != nil {
		return nil, err
	}
	var nameMatches []map[string]any
	for _, author := range authors {
		if foreignID != "" && strings.EqualFold(stringFromMap(author, "foreignAuthorId"), foreignID) {
			return author, nil
		}
		if name != "" && normalizeBookTitle(stringFromMap(author, "authorName", "name")) == name {
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

// findChaptarrBook matches by media type + normalized title, then prefers
// (but does not require) a foreignBookId match. Chaptarr's metadata pipeline
// canonicalizes a book's foreignBookId to whichever source (Goodreads,
// Hardcover, ...) it trusts most once the full author bibliography syncs,
// which frequently differs from the id the original search result carried.
// Requiring an exact match here caused every add to time out: the book was
// present under the same title but a re-mapped foreignBookId, so it was
// never found and requestChaptarrBook kept polling until the deadline.
func (r *Readarr) findChaptarrBook(books []map[string]any, title, foreignID, format string) map[string]any {
	var candidates []map[string]any
	for _, book := range books {
		if !strings.EqualFold(stringFromMap(book, "mediaType"), format) {
			continue
		}
		if normalizeBookTitle(stringFromMap(book, "title")) != normalizeBookTitle(title) {
			continue
		}
		candidates = append(candidates, book)
	}
	if len(candidates) == 0 {
		return nil
	}
	if foreignID != "" {
		for _, book := range candidates {
			if strings.EqualFold(stringFromMap(book, "foreignBookId"), foreignID) {
				return book
			}
		}
	}
	sort.SliceStable(candidates, func(i, j int) bool { return positiveInt(candidates[i]["id"]) < positiveInt(candidates[j]["id"]) })
	return candidates[0]
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

func (r *Readarr) prepareChaptarrBook(ctx context.Context, bookID int) (map[string]any, error) {
	var book map[string]any
	bookPath := fmt.Sprintf("/api/v1/book/%d", bookID)
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, bookPath, nil, nil, &book); err != nil {
		return nil, err
	}
	authorID := positiveInt(book["authorId"])
	if authorID <= 0 {
		return nil, fmt.Errorf("chaptarr book %d has no author id", bookID)
	}
	if err := r.enableChaptarrAuthorFormat(ctx, authorID); err != nil {
		return nil, err
	}
	var editions []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/edition", url.Values{"bookId": {fmt.Sprint(bookID)}}, nil, &editions); err != nil {
		return nil, err
	}
	format := r.chaptarrFormat()
	chosen := -1
	for i, edition := range editions {
		if strings.EqualFold(stringFromMap(edition, "format"), format) {
			chosen = i
			break
		}
	}
	if chosen < 0 {
		return nil, fmt.Errorf("chaptarr has no usable %s edition for %q", format, stringFromMap(book, "title"))
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
		return nil, err
	}
	var verifiedEditions []map[string]any
	if _, err := r.chaptarrJSON(ctx, http.MethodGet, "/api/v1/edition", url.Values{"bookId": {fmt.Sprint(bookID)}}, nil, &verifiedEditions); err != nil {
		return nil, err
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
		return nil, fmt.Errorf("chaptarr did not retain exactly one selected %s edition for %q", format, stringFromMap(book, "title"))
	}
	return book, nil
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
	if _, err := r.prepareChaptarrBook(ctx, bookID); err != nil {
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
	formatMonitored, _ := verified[format+"Monitored"].(bool)
	if !monitored || !formatMonitored {
		return payload, postResponse, fmt.Errorf("chaptarr did not retain %s monitoring for %q", format, title)
	}
	command := map[string]any{"name": "BookSearch", "bookIds": []int{bookID}}
	if _, err := r.chaptarrJSON(ctx, http.MethodPost, "/api/v1/command", nil, command, nil); err != nil {
		return payload, postResponse, err
	}
	response, _ := json.Marshal(verified)
	return payload, response, nil
}

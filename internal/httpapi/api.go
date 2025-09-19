package httpapi

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"gitea.knapp/jacoknapp/scriptorum/internal/util"
	"github.com/go-chi/chi/v5"
)

type RequestPayload struct {
	Title           string   `json:"title"`
	Authors         []string `json:"authors"`
	ISBN10          string   `json:"isbn10"`
	ISBN13          string   `json:"isbn13"`
	ASIN            string   `json:"asin"`
	Format          string   `json:"format"` // ebook | audiobook
	Provider        string   `json:"provider"`
	ProviderPayload string   `json:"provider_payload"`
}

func (s *Server) mountAPI(r chi.Router) {
	r.Route("/api/v1/requests", func(rr chi.Router) {
		rr.Post("/", s.requireLogin(s.apiCreateRequest))
		rr.Get("/", s.requireLogin(s.apiListRequests))
		rr.Post("/{id}/approve", s.requireAdmin(s.apiApproveRequest))
		rr.Post("/{id}/hydrate", s.requireAdmin(s.apiHydrateRequest))
		rr.Post("/{id}/decline", s.requireAdmin(s.apiDeclineRequest))
		rr.Delete("/{id}", s.requireAdmin(s.apiDeleteRequest))
		rr.Delete("/", s.requireAdmin(s.apiDeleteAllRequests))
		rr.Post("/approve-all", s.requireAdmin(s.apiApproveAllRequests))
	})
	// Book details endpoint used by UI to fetch richer metadata on-demand
	r.Route("/api/v1/book", func(br chi.Router) {
		br.Post("/details", s.apiBookDetails)
		br.Post("/enriched", s.apiBookEnriched)
	})
}

// apiBookDetails returns a normalized book details object.
// Input (JSON or form): any of provider_payload, provider_payload_ebook, provider_payload_audiobook,
// isbn13, isbn10, asin, title, authors
func (s *Server) apiBookDetails(w http.ResponseWriter, r *http.Request) {
	// Parse input (JSON preferred)
	var in map[string]any
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&in)
	} else {
		_ = r.ParseForm()
		in = map[string]any{}
		for k := range r.Form {
			if len(r.Form[k]) == 1 {
				in[k] = r.Form.Get(k)
			} else {
				// authors may be multiple
				in[k] = r.Form[k]
			}
		}
	}

	// Helper to write normalized response
	writeNormalized := func(obj map[string]any) {
		// Ensure keys: title, authors ([]string), isbn10, isbn13, asin, cover, description, provider_payload
		if obj == nil {
			writeJSON(w, map[string]any{"error": "no details found"}, 404)
			return
		}
		// normalize authors
		if a, ok := obj["authors"]; ok {
			switch t := a.(type) {
			case []string:
				// ok
			case []any:
				var out []string
				for _, v := range t {
					if s2, _ := v.(string); s2 != "" {
						out = append(out, s2)
					}
				}
				obj["authors"] = out
			case string:
				obj["authors"] = []string{t}
			}
		}
		writeJSON(w, obj, 200)
	}

	// If provider payload is present, try to parse it and extract fields
	pickPayload := ""
	if v, ok := in["provider_payload_ebook"].(string); ok && strings.TrimSpace(v) != "" {
		pickPayload = v
	}
	if pickPayload == "" {
		if v, ok := in["provider_payload_audiobook"].(string); ok && strings.TrimSpace(v) != "" {
			pickPayload = v
		}
	}
	if pickPayload == "" {
		if v, ok := in["provider_payload"].(string); ok && strings.TrimSpace(v) != "" {
			pickPayload = v
		}
	}
	if pickPayload != "" {
		var pp map[string]any
		if err := json.Unmarshal([]byte(pickPayload), &pp); err == nil {
			// Build normalized object
			out := map[string]any{}
			if t, ok := pp["title"].(string); ok {
				out["title"] = t
			}
			if d, ok := pp["description"].(string); ok {
				out["description"] = d
			}
			if d, ok := pp["overview"].(string); ok && out["description"] == nil {
				out["description"] = d
			}
			if im, ok := pp["images"].([]any); ok && out["cover"] == nil {
				for _, it := range im {
					if m, ok := it.(map[string]any); ok {
						if url, ok := m["remoteUrl"].(string); ok && url != "" {
							out["cover"] = url
							break
						}
						if url, ok := m["url"].(string); ok && url != "" {
							out["cover"] = url
							break
						}
					}
				}
			}
			if a, ok := pp["author"].(map[string]any); ok {
				if n, _ := a["name"].(string); n != "" {
					out["authors"] = []string{n}
				}
			}
			if a, ok := pp["authors"].([]any); ok {
				var outA []string
				for _, aa := range a {
					if s2, _ := aa.(string); s2 != "" {
						outA = append(outA, s2)
					}
				}
				if len(outA) > 0 {
					out["authors"] = outA
				}
			}
			if v, ok := in["isbn13"].(string); ok && v != "" {
				out["isbn13"] = v
			}
			if v, ok := in["isbn10"].(string); ok && v != "" {
				out["isbn10"] = v
			}
			if v, ok := in["asin"].(string); ok && v != "" {
				out["asin"] = v
			}
			out["provider_payload"] = pp
			writeNormalized(out)
			return
		}
	}

	// No provider payload: attempt Readarr lookup if identifiers or title present
	term := ""
	if v, ok := in["asin"].(string); ok && v != "" {
		term = v
	}
	if term == "" {
		if v, ok := in["isbn13"].(string); ok && v != "" {
			term = v
		}
	}
	if term == "" {
		if v, ok := in["isbn10"].(string); ok && v != "" {
			term = v
		}
	}
	if term == "" {
		if v, ok := in["title"].(string); ok && v != "" {
			term = v
		}
		if a, ok := in["authors"]; ok && term != "" {
			switch t := a.(type) {
			case []string:
				if len(t) > 0 && strings.TrimSpace(t[0]) != "" {
					term = term + " " + t[0]
				}
			case []any:
				if len(t) > 0 {
					if s2, _ := t[0].(string); s2 != "" {
						term = term + " " + s2
					}
				}
			case string:
				if t != "" {
					term = term + " " + t
				}
			}
		}
	}
	if term == "" {
		writeJSON(w, map[string]any{"error": "no query provided"}, 400)
		return
	}

	// Prefer Readarr if configured (ebooks first)
	cfg := s.settings.Get()
	var inst providers.ReadarrInstance
	if cfg != nil && strings.TrimSpace(cfg.Readarr.Ebooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Ebooks.APIKey) != "" {
		inst = providers.ReadarrInstance{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKey: cfg.Readarr.Ebooks.APIKey, DefaultQualityProfileID: cfg.Readarr.Ebooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Ebooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Ebooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Ebooks.InsecureSkipVerify}
	}
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		// No Readarr configured — return basic info from inputs
		out := map[string]any{"title": in["title"], "isbn13": in["isbn13"], "isbn10": in["isbn10"], "asin": in["asin"], "authors": in["authors"]}
		writeNormalized(out)
		return
	}

	ra := providers.NewReadarrWithDB(inst, s.db.SQL())
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := ra.LookupByTerm(ctx, term)
	if err != nil || len(list) == 0 {
		writeJSON(w, map[string]any{"error": "no matches from Readarr"}, 404)
		return
	}
	pick := list[0]
	// prefer exact title match
	for _, b := range list {
		if strings.EqualFold(strings.TrimSpace(b.Title), strings.TrimSpace(in["title"].(string))) && strings.TrimSpace(b.Title) != "" {
			pick = b
			break
		}
	}
	out := map[string]any{}
	out["title"] = pick.Title
	if pick.Author != nil {
		if n, _ := pick.Author["name"].(string); n != "" {
			out["authors"] = []string{n}
		}
	} else if len(pick.Authors) > 0 {
		var aa []string
		for _, a := range pick.Authors {
			if n, _ := a["name"].(string); n != "" {
				aa = append(aa, n)
			}
		}
		if len(aa) > 0 {
			out["authors"] = aa
		}
	}
	// Extract identifiers (ISBN10/ISBN13/ASIN) from Readarr lookup Identifiers
	var isbn10, isbn13, asin string
	for _, id := range pick.Identifiers {
		if id == nil {
			continue
		}
		if typ, ok := id["type"].(string); ok {
			if val, ok2 := id["value"].(string); ok2 && val != "" {
				switch strings.ToLower(strings.TrimSpace(typ)) {
				case "isbn_10", "isbn10", "isbn-10":
					if isbn10 == "" {
						isbn10 = val
					}
				case "isbn_13", "isbn13", "isbn-13":
					if isbn13 == "" {
						isbn13 = val
					}
				case "asin":
					if asin == "" {
						asin = val
					}
				}
			}
		} else {
			// fallback: some servers provide keys directly
			if v, ok := id["isbn10"].(string); ok && v != "" && isbn10 == "" {
				isbn10 = v
			}
			if v, ok := id["isbn13"].(string); ok && v != "" && isbn13 == "" {
				isbn13 = v
			}
			if v, ok := id["asin"].(string); ok && v != "" && asin == "" {
				asin = v
			}
		}
	}
	if isbn10 != "" {
		out["isbn10"] = isbn10
	}
	if isbn13 != "" {
		out["isbn13"] = isbn13
	}
	if asin != "" {
		out["asin"] = asin
	}
	// pick a cover if present
	if pick.CoverUrl != "" {
		out["cover"] = pick.CoverUrl
	}
	if pick.RemoteCover != "" && out["cover"] == nil {
		out["cover"] = pick.RemoteCover
	}
	// include raw provider payload for the client if desired
	// Attempt to marshal the pick into a generic map
	ppb, _ := json.Marshal(pick)
	var pp map[string]any
	_ = json.Unmarshal(ppb, &pp)
	out["provider_payload"] = pp
	writeNormalized(out)
}

// apiBookEnriched returns full Readarr book data directly for UI modals
func (s *Server) apiBookEnriched(w http.ResponseWriter, r *http.Request) {
	// Parse input to get book identifiers
	var in map[string]any
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		_ = json.NewDecoder(r.Body).Decode(&in)
	} else {
		_ = r.ParseForm()
		in = map[string]any{}
		for k := range r.Form {
			if len(r.Form[k]) == 1 {
				in[k] = r.Form.Get(k)
			} else {
				in[k] = r.Form[k]
			}
		}
	}

	// Build search term from available identifiers
	term := ""
	if v, ok := in["asin"].(string); ok && v != "" {
		term = v
	}
	if term == "" {
		if v, ok := in["isbn13"].(string); ok && v != "" {
			term = v
		}
	}
	if term == "" {
		if v, ok := in["isbn10"].(string); ok && v != "" {
			term = v
		}
	}
	if term == "" {
		if v, ok := in["title"].(string); ok && v != "" {
			term = v
			// Add author to improve search accuracy
			if a, ok := in["authors"]; ok {
				switch t := a.(type) {
				case []string:
					if len(t) > 0 && strings.TrimSpace(t[0]) != "" {
						term = term + " " + t[0]
					}
				case []any:
					if len(t) > 0 {
						if s2, _ := t[0].(string); s2 != "" {
							term = term + " " + s2
						}
					}
				case string:
					if t != "" {
						term = term + " " + t
					}
				}
			}
		}
	}
	if term == "" {
		writeJSON(w, map[string]any{"error": "no query provided"}, 400)
		return
	}

	// Get Readarr configuration
	cfg := s.settings.Get()
	var inst providers.ReadarrInstance
	if cfg != nil && strings.TrimSpace(cfg.Readarr.Ebooks.BaseURL) != "" && strings.TrimSpace(cfg.Readarr.Ebooks.APIKey) != "" {
		inst = providers.ReadarrInstance{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKey: cfg.Readarr.Ebooks.APIKey, DefaultQualityProfileID: cfg.Readarr.Ebooks.DefaultQualityProfileID, DefaultRootFolderPath: cfg.Readarr.Ebooks.DefaultRootFolderPath, DefaultTags: cfg.Readarr.Ebooks.DefaultTags, InsecureSkipVerify: cfg.Readarr.Ebooks.InsecureSkipVerify}
	}
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		writeJSON(w, map[string]any{"error": "Readarr not configured"}, 500)
		return
	}

	// Query Readarr
	ra := providers.NewReadarrWithDB(inst, s.db.SQL())
	ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
	defer cancel()
	list, err := ra.LookupByTerm(ctx, term)
	if err != nil || len(list) == 0 {
		writeJSON(w, map[string]any{"error": "no matches from Readarr", "term": term}, 404)
		return
	}

	// Find best match - prefer exact title match
	pick := list[0]
	if titleStr, ok := in["title"].(string); ok && titleStr != "" {
		for _, b := range list {
			if strings.EqualFold(strings.TrimSpace(b.Title), strings.TrimSpace(titleStr)) {
				pick = b
				break
			}
		}
	}

	// Convert Readarr book to map and return directly
	bookData, err := json.Marshal(pick)
	if err != nil {
		writeJSON(w, map[string]any{"error": "failed to serialize book data"}, 500)
		return
	}

	var result map[string]any
	if err := json.Unmarshal(bookData, &result); err != nil {
		writeJSON(w, map[string]any{"error": "failed to parse book data"}, 500)
		return
	}

	// If we have a book ID, try to get detailed book information including description
	if pick.ID > 0 {
		if details, err := ra.GetBookDetails(ctx, pick.ID); err == nil {
			// Merge details into result, giving priority to detailed information
			for key, value := range details {
				// Don't overwrite key fields from lookup, but add new ones
				if _, exists := result[key]; !exists || key == "description" || key == "overview" || key == "synopsis" || key == "summary" {
					result[key] = value
				}
			}
		}
	}

	// Add normalized author field for easier frontend access
	// Try multiple author sources: Author object, Authors array, or AuthorTitle string
	var authorNames []string
	if pick.Author != nil {
		if name, ok := pick.Author["name"].(string); ok && name != "" {
			authorNames = append(authorNames, name)
		}
	}
	if len(authorNames) == 0 && len(pick.Authors) > 0 {
		for _, a := range pick.Authors {
			if name, ok := a["name"].(string); ok && name != "" {
				authorNames = append(authorNames, name)
			}
		}
	}
	// If still no authors, try to extract from AuthorTitle field
	if len(authorNames) == 0 && pick.AuthorTitle != "" {
		// AuthorTitle is often in format "lastname, firstname BookTitle"
		// Try to extract just the author part before the book title
		authorTitle := pick.AuthorTitle
		if pick.Title != "" {
			// Remove the book title from the end if present
			authorTitle = strings.TrimSuffix(authorTitle, " "+pick.Title)
		}
		// Convert "lastname, firstname" to "firstname lastname"
		if strings.Contains(authorTitle, ",") {
			parts := strings.SplitN(authorTitle, ",", 2)
			if len(parts) == 2 {
				lastname := strings.TrimSpace(parts[0])
				firstname := strings.TrimSpace(parts[1])
				if firstname != "" && lastname != "" {
					authorNames = append(authorNames, firstname+" "+lastname)
				}
			}
		} else {
			// Use as-is if no comma
			authorTitle = strings.TrimSpace(authorTitle)
			if authorTitle != "" {
				authorNames = append(authorNames, authorTitle)
			}
		}
	}

	if len(authorNames) > 0 {
		result["authors"] = authorNames
		result["author"] = strings.Join(authorNames, ", ")
	}

	writeJSON(w, result, 200)
}

func (s *Server) apiCreateRequest(w http.ResponseWriter, r *http.Request) {
	var p RequestPayload
	ct := r.Header.Get("Content-Type")
	if strings.Contains(ct, "application/json") {
		if err := json.NewDecoder(r.Body).Decode(&p); err == nil {
			// ok
		} else {
			// Fall back to form parsing if body isn't actually JSON
			_ = r.ParseForm()
			p.Title = strings.TrimSpace(r.FormValue("title"))
			if a := r.Form["authors"]; len(a) > 0 {
				p.Authors = a
			} else if s := strings.TrimSpace(r.FormValue("authors")); s != "" {
				p.Authors = []string{s}
			}
			p.ISBN10 = strings.TrimSpace(r.FormValue("isbn10"))
			p.ISBN13 = strings.TrimSpace(r.FormValue("isbn13"))
			p.ASIN = strings.TrimSpace(r.FormValue("asin"))
			p.Format = strings.TrimSpace(r.FormValue("format"))
			p.Provider = strings.TrimSpace(r.FormValue("provider"))
			p.ProviderPayload = strings.TrimSpace(r.FormValue("provider_payload"))
		}
	} else {
		// Fallback: parse form-encoded body
		_ = r.ParseForm()
		p.Title = strings.TrimSpace(r.FormValue("title"))
		if a := r.Form["authors"]; len(a) > 0 {
			p.Authors = a
		} else if s := strings.TrimSpace(r.FormValue("authors")); s != "" {
			p.Authors = []string{s}
		}
		p.ISBN10 = strings.TrimSpace(r.FormValue("isbn10"))
		p.ISBN13 = strings.TrimSpace(r.FormValue("isbn13"))
		p.ASIN = strings.TrimSpace(r.FormValue("asin"))
		p.Format = strings.TrimSpace(r.FormValue("format"))
		p.Provider = strings.TrimSpace(r.FormValue("provider"))
		p.ProviderPayload = strings.TrimSpace(r.FormValue("provider_payload"))
	}
	if p.Title == "" && p.ISBN13 == "" && p.ISBN10 == "" && p.ASIN == "" {
		http.Error(w, "title or identifier required", 400)
		return
	}
	format := strings.ToLower(p.Format)
	if format != "ebook" && format != "audiobook" {
		format = "ebook"
	}

	u := r.Context().Value(ctxUser).(*session)
	req := &db.Request{
		// store username in requester_email for backward-compatible storage
		RequesterEmail: strings.ToLower(u.Username),
		Title:          p.Title, Authors: p.Authors, ISBN10: p.ISBN10, ISBN13: p.ISBN13,
		Format: format, Status: "pending",
	}
	// Stash provider payload on request so approval can use it.
	// If missing, try to attach by looking it up from Readarr now.
	if strings.TrimSpace(p.ProviderPayload) != "" {
		req.ReadarrReq = json.RawMessage([]byte(p.ProviderPayload))
	} else {
		// Attempt server-side attach for convenience/fallback
		// Pick instance based on format
		var inst providers.ReadarrInstance
		if format == "audiobook" {
			c := s.settings.Get().Readarr.Audiobooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
		} else {
			c := s.settings.Get().Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
		}
		if strings.TrimSpace(inst.BaseURL) != "" && strings.TrimSpace(inst.APIKey) != "" {
			ra := providers.NewReadarrWithDB(inst, s.db.SQL())
			term := util.FirstNonEmpty(p.ASIN, p.ISBN13, p.ISBN10)
			if term == "" {
				term = strings.TrimSpace(p.Title)
				if len(p.Authors) > 0 && strings.TrimSpace(p.Authors[0]) != "" {
					term = term + " " + strings.TrimSpace(p.Authors[0])
				}
			}
			if term != "" {
				ctx, cancel := context.WithTimeout(r.Context(), 10*time.Second)
				defer cancel()
				if list, err := ra.LookupByTerm(ctx, term); err == nil && len(list) > 0 {
					pick := list[0]
					for _, b := range list {
						titleOK := strings.EqualFold(strings.TrimSpace(b.Title), strings.TrimSpace(p.Title)) && strings.TrimSpace(b.Title) != ""
						authorOK := false
						if len(p.Authors) > 0 {
							want := strings.TrimSpace(p.Authors[0])
							if b.Author != nil {
								if n, _ := b.Author["name"].(string); n != "" && strings.EqualFold(strings.TrimSpace(n), want) {
									authorOK = true
								}
							} else if len(b.Authors) > 0 {
								if n, _ := b.Authors[0]["name"].(string); n != "" && strings.EqualFold(strings.TrimSpace(n), want) {
									authorOK = true
								}
							} else if b.AuthorTitle != "" {
								if strings.Contains(strings.ToLower(b.AuthorTitle), strings.ToLower(strings.ReplaceAll(want, " ", ""))) {
									authorOK = true
								}
							}
						}
						if titleOK && authorOK {
							pick = b
							break
						}
					}
					var author map[string]any
					if pick.Author != nil {
						author = pick.Author
					}
					if author == nil && len(pick.Authors) > 0 {
						author = pick.Authors[0]
					}
					if author == nil && pick.AuthorId > 0 {
						author = map[string]any{"id": pick.AuthorId}
					}
					if author == nil && pick.AuthorTitle != "" {
						author = map[string]any{"name": parseAuthorNameFromTitle(pick.AuthorTitle)}
					}
					cand := map[string]any{
						"title":     pick.Title,
						"titleSlug": pick.TitleSlug,
						"author":    author,
						// include one monitored edition to pin selection
						"editions":         []any{map[string]any{"foreignEditionId": pick.ForeignEditionId, "monitored": true}},
						"foreignBookId":    pick.ForeignBookId,
						"foreignEditionId": pick.ForeignEditionId,
						// provider will backfill remaining defaults if missing
						"monitored":         true,
						"metadataProfileId": 1,
					}
					if b, err := json.Marshal(cand); err == nil {
						req.ReadarrReq = json.RawMessage(b)
					}
				}
			}
		}
	}
	id, err := s.db.CreateRequest(r.Context(), req)
	if err != nil {
		http.Error(w, "db: "+err.Error(), 500)
		return
	}

	// Send notification for new request
	s.SendRequestNotification(id, u.Username, p.Title, p.Authors)

	// If HTMX, return a tiny HTML notice instead of JSON
	if strings.Contains(r.Header.Get("HX-Request"), "true") || r.Header.Get("HX-Request") == "true" {
		// let the client know a request was created so UI can refresh if needed
		w.Header().Set("HX-Trigger", `{"request:created": {"id": `+strconv.FormatInt(id, 10)+`}}`)
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		w.WriteHeader(201)
		w.Write([]byte(`<li class="p-3 bg-emerald-50 text-emerald-700 rounded mb-2">Request submitted (ID ` + strconv.FormatInt(id, 10) + `). <a class="underline text-emerald-800" href="/requests">View in Requests</a></li>`))
		return
	}
	// Non-HTMX: prefer redirect back to the referrer if it's a browser form post
	if r.Method == http.MethodPost && strings.Contains(r.Header.Get("Content-Type"), "application/x-www-form-urlencoded") {
		ref := r.Header.Get("Referer")
		if strings.TrimSpace(ref) == "" {
			ref = "/search"
		}
		http.Redirect(w, r, ref, http.StatusSeeOther)
		return
	}
	writeJSON(w, map[string]any{"id": id, "status": "pending"}, 201)
}

func (s *Server) apiListRequests(w http.ResponseWriter, r *http.Request) {
	u := r.Context().Value(ctxUser).(*session)
	items, err := s.db.ListRequests(r.Context(), u.Username, 200)
	if err != nil {
		http.Error(w, "db: "+err.Error(), 500)
		return
	}
	writeJSON(w, items, 200)
}

func (s *Server) apiApproveRequest(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	req, err := s.db.GetRequest(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	var inst providers.ReadarrInstance
	if req.Format == "audiobook" {
		c := s.settings.Get().Readarr.Audiobooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	}
	// If Readarr not configured, approve without sending
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
		_ = s.db.UpdateRequestStatus(r.Context(), id, "approved", "approved (no Readarr configured)", r.Context().Value(ctxUser).(*session).Username, nil, nil)

		// Send notification for approved request asynchronously
		go s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

		w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
		writeJSON(w, map[string]string{"status": "approved"}, 200)
		return
	}

	// For Readarr-enabled approvals, first update the UI immediately then process async
	username := r.Context().Value(ctxUser).(*session).Username
	_ = s.db.UpdateRequestStatus(r.Context(), id, "processing", "approval in progress", username, nil, nil)

	// Send immediate response to unblock the UI
	w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
	writeJSON(w, map[string]string{"status": "processing"}, 200)

	// Process approval asynchronously
	go s.processAsyncApproval(id, req, inst, username)
}

// processAsyncApproval handles the long-running approval process asynchronously
func (s *Server) processAsyncApproval(id int64, req *db.Request, inst providers.ReadarrInstance, username string) {
	ctx := context.Background()
	ra := providers.NewReadarrWithDB(inst, s.db.SQL())

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// Require an exact selection payload saved at request-time
	if len(req.ReadarrReq) == 0 {
		_ = s.db.UpdateRequestStatus(ctx, id, "error", "request has no stored selection payload; please re-request from search", username, nil, nil)
		return
	}

	var cand map[string]any
	if err := json.Unmarshal(req.ReadarrReq, &cand); err != nil || cand == nil {
		_ = s.db.UpdateRequestStatus(ctx, id, "error", "invalid stored selection payload", username, nil, nil)
		return
	}

	// Ensure candidate has an author id. If missing, try to resolve by name
	if a, ok := cand["author"].(map[string]any); ok {
		if _, hasID := a["id"]; !hasID {
			// try to find by name
			var name string
			if n, _ := a["name"].(string); n != "" {
				name = n
			} else if n, _ := cand["title"].(string); n != "" {
				name = n
			}
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Author missing id, trying to resolve name='%s'\n", name)
			}
			if name != "" {
				if aid, err := ra.FindAuthorIDByName(reqCtx, name); err == nil && aid != 0 {
					a["id"] = aid
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Found author id %d for name '%s'\n", aid, name)
					}
				} else {
					if s.settings.Get().Debug {
						if err == nil {
							fmt.Printf("DEBUG: Author not found for name '%s' (will not create)\n", name)
						} else {
							fmt.Printf("DEBUG: Error finding author for name '%s': %v\n", name, err)
						}
					}
				}
			}
			cand["author"] = a
		}
	}

	// Try to add the book to Readarr
	var payload []byte
	var respBody []byte
	var err error

	// If the stored payload looks like a full Readarr Book schema, send it as-is
	if len(req.ReadarrReq) > 0 {
		var raw map[string]any
		if json.Unmarshal(req.ReadarrReq, &raw) == nil {
			// Heuristic: treat as full schema if it contains indicators
			if _, ok := raw["authorTitle"]; ok || raw["author"] != nil || raw["editions"] != nil || raw["addOptions"] != nil {
				payload, respBody, err = ra.AddBookRaw(reqCtx, req.ReadarrReq)
			}
		}
	}

	// Fallback to templated add if raw wasn't used
	if payload == nil && err == nil {
		payload, respBody, err = ra.AddBook(reqCtx, cand, providers.AddOpts{
			QualityProfileID: inst.DefaultQualityProfileID,
			RootFolderPath:   inst.DefaultRootFolderPath,
			SearchForMissing: true,
			Tags:             inst.DefaultTags,
		})
	}

	// Debug logging
	if s.settings.Get().Debug && payload != nil {
		fmt.Printf("DEBUG: Readarr add sent payload:\n%s\n", string(payload))
		if respBody != nil {
			fmt.Printf("DEBUG: Readarr add returned body:\n%s\n", string(respBody))
		}
	}

	if err != nil {
		// Handle duplicate book error
		emsg := strings.ToLower(err.Error())
		if strings.Contains(emsg, "ix_editions_foreigneditionid") || strings.Contains(emsg, "duplicate key value") || strings.Contains(emsg, "already exists") {
			// Try to monitor existing book
			if payload != nil {
				if bid, gotBody, gerr := ra.GetBookByAddPayload(reqCtx, payload); gerr == nil && bid > 0 {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Duplicate detected; GET existing book with same payload returned (id=%d):\n%s\n", bid, string(gotBody))
					}
					if monBody, merr := ra.MonitorBooks(reqCtx, []int{bid}, true); merr == nil {
						if s.settings.Get().Debug {
							mb, _ := json.Marshal(map[string]any{"bookIds": []int{bid}, "monitored": true})
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor sent payload:\n%s\n", string(mb))
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor returned body:\n%s\n", string(monBody))
						}
						_ = s.db.ApproveRequest(ctx, id, username)
						_ = s.db.UpdateRequestStatus(ctx, id, "queued", fmt.Sprintf("already in Readarr; monitoring enabled for id %d", bid), username, payload, respBody)
						go s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)
						return
					}
				}
			}
			// Fallback: treat as already present without monitor update
			_ = s.db.ApproveRequest(ctx, id, username)
			_ = s.db.UpdateRequestStatus(ctx, id, "queued", "already in Readarr (duplicate edition)", username, payload, respBody)
			go s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)
			return
		}

		_ = s.db.UpdateRequestStatus(ctx, id, "error", err.Error(), "system", payload, respBody)
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Readarr add error: %v\n---payload---\n%s\n---response---\n%s\n", err, string(payload), string(respBody))
		}
		return
	}

	// Success: book added to Readarr
	_ = s.db.ApproveRequest(ctx, id, username)
	_ = s.db.UpdateRequestStatus(ctx, id, "queued", "sent to Readarr", username, payload, respBody)

	// Start background monitoring task for successful additions
	if respBody != nil {
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Readarr add response body for monitoring:\n%s\n", string(respBody))
		}
		var rb map[string]any
		if json.Unmarshal(respBody, &rb) == nil {
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Parsed response structure: %+v\n", rb)
			}
			if v, ok := rb["id"]; ok {
				var bid int
				switch t := v.(type) {
				case float64:
					bid = int(t)
				case int:
					bid = t
				case int64:
					bid = int(t)
				}
				if bid > 0 {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Starting background monitor for book ID: %d\n", bid)
					}
					go s.backgroundMonitorBook(ra, bid)
				} else {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Book ID is 0 or invalid: %v (type: %T)\n", v, v)
					}
				}
			} else {
				if s.settings.Get().Debug {
					keys := make([]string, 0, len(rb))
					for k := range rb {
						keys = append(keys, k)
					}
					fmt.Printf("DEBUG: No 'id' field found in response. Available fields: %v\n", keys)
				}
			}
		} else {
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Failed to parse response body as JSON\n")
			}
		}
	} else {
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: No response body from Readarr add operation\n")
		}
	}

	// Send notification for approved request
	go s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

	// Trigger UI update via server-sent events or websockets would be ideal,
	// but for now we'll rely on the existing periodic refresh mechanisms
}

// backgroundMonitorBook ensures a newly added book stays monitored
func (s *Server) backgroundMonitorBook(ra *providers.Readarr, bookID int) {
	if s.settings.Get().Debug {
		fmt.Printf("DEBUG: backgroundMonitorBook started for book ID: %d\n", bookID)
	}

	bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	ticker := time.NewTicker(30 * time.Second)
	defer ticker.Stop()

	attempts := 0
	maxAttempts := 10 // 5 minutes / 30 seconds = 10 attempts

	sendMonitor := func() {
		attempts++
		perCtx, pCancel := context.WithTimeout(bgCtx, 12*time.Second)
		defer pCancel()

		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: MonitorBooks attempt %d/%d for book ID: %d\n", attempts, maxAttempts, bookID)
		}

		if monBody, merr := ra.MonitorBooks(perCtx, []int{bookID}, true); merr == nil {
			if s.settings.Get().Debug {
				mb, _ := json.Marshal(map[string]any{"bookIds": []int{bookID}, "monitored": true})
				fmt.Printf("DEBUG: PUT /api/v1/book/monitor sent payload:\n%s\n", string(mb))
				fmt.Printf("DEBUG: PUT /api/v1/book/monitor returned body:\n%s\n", string(monBody))
			}
		} else if s.settings.Get().Debug {
			fmt.Printf("DEBUG: MonitorBooks attempt for id=%d failed: %v\n", bookID, merr)
		}
	}

	// First immediate attempt
	sendMonitor()
	for {
		select {
		case <-bgCtx.Done():
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: backgroundMonitorBook finished for book ID: %d after %d attempts\n", bookID, attempts)
			}
			return
		case <-ticker.C:
			if attempts >= maxAttempts {
				if s.settings.Get().Debug {
					fmt.Printf("DEBUG: backgroundMonitorBook reached max attempts (%d) for book ID: %d\n", maxAttempts, bookID)
				}
				return
			}
			sendMonitor()
		}
	}
}

func (s *Server) apiDeclineRequest(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	_, err := s.db.GetRequest(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	err = s.db.DeclineRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
	if err != nil {
		http.Error(w, "failed to decline request", 500)
		return
	}

	w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
	writeJSON(w, map[string]string{"status": "declined"}, 200)
}

func (s *Server) apiDeleteRequest(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	_, err := s.db.GetRequest(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}

	err = s.db.DeleteRequest(r.Context(), id)
	if err != nil {
		http.Error(w, "failed to delete request", 500)
		return
	}

	w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
	writeJSON(w, map[string]string{"status": "deleted"}, 200)
}

func (s *Server) apiDeleteAllRequests(w http.ResponseWriter, r *http.Request) {
	err := s.db.DeleteAllRequests(r.Context())
	if err != nil {
		http.Error(w, "failed to delete all requests", 500)
		return
	}

	w.Header().Set("HX-Trigger", `{"request:updated": {}}`)
	writeJSON(w, map[string]string{"status": "all requests deleted"}, 200)
}

func (s *Server) apiApproveAllRequests(w http.ResponseWriter, r *http.Request) {
	// Get all pending requests
	allRequests, err := s.db.ListRequests(r.Context(), "", 1000) // Get up to 1000 requests
	if err != nil {
		http.Error(w, "failed to fetch requests", 500)
		return
	}

	// Filter to only pending requests and collect their details for notifications
	var pendingRequests []db.Request
	for _, req := range allRequests {
		if req.Status == "pending" {
			pendingRequests = append(pendingRequests, req)
		}
	}

	if len(pendingRequests) == 0 {
		writeJSON(w, map[string]string{"status": "no pending requests to approve"}, 200)
		return
	}

	// Approve each pending request by updating status to "queued"
	username := r.Context().Value(ctxUser).(*session).Username
	for _, req := range pendingRequests {
		err := s.db.UpdateRequestStatus(r.Context(), req.ID, "queued", "bulk approved", username, nil, nil)
		if err != nil {
			http.Error(w, fmt.Sprintf("failed to approve request %d", req.ID), 500)
			return
		}

		// Send notification for each approved request
		s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)
	}

	w.Header().Set("HX-Trigger", `{"request:updated": {}}`)
	writeJSON(w, map[string]string{"status": fmt.Sprintf("approved %d requests", len(pendingRequests))}, 200)
}

// apiHydrateRequest tries to populate the stored selection payload for a request by
// performing a Readarr lookup using the request's identifiers or title/author.
// This is useful for older requests created before selection payloads were stored.
func (s *Server) apiHydrateRequest(w http.ResponseWriter, r *http.Request) {
	idStr := chi.URLParam(r, "id")
	id, _ := strconv.ParseInt(idStr, 10, 64)

	req, err := s.db.GetRequest(r.Context(), id)
	if err != nil {
		http.Error(w, "not found", 404)
		return
	}
	// If already has payload, nothing to do
	if len(req.ReadarrReq) > 0 {
		w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
		writeJSON(w, map[string]any{"status": "ok", "message": "already attached"}, 200)
		return
	}

	// Pick instance based on format
	var inst providers.ReadarrInstance
	if strings.ToLower(req.Format) == "audiobook" {
		c := s.settings.Get().Readarr.Audiobooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	}
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		http.Error(w, "readarr not configured", http.StatusBadRequest)
		return
	}

	ra := providers.NewReadarrWithDB(inst, s.db.SQL())

	// Build lookup term preference: ISBN13 > ISBN10 > Title [Author]
	term := util.FirstNonEmpty(req.ISBN13, req.ISBN10)
	if term == "" {
		term = strings.TrimSpace(req.Title)
		if len(req.Authors) > 0 && strings.TrimSpace(req.Authors[0]) != "" {
			term = term + " " + strings.TrimSpace(req.Authors[0])
		}
	}
	if term == "" {
		http.Error(w, "no identifiers or title to search", http.StatusBadRequest)
		return
	}

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()
	list, err := ra.LookupByTerm(ctx, term)
	if err != nil || len(list) == 0 {
		http.Error(w, "no matches from Readarr", http.StatusBadRequest)
		return
	}
	// Choose best match: prefer title match and author match, else first
	pick := list[0]
	for _, b := range list {
		titleOK := strings.EqualFold(strings.TrimSpace(b.Title), strings.TrimSpace(req.Title)) && strings.TrimSpace(b.Title) != ""
		authorOK := false
		if len(req.Authors) > 0 {
			want := strings.TrimSpace(req.Authors[0])
			if b.Author != nil {
				if n, _ := b.Author["name"].(string); n != "" && strings.EqualFold(strings.TrimSpace(n), want) {
					authorOK = true
				}
			} else if len(b.Authors) > 0 {
				if n, _ := b.Authors[0]["name"].(string); n != "" && strings.EqualFold(strings.TrimSpace(n), want) {
					authorOK = true
				}
			} else if b.AuthorTitle != "" {
				if strings.Contains(strings.ToLower(b.AuthorTitle), strings.ToLower(strings.ReplaceAll(want, " ", ""))) {
					authorOK = true
				}
			}
		}
		if titleOK && authorOK {
			pick = b
			break
		}
	}

	// Build candidate payload similar to search.go
	var author map[string]any
	if pick.Author != nil {
		author = pick.Author
	} else if len(pick.Authors) > 0 {
		author = pick.Authors[0]
	} else if pick.AuthorId > 0 {
		author = map[string]any{"id": pick.AuthorId}
	} else if pick.AuthorTitle != "" {
		author = map[string]any{"name": parseAuthorNameFromTitle(pick.AuthorTitle)}
	}
	cand := map[string]any{
		"title":            pick.Title,
		"titleSlug":        pick.TitleSlug,
		"author":           author,
		"editions":         []any{},
		"foreignBookId":    pick.ForeignBookId,
		"foreignEditionId": pick.ForeignEditionId,
	}
	cjson, _ := json.Marshal(cand)

	// Save to DB
	if err := s.db.UpdateRequestStatus(r.Context(), id, req.Status, "hydrated", s.userEmail(r), cjson, nil); err != nil {
		http.Error(w, "db: "+err.Error(), 500)
		return
	}
	w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
	writeJSON(w, map[string]any{"status": "ok"}, 200)
}

// parseAuthorNameFromTitle extracts author name from authorTitle like "andrews, ilona Burn for Me"
func parseAuthorNameFromTitle(title string) string {
	parts := strings.Split(strings.TrimSpace(title), " ")
	if len(parts) >= 2 {
		// Assume "lastname, firstname ..."
		last := strings.Trim(parts[0], ",")
		first := parts[1]
		return util.ToTitleCase(first + " " + last)
	}
	return util.ToTitleCase(strings.TrimSpace(title))
}

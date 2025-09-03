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
	"github.com/go-chi/chi/v5"
)

type RequestPayload struct {
	Title   string   `json:"title"`
	Authors []string `json:"authors"`
	ISBN10  string   `json:"isbn10"`
	ISBN13  string   `json:"isbn13"`
	ASIN    string   `json:"asin"`
	Format  string   `json:"format"` // ebook | audiobook
}

func (s *Server) mountAPI(r chi.Router) {
	r.Route("/api/v1/requests", func(rr chi.Router) {
		rr.Post("/", s.requireLogin(s.apiCreateRequest))
		rr.Get("/", s.requireLogin(s.apiListRequests))
		rr.Post("/{id}/approve", s.requireAdmin(s.apiApproveRequest))
		rr.Post("/{id}/decline", s.requireAdmin(s.apiDeclineRequest))
	})
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
		RequesterEmail: strings.ToLower(u.Email),
		Title:          p.Title, Authors: p.Authors, ISBN10: p.ISBN10, ISBN13: p.ISBN13,
		Format: format, Status: "pending",
	}
	id, err := s.db.CreateRequest(r.Context(), req)
	if err != nil {
		http.Error(w, "db: "+err.Error(), 500)
		return
	}
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
	items, err := s.db.ListRequests(r.Context(), u.Email, 200)
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
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags}
	}
	// If Readarr not configured, approve without sending
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Email)
		_ = s.db.UpdateRequestStatus(r.Context(), id, "approved", "approved (no Readarr configured)", r.Context().Value(ctxUser).(*session).Email, nil, nil)
		w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
		writeJSON(w, map[string]string{"status": "approved"}, 200)
		return
	}
	ra := providers.NewReadarrWithDB(inst, s.db.SQL())

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	// Auto-enrich via Amazon public if identifiers are missing but an ASIN can be derived.
	asin := providers.ExtractASINFromInput(firstNonEmpty("", req.Title)) // title may contain pasted URL
	if (strings.TrimSpace(req.ISBN13) == "" && strings.TrimSpace(req.ISBN10) == "") && asin != "" {
		ap := providers.NewAmazonPublic("www.amazon.com")
		if pb, err := ap.GetByASIN(ctx, asin); err == nil && pb != nil {
			if req.ISBN13 == "" {
				req.ISBN13 = pb.ISBN13
			}
			if req.ISBN10 == "" {
				req.ISBN10 = pb.ISBN10
			}
			if req.Title == "" {
				req.Title = pb.Title
			}
		}
	}

	term := firstNonEmpty(req.ISBN13, req.ISBN10, req.Title)
	res, err := ra.LookupByTerm(ctx, term)
	if err != nil {
		http.Error(w, "readarr lookup: "+err.Error(), 502)
		return
	}
	// Debug: log lookup results
	fmt.Printf("DEBUG: Readarr lookup for term '%s' returned %d results\n", term, len(res))
	for i, b := range res {
		fmt.Printf("DEBUG: Result %d: title='%s', authorId=%d, authorTitle='%s', author=%v, authors=%v\n", i, b.Title, b.AuthorId, b.AuthorTitle, b.Author, b.Authors)
	}
	cand, ok := ra.SelectCandidate(res, req.ISBN13, req.ISBN10, asin)
	if !ok {
		if len(res) > 0 {
			validCount := 0
			for _, b := range res {
				if b.Author != nil {
					if _, hasID := b.Author["id"]; hasID {
						validCount++
					}
				} else if b.AuthorId > 0 {
					validCount++
				} else if b.AuthorTitle != "" {
					validCount++
				}
			}
			if validCount > 0 {
				// Pick the first valid one
				for _, b := range res {
					if b.Author != nil {
						if _, hasID := b.Author["id"]; hasID {
							cand = map[string]any{"title": b.Title, "titleSlug": b.TitleSlug, "author": b.Author, "editions": b.Editions}
							break
						}
					} else if b.AuthorId > 0 {
						cand = map[string]any{"title": b.Title, "titleSlug": b.TitleSlug, "author": map[string]any{"id": b.AuthorId}, "editions": b.Editions}
						break
					} else if b.AuthorTitle != "" {
						name := parseAuthorNameFromTitle(b.AuthorTitle)
						cand = map[string]any{"title": b.Title, "titleSlug": b.TitleSlug, "author": map[string]any{"name": name}, "editions": b.Editions}
						break
					}
				}
			} else {
				http.Error(w, fmt.Sprintf("no valid candidate found: %d results, all missing author id", len(res)), 404)
				return
			}
		} else {
			http.Error(w, "no candidates found in Readarr lookup", 404)
			return
		}
	}

	// Ensure candidate has an author id. If missing, try to resolve by name and create if necessary.
	if a, ok := cand["author"].(map[string]any); ok {
		if _, hasID := a["id"]; !hasID {
			// try to find by name
			var name string
			if n, _ := a["name"].(string); n != "" {
				name = n
			} else if n, _ := cand["title"].(string); n != "" {
				name = n
			}
			fmt.Printf("DEBUG: Author missing id, trying to resolve name='%s'\n", name)
			if name != "" {
				if aid, err := ra.FindAuthorIDByName(ctx, name); err == nil && aid != 0 {
					a["id"] = aid
					fmt.Printf("DEBUG: Found author id %d for name '%s'\n", aid, name)
				} else if err == nil {
					// not found, try to create
					fmt.Printf("DEBUG: Author not found, trying to create for name '%s'\n", name)
					if aid2, cerr := ra.CreateAuthor(ctx, name); cerr == nil && aid2 != 0 {
						a["id"] = aid2
						fmt.Printf("DEBUG: Created author id %d for name '%s'\n", aid2, name)
					} else {
						fmt.Printf("DEBUG: Failed to create author for name '%s': %v\n", name, cerr)
					}
				} else {
					fmt.Printf("DEBUG: Error finding author for name '%s': %v\n", name, err)
				}
			}
			cand["author"] = a
		}
	}

	payload, respBody, err := ra.AddBook(ctx, cand, providers.AddOpts{
		QualityProfileID: inst.DefaultQualityProfileID,
		RootFolderPath:   inst.DefaultRootFolderPath,
		SearchForMissing: true,
		Tags:             inst.DefaultTags,
	})
	if err != nil {
		_ = s.db.UpdateRequestStatus(r.Context(), id, "error", err.Error(), "system", payload, respBody)
		http.Error(w, "readarr add: "+err.Error(), 502)
		return
	}

	_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Email)
	_ = s.db.UpdateRequestStatus(r.Context(), id, "queued", "sent to Readarr", r.Context().Value(ctxUser).(*session).Email, payload, respBody)
	// Include minimal identifiers so the UI can update any matching search results
	trig := map[string]any{"request:updated": map[string]any{
		"id":     id,
		"isbn13": req.ISBN13,
		"isbn10": req.ISBN10,
		"asin":   asin,
		"title":  req.Title,
		"format": req.Format,
	}}
	if b, err := json.Marshal(trig); err == nil {
		w.Header().Set("HX-Trigger", string(b))
	} else {
		w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
	}
	writeJSON(w, map[string]string{"status": "queued"}, 200)
}

func (s *Server) apiDeclineRequest(w http.ResponseWriter, r *http.Request) {
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

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

// parseAuthorNameFromTitle extracts author name from authorTitle like "andrews, ilona Burn for Me"
func parseAuthorNameFromTitle(title string) string {
	parts := strings.Split(strings.TrimSpace(title), " ")
	if len(parts) >= 2 {
		// Assume "lastname, firstname ..."
		last := strings.Trim(parts[0], ",")
		first := parts[1]
		return strings.Title(strings.ToLower(first + " " + last))
	}
	return strings.Title(strings.ToLower(strings.TrimSpace(title)))
}

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
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
		} else {
			c := s.settings.Get().Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
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
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	}
	// If Readarr not configured, approve without sending
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
		_ = s.db.UpdateRequestStatus(r.Context(), id, "approved", "approved (no Readarr configured)", r.Context().Value(ctxUser).(*session).Username, nil, nil)
		w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
		writeJSON(w, map[string]string{"status": "approved"}, 200)
		return
	}
	ra := providers.NewReadarrWithDB(inst, s.db.SQL())

	ctx, cancel := context.WithTimeout(r.Context(), 12*time.Second)
	defer cancel()

	// Require an exact selection payload saved at request-time; don't perform lookups here
	if len(req.ReadarrReq) == 0 {
		http.Error(w, "request has no stored selection payload; please re-request from search", http.StatusBadRequest)
		return
	}
	var cand map[string]any
	if err := json.Unmarshal(req.ReadarrReq, &cand); err != nil || cand == nil {
		http.Error(w, "invalid stored selection payload", http.StatusBadRequest)
		return
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
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Author missing id, trying to resolve name='%s'\n", name)
			}
			if name != "" {
				if aid, err := ra.FindAuthorIDByName(ctx, name); err == nil && aid != 0 {
					a["id"] = aid
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Found author id %d for name '%s'\n", aid, name)
					}
				} else {
					// Do not attempt to create an author here. If not found, leave author as-is
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

	// If the stored payload looks like a full Readarr Book schema, send it as-is
	// during approval to honor the provided structure from the client.
	var payload []byte
	var respBody []byte
	if len(req.ReadarrReq) > 0 {
		var raw map[string]any
		if json.Unmarshal(req.ReadarrReq, &raw) == nil {
			// Heuristic: treat as full schema if it contains any of these indicators.
			if _, ok := raw["authorTitle"]; ok || raw["author"] != nil || raw["editions"] != nil || raw["addOptions"] != nil {
				payload, respBody, err = ra.AddBookRaw(ctx, req.ReadarrReq)
			}
		}
	}
	// Fallback to templated add if raw wasn't used
	if payload == nil && err == nil {
		payload, respBody, err = ra.AddBook(ctx, cand, providers.AddOpts{
			QualityProfileID: inst.DefaultQualityProfileID,
			RootFolderPath:   inst.DefaultRootFolderPath,
			SearchForMissing: true,
			Tags:             inst.DefaultTags,
		})
	}
	// Debug: always log sent payload and Readarr response if enabled
	if s.settings.Get().Debug && payload != nil {
		fmt.Printf("DEBUG: Readarr add sent payload:\n%s\n", string(payload))
		if respBody != nil {
			fmt.Printf("DEBUG: Readarr add returned body:\n%s\n", string(respBody))
		}
	}
	if err != nil {
		// If Readarr reports a duplicate edition (already added), try to fetch the
		// existing book and ensure it's monitored using /api/v1/book/monitor
		emsg := strings.ToLower(err.Error())
		if strings.Contains(emsg, "ix_editions_foreigneditionid") || strings.Contains(emsg, "duplicate key value") || strings.Contains(emsg, "already exists") {
			// Attempt GET with the same payload to discover the existing book id
			if payload != nil {
				if bid, gotBody, gerr := ra.GetBookByAddPayload(ctx, payload); gerr == nil && bid > 0 {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Duplicate detected; GET existing book with same payload returned (id=%d):\n%s\n", bid, string(gotBody))
					}
					if monBody, merr := ra.MonitorBooks(ctx, []int{bid}, true); merr == nil {
						if s.settings.Get().Debug {
							mb, _ := json.Marshal(map[string]any{"bookIds": []int{bid}, "monitored": true})
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor sent payload:\n%s\n", string(mb))
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor returned body:\n%s\n", string(monBody))
						}
						_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
						_ = s.db.UpdateRequestStatus(r.Context(), id, "queued", fmt.Sprintf("already in Readarr; monitoring enabled for id %d", bid), r.Context().Value(ctxUser).(*session).Username, payload, respBody)
						trig := map[string]any{"request:updated": map[string]any{
							"id":     id,
							"isbn13": req.ISBN13,
							"isbn10": req.ISBN10,
							"asin":   "",
							"title":  req.Title,
							"format": req.Format,
						}}
						if b, mErr := json.Marshal(trig); mErr == nil {
							w.Header().Set("HX-Trigger", string(b))
						} else {
							w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
						}
						writeJSON(w, map[string]string{"status": "queued"}, 200)
						return
					}
				}
			}
			// Fallback: treat as already present without monitor update
			_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
			_ = s.db.UpdateRequestStatus(r.Context(), id, "queued", "already in Readarr (duplicate edition)", r.Context().Value(ctxUser).(*session).Username, payload, respBody)
			trig := map[string]any{"request:updated": map[string]any{
				"id":     id,
				"isbn13": req.ISBN13,
				"isbn10": req.ISBN10,
				"asin":   "",
				"title":  req.Title,
				"format": req.Format,
			}}
			if b, mErr := json.Marshal(trig); mErr == nil {
				w.Header().Set("HX-Trigger", string(b))
			} else {
				w.Header().Set("HX-Trigger", `{"request:updated": {"id": `+strconv.FormatInt(id, 10)+`}}`)
			}
			writeJSON(w, map[string]string{"status": "queued"}, 200)
			return
		}
		_ = s.db.UpdateRequestStatus(r.Context(), id, "error", err.Error(), "system", payload, respBody)
		// Debug: surface payload and Readarr response in server logs for troubleshooting
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Readarr add error: %v\n---payload---\n%s\n---response---\n%s\n", err, string(payload), string(respBody))
		}
		http.Error(w, "readarr add: "+err.Error(), http.StatusBadGateway)
		return
	}

	// If the add to Readarr succeeded, start a background monitor task that
	// repeatedly sends a monitor payload for the newly created book every 30s
	// for 5 minutes. This helps ensure the book is properly monitored even if
	// the initial request has transient issues. Do not block the HTTP flow.
	if respBody != nil {
		var rb map[string]any
		if json.Unmarshal(respBody, &rb) == nil {
			// Readarr returns the created book with an "id" field when successful
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
					go func(rra *providers.Readarr, bookID int, dbg bool) {
						bgCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
						defer cancel()
						ticker := time.NewTicker(30 * time.Second)
						defer ticker.Stop()

						sendMonitor := func() {
							perCtx, pCancel := context.WithTimeout(bgCtx, 12*time.Second)
							defer pCancel()
							if monBody, merr := rra.MonitorBooks(perCtx, []int{bookID}, true); merr == nil {
								if dbg {
									mb, _ := json.Marshal(map[string]any{"bookIds": []int{bookID}, "monitored": true})
									fmt.Printf("DEBUG: PUT /api/v1/book/monitor sent payload:\n%s\n", string(mb))
									fmt.Printf("DEBUG: PUT /api/v1/book/monitor returned body:\n%s\n", string(monBody))
								}
							} else if dbg {
								fmt.Printf("DEBUG: MonitorBooks attempt for id=%d failed: %v\n", bookID, merr)
							}
						}

						// First immediate attempt
						sendMonitor()
						for {
							select {
							case <-bgCtx.Done():
								return
							case <-ticker.C:
								sendMonitor()
							}
						}
					}(ra, bid, s.settings.Get().Debug)
				}
			}
		}
	}

	_ = s.db.ApproveRequest(r.Context(), id, r.Context().Value(ctxUser).(*session).Username)
	_ = s.db.UpdateRequestStatus(r.Context(), id, "queued", "sent to Readarr", r.Context().Value(ctxUser).(*session).Username, payload, respBody)
	// Include minimal identifiers so the UI can update any matching search results
	trig := map[string]any{"request:updated": map[string]any{
		"id":     id,
		"isbn13": req.ISBN13,
		"isbn10": req.ISBN10,
		"asin":   "",
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
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, AddEndpoint: c.AddEndpoint, AddMethod: c.AddMethod, AddPayloadTemplate: c.AddPayloadTemplate, DefaultQualityProfileID: c.DefaultQualityProfileID, DefaultRootFolderPath: c.DefaultRootFolderPath, DefaultTags: c.DefaultTags, InsecureSkipVerify: c.InsecureSkipVerify}
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

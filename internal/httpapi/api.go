package httpapi

import (
	"context"
	"encoding/json"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jacoknapp/scriptorum/internal/db"
	"github.com/jacoknapp/scriptorum/internal/providers"
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
	})
}

func (s *Server) apiCreateRequest(w http.ResponseWriter, r *http.Request) {
	var p RequestPayload
	if err := json.NewDecoder(r.Body).Decode(&p); err != nil {
		http.Error(w, "bad json", 400)
		return
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
	ra := providers.NewReadarr(inst)

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
	cand, ok := ra.SelectCandidate(res, req.ISBN13, req.ISBN10, asin)
	if !ok {
		if len(res) > 0 {
			cand = map[string]any{"title": res[0].Title, "titleSlug": res[0].TitleSlug, "author": res[0].Author, "editions": res[0].Editions}
		} else {
			http.Error(w, "no candidate found", 404)
			return
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
	writeJSON(w, map[string]string{"status": "queued"}, 200)
}

func firstNonEmpty(vs ...string) string {
	for _, v := range vs {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}

package httpapi

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/util"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountUI(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON":        func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"csrfToken":     func(r *http.Request) string { return s.getCSRFToken(r) },
		"authorsText":   authorsText,
		"truncateChars": truncateChars,
	}
	u := &ui{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router) {
		rt.Use(s.withUser)
		rt.Get("/", s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
			ses := r.Context().Value(ctxUser).(*session)
			mine := ""
			if ses == nil || !ses.Admin {
				mine = s.userEmail(r)
			}
			items, _ := s.db.ListRequestsPage(r.Context(), mine, 200)
			data := map[string]any{"UserName": s.userName(r), "IsAdmin": ses != nil && ses.Admin, "Items": s.buildRequestListItems(r.Context(), items), "FallbackAll": false, "CSRFToken": s.getCSRFToken(r)}
			_ = u.tpl.ExecuteTemplate(w, "requests.html", data)
		}))
		rt.Get("/dashboard", s.requireLogin(u.handleDashboard(s)))
		rt.Get("/search", s.requireLogin(u.handleHome(s)))
		rt.Get("/requests", s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
			ses := r.Context().Value(ctxUser).(*session)
			mine := ""
			if ses == nil || !ses.Admin {
				mine = s.userEmail(r)
			}
			items, _ := s.db.ListRequestsPage(r.Context(), mine, 200)
			data := map[string]any{"UserName": s.userName(r), "IsAdmin": ses != nil && ses.Admin, "Items": s.buildRequestListItems(r.Context(), items), "FallbackAll": false, "CSRFToken": s.getCSRFToken(r)}
			_ = u.tpl.ExecuteTemplate(w, "requests.html", data)
		}))
		rt.HandleFunc("/users", s.requireAdmin(u.handleUsers(s)))
	})
	r.Get("/ui/requests/table", s.requireLogin(u.handleRequestsTable(s)))
	r.Group(func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler { return s.requireAdmin(next.ServeHTTP) })
		rt.Post("/users/delete", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if id := r.URL.Query().Get("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					_ = s.db.DeleteUser(r.Context(), n)
				}
			}
			if id := r.FormValue("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					_ = s.db.DeleteUser(r.Context(), n)
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
		rt.Post("/users/edit", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			idStr := r.FormValue("user_id")
			password := r.FormValue("password")
			confirmPassword := r.FormValue("confirm_password")
			admin := r.FormValue("is_admin") == "on"
			autoApprove := r.FormValue("is_auto_approve") == "on"

			if idStr != "" {
				if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
					// Update admin status
					_ = s.db.SetUserAdmin(r.Context(), id, admin)
					// Update auto-approve status
					_ = s.db.SetUserAutoApprove(r.Context(), id, autoApprove)

					// Update password if provided and confirmed
					if password != "" && password == confirmPassword {
						hash, _ := s.hashPassword(password, s.settings.Get().Auth.Salt)
						_ = s.db.UpdateUserPassword(r.Context(), id, hash)
					}
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
		rt.Post("/users/toggle", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			if id := r.FormValue("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					users, _ := s.db.ListUsers(r.Context())
					for _, u := range users {
						if u.ID == n {
							_ = s.db.SetUserAdmin(r.Context(), n, !u.IsAdmin)
							break
						}
					}
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
	})
}

type ui struct{ tpl *template.Template }

func (u *ui) handleHome(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, isAdmin := "", false
		if ses, ok := r.Context().Value(ctxUser).(*session); ok && ses != nil {
			name, isAdmin = ses.Name, ses.Admin
		}
		data := map[string]any{
			"UserName":  name,
			"IsAdmin":   isAdmin,
			"CSRFToken": s.getCSRFToken(r),
		}
		_ = u.tpl.ExecuteTemplate(w, "home.html", data)
	}
}

func (u *ui) handleDashboard(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ses := r.Context().Value(ctxUser).(*session)
		data := map[string]any{"UserName": ses.Name, "IsAdmin": ses.Admin, "CSRFToken": s.getCSRFToken(r)}
		_ = u.tpl.ExecuteTemplate(w, "dashboard.html", data)
	}
}

func (u *ui) handleRequestsTable(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ses := r.Context().Value(ctxUser).(*session)
		mine := ""
		if ses == nil || !ses.Admin {
			mine = s.userEmail(r)
		}
		items, _ := s.db.ListRequestsPage(r.Context(), mine, 200)
		data := map[string]any{"Items": s.buildRequestListItems(r.Context(), items), "IsAdmin": ses != nil && ses.Admin, "FallbackAll": false}
		_ = u.tpl.ExecuteTemplate(w, "requests_table", data)
	}
}

type requestListItem struct {
	db.Request
	Cover string
}

func (s *Server) buildRequestListItems(ctx context.Context, items []db.Request) []requestListItem {
	matchedBooks := s.requestListMatchedBooks(ctx, items)
	out := make([]requestListItem, 0, len(items))
	for _, item := range items {
		cover := s.requestListCoverData(item, matchedBooks)
		out = append(out, requestListItem{
			Request: item,
			Cover:   cover,
		})
	}
	return out
}

func (s *Server) requestListMatchedBooks(ctx context.Context, items []db.Request) map[string]db.ReadarrBook {
	idsByKind := make(map[string][]int64)
	for _, item := range items {
		if item.MatchedReadarrID <= 0 {
			continue
		}
		kind := normalizeSyncKind(item.Format)
		idsByKind[kind] = append(idsByKind[kind], item.MatchedReadarrID)
	}

	out := make(map[string]db.ReadarrBook)
	for kind, ids := range idsByKind {
		books, err := s.db.ListReadarrBooksByIDs(ctx, kind, ids)
		if err != nil {
			continue
		}
		for id, book := range books {
			out[kind+"|"+strconv.FormatInt(id, 10)] = book
		}
	}
	return out
}

func requestListMatchedBookKey(format string, readarrID int64) string {
	if readarrID <= 0 {
		return ""
	}
	return normalizeSyncKind(format) + "|" + strconv.FormatInt(readarrID, 10)
}

func (s *Server) requestListCoverData(req db.Request, matchedBooks map[string]db.ReadarrBook) string {
	if cover := strings.TrimSpace(req.CoverURL); cover != "" {
		if normalized := s.normalizeRequestCover(req.Format, cover); normalized != "" {
			return normalized
		}
	}
	if cover := s.requestCoverFromPayload(req.Format, req.ReadarrResp); cover != "" {
		return cover
	}
	if cover := s.requestCoverFromPayload(req.Format, req.ReadarrReq); cover != "" {
		return cover
	}
	if key := requestListMatchedBookKey(req.Format, req.MatchedReadarrID); key != "" {
		if book, ok := matchedBooks[key]; ok && len(book.ReadarrData) > 0 {
			if cover := s.requestCoverFromPayload(req.Format, book.ReadarrData); cover != "" {
				return cover
			}
		}
	}
	return ""
}

func (s *Server) requestCoverFromPayload(format string, raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var payload map[string]any
	if err := json.Unmarshal(raw, &payload); err != nil {
		return ""
	}

	cover := util.FirstNonEmpty(
		mapStringValue(payload, "cover"),
		mapStringValue(payload, "remoteCover"),
		mapStringValue(payload, "remotePoster"),
		mapStringValue(payload, "coverUrl"),
	)
	if cover == "" {
		cover = payloadImageURL(payload)
	}
	return s.normalizeRequestCover(format, cover)
}

func payloadImageURL(payload map[string]any) string {
	images, ok := payload["images"].([]any)
	if !ok {
		return ""
	}
	for _, image := range images {
		m, ok := image.(map[string]any)
		if !ok {
			continue
		}
		coverType := strings.ToLower(strings.TrimSpace(mapStringValue(m, "coverType")))
		if coverType != "" && coverType != "cover" && coverType != "poster" {
			continue
		}
		if cover := util.FirstNonEmpty(mapStringValue(m, "remoteUrl"), mapStringValue(m, "url")); cover != "" {
			return cover
		}
	}
	return ""
}

func mapStringValue(payload map[string]any, key string) string {
	if payload == nil {
		return ""
	}
	value, ok := payload[key]
	if !ok {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		return ""
	}
}

func (s *Server) normalizeRequestCover(format, cover string) string {
	cover = strings.TrimSpace(cover)
	if cover == "" {
		return ""
	}
	if strings.HasPrefix(cover, "/ui/readarr-cover") {
		return cover
	}
	inst, ok := s.readarrInstanceForLookup(format)
	if strings.HasPrefix(cover, "/") && (!ok || strings.TrimSpace(inst.BaseURL) == "") {
		return ""
	}
	if strings.HasPrefix(cover, "/") && ok && strings.TrimSpace(inst.BaseURL) != "" {
		cover = strings.TrimRight(inst.BaseURL, "/") + cover
	}
	if ok && strings.HasPrefix(strings.TrimSpace(inst.BaseURL), "http") {
		if parsed, err := url.Parse(cover); err == nil {
			// Some Readarr payloads return absolute MediaCover URLs with localhost,
			// container-only hosts, or without a reverse-proxy base path. Rebase
			// those URLs onto the configured Readarr base before proxying them.
			if parsed.IsAbs() && isReadarrMediaPath(parsed.Path) {
				cover = rebaseReadarrMediaURL(inst.BaseURL, cover)
			}
			if reparsed, reparseErr := url.Parse(cover); reparseErr == nil && strings.EqualFold(reparsed.Host, urlHost(inst.BaseURL)) {
				return "/ui/readarr-cover?u=" + url.QueryEscape(cover)
			}
		}
	}
	return cover
}

func rebaseReadarrMediaURL(baseURL, rawCover string) string {
	base, err := url.Parse(strings.TrimSpace(baseURL))
	if err != nil || base == nil || base.Host == "" {
		return rawCover
	}
	cover, err := url.Parse(strings.TrimSpace(rawCover))
	if err != nil || cover == nil || !cover.IsAbs() || !isReadarrMediaPath(cover.Path) {
		return rawCover
	}

	cover.Scheme = base.Scheme
	cover.Host = base.Host
	cover.Path = joinReadarrBasePath(base.Path, cover.Path)
	cover.RawPath = ""
	return cover.String()
}

func joinReadarrBasePath(basePath, mediaPath string) string {
	basePath = strings.TrimSpace(basePath)
	if basePath == "" || basePath == "/" {
		return mediaPath
	}
	basePath = "/" + strings.Trim(strings.TrimSpace(basePath), "/")
	mediaPath = "/" + strings.TrimLeft(strings.TrimSpace(mediaPath), "/")
	if strings.EqualFold(mediaPath, basePath) || strings.HasPrefix(strings.ToLower(mediaPath), strings.ToLower(basePath+"/")) {
		return mediaPath
	}
	return basePath + mediaPath
}

func isReadarrMediaPath(path string) bool {
	p := strings.ToLower(strings.TrimSpace(path))
	return strings.HasPrefix(p, "/mediacover/") || strings.HasPrefix(p, "/api/v1/mediacover/")
}

func (u *ui) handleUsers(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			username := strings.TrimSpace(r.FormValue("username"))
			password := r.FormValue("password")
			admin := r.FormValue("is_admin") == "on"
			autoApprove := r.FormValue("is_auto_approve") == "on"
			if username != "" && password != "" {
				hash, _ := s.hashPassword(password, s.settings.Get().Auth.Salt)
				_, _ = s.db.CreateUser(r.Context(), username, hash, admin, autoApprove)
			}
			http.Redirect(w, r, "/users", http.StatusFound)
			return
		}
		users, _ := s.db.ListUsers(r.Context())
		data := map[string]any{
			"UserName":  s.userName(r),
			"IsAdmin":   true,
			"Users":     users,
			"CSRFToken": s.getCSRFToken(r),
		}
		_ = u.tpl.ExecuteTemplate(w, "users.html", data)
	}
}

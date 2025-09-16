package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSettings(r chi.Router) {
	funcMap := template.FuncMap{"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) }}
	u := &settingsUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler { return s.requireAdmin(next.ServeHTTP) })
		rt.Get("/settings", u.handleSettings(s))
		rt.Post("/settings/save", u.handleSettingsSave(s))
		rt.Get("/api/readarr/profiles", s.apiReadarrProfiles())
		// Debug endpoint for admins to inspect runtime Readarr settings (API keys redacted)
		rt.Get("/api/readarr/debug", s.apiReadarrDebug())
	})
}

type settingsUI struct{ tpl *template.Template }

func (u *settingsUI) handleSettings(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"Cfg":       s.settings.Get(),
			"UserName":  s.userName(r),
			"IsAdmin":   true,
			"CSRFToken": s.getCSRFToken(r),
		}
		_ = u.tpl.ExecuteTemplate(w, "settings.html", data)
	}
}

func (u *settingsUI) handleSettingsSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()
		// General
		cur.Debug = (r.FormValue("debug") == "on")
		cur.ServerURL = strings.TrimSpace(r.FormValue("server_url"))
		cur.Readarr.Ebooks.BaseURL = strings.TrimSpace(r.FormValue("ra_ebooks_base"))
		cur.Readarr.Ebooks.APIKey = strings.TrimSpace(r.FormValue("ra_ebooks_key"))
		cur.Readarr.Ebooks.InsecureSkipVerify = (r.FormValue("ra_ebooks_insecure") == "on")
		cur.Readarr.Audiobooks.BaseURL = strings.TrimSpace(r.FormValue("ra_audio_base"))
		cur.Readarr.Audiobooks.APIKey = strings.TrimSpace(r.FormValue("ra_audio_key"))
		cur.Readarr.Audiobooks.InsecureSkipVerify = (r.FormValue("ra_audio_insecure") == "on")
		// Save quality profile selections
		if v := strings.TrimSpace(r.FormValue("ra_ebooks_qp")); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				cur.Readarr.Ebooks.DefaultQualityProfileID = i
			}
		}
		if v := strings.TrimSpace(r.FormValue("ra_audio_qp")); v != "" {
			if i, err := strconv.Atoi(v); err == nil {
				cur.Readarr.Audiobooks.DefaultQualityProfileID = i
			}
		}

		// OAuth settings (merged into the settings form)
		vEnabled := strings.ToLower(strings.TrimSpace(r.FormValue("oauth_enabled")))
		cur.OAuth.Enabled = (vEnabled == "true" || vEnabled == "on" || vEnabled == "1")
		cur.OAuth.Issuer = strings.TrimSpace(r.FormValue("oauth_issuer"))
		cur.OAuth.ClientID = strings.TrimSpace(r.FormValue("oauth_client_id"))
		if v := strings.TrimSpace(r.FormValue("oauth_client_secret")); v != "" {
			cur.OAuth.ClientSecret = v
		}
		cur.OAuth.RedirectURL = strings.TrimSpace(r.FormValue("oauth_redirect"))

		// Lists helper
		parseCSV := func(sv string) []string {
			sv = strings.TrimSpace(sv)
			if sv == "" {
				return []string{}
			}
			parts := strings.Split(sv, ",")
			out := make([]string, 0, len(parts))
			for _, p := range parts {
				t := strings.TrimSpace(p)
				if t != "" {
					out = append(out, t)
				}
			}
			return out
		}
		cur.OAuth.Scopes = parseCSV(r.FormValue("oauth_scopes"))
		cur.OAuth.UsernameClaim = strings.TrimSpace(r.FormValue("oauth_username_claim"))
		cur.OAuth.AutoCreateUsers = r.FormValue("oauth_autocreate") == "on"
		_ = s.settings.Update(&cur)
		// Propagate debug flag to provider packages that use package-level Debug variables
		providers.Debug = cur.Debug
		_ = s.initOIDC()
		http.Redirect(w, r, "/settings", http.StatusFound)
	}
}

// OAuth settings are handled as part of the main settings form (/settings/save).

// apiReadarrProfiles returns quality profiles for a given instance (ebooks|audiobooks)
func (s *Server) apiReadarrProfiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		kind := r.URL.Query().Get("kind")
		var inst providers.ReadarrInstance
		cfg := s.settings.Get()
		switch kind {
		case "ebooks":
			c := cfg.Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, InsecureSkipVerify: c.InsecureSkipVerify}
			if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
				http.Error(w, "readarr ebooks not configured", http.StatusBadRequest)
				return
			}
		case "audiobooks":
			c := cfg.Readarr.Audiobooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, InsecureSkipVerify: c.InsecureSkipVerify}
			if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
				http.Error(w, "readarr audiobooks not configured", http.StatusBadRequest)
				return
			}
		default:
			http.Error(w, "missing kind", http.StatusBadRequest)
			return
		}
		ra := providers.NewReadarrWithDB(inst, s.db.SQL())
		// Use the by-id fetcher as requested
		qps, err := ra.GetQualityProfilesByID(r.Context())
		if err != nil {
			http.Error(w, err.Error(), http.StatusBadGateway)
			return
		}
		b, _ := json.Marshal(qps)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write(b)
	}
}

// apiReadarrDebug returns the effective Readarr configuration (API keys masked) so
// admins can verify that InsecureSkipVerify and BaseURL are set as expected.
func (s *Server) apiReadarrDebug() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.settings.Get()
		type inst struct {
			BaseURL            string `json:"base_url"`
			APIKeyMasked       string `json:"api_key_masked"`
			InsecureSkipVerify bool   `json:"insecure_skip_verify"`
		}
		mask := func(k string) string {
			if k == "" {
				return ""
			}
			if len(k) <= 4 {
				return strings.Repeat("*", len(k))
			}
			return k[:4] + strings.Repeat("*", len(k)-4)
		}
		out := map[string]any{
			"debug":      cfg.Debug,
			"ebooks":     inst{BaseURL: cfg.Readarr.Ebooks.BaseURL, APIKeyMasked: mask(cfg.Readarr.Ebooks.APIKey), InsecureSkipVerify: cfg.Readarr.Ebooks.InsecureSkipVerify},
			"audiobooks": inst{BaseURL: cfg.Readarr.Audiobooks.BaseURL, APIKeyMasked: mask(cfg.Readarr.Audiobooks.APIKey), InsecureSkipVerify: cfg.Readarr.Audiobooks.InsecureSkipVerify},
		}
		writeJSON(w, out, http.StatusOK)
	}
}

func (s *Server) userName(r *http.Request) string {
	u, _ := r.Context().Value(ctxUser).(*session)
	if u == nil {
		return ""
	}
	return u.Name
}

func (s *Server) userEmail(r *http.Request) string {
	u, _ := r.Context().Value(ctxUser).(*session)
	if u == nil {
		return ""
	}
	return strings.ToLower(u.Username)
}

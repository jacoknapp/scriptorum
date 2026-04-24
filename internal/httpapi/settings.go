package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

type discoveryLanguageOption struct {
	Code  string
	Label string
}

var discoveryLanguageOptions = []discoveryLanguageOption{
	{Code: "eng", Label: "English"},
	{Code: "spa", Label: "Spanish"},
	{Code: "fre", Label: "French"},
	{Code: "ger", Label: "German"},
	{Code: "ita", Label: "Italian"},
	{Code: "por", Label: "Portuguese"},
	{Code: "dut", Label: "Dutch"},
	{Code: "swe", Label: "Swedish"},
	{Code: "nor", Label: "Norwegian"},
	{Code: "dan", Label: "Danish"},
	{Code: "fin", Label: "Finnish"},
	{Code: "pol", Label: "Polish"},
	{Code: "cze", Label: "Czech"},
	{Code: "hun", Label: "Hungarian"},
	{Code: "rum", Label: "Romanian"},
	{Code: "bul", Label: "Bulgarian"},
	{Code: "gre", Label: "Greek"},
	{Code: "rus", Label: "Russian"},
	{Code: "ukr", Label: "Ukrainian"},
	{Code: "ara", Label: "Arabic"},
	{Code: "heb", Label: "Hebrew"},
	{Code: "hin", Label: "Hindi"},
	{Code: "ben", Label: "Bengali"},
	{Code: "tam", Label: "Tamil"},
	{Code: "tel", Label: "Telugu"},
	{Code: "mal", Label: "Malayalam"},
	{Code: "mar", Label: "Marathi"},
	{Code: "guj", Label: "Gujarati"},
	{Code: "pan", Label: "Punjabi"},
	{Code: "urd", Label: "Urdu"},
	{Code: "tur", Label: "Turkish"},
	{Code: "per", Label: "Persian"},
	{Code: "chi", Label: "Chinese"},
	{Code: "jpn", Label: "Japanese"},
	{Code: "kor", Label: "Korean"},
	{Code: "tha", Label: "Thai"},
	{Code: "vie", Label: "Vietnamese"},
	{Code: "ind", Label: "Indonesian"},
}

func discoveryLanguageSelectedMap(codes []string) map[string]bool {
	codes = config.NormalizeDiscoveryLanguages(codes)
	out := make(map[string]bool, len(codes))
	for _, code := range codes {
		out[code] = true
	}
	return out
}

func (s *Server) mountSettings(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON":        func(v any) string { b, _ := json.Marshal(v); return string(b) },
		"authorsText":   authorsText,
		"truncateChars": truncateChars,
	}
	u := &settingsUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler { return s.requireAdmin(next.ServeHTTP) })
		rt.Get("/settings", u.handleSettings(s))
		rt.Post("/settings/save", u.handleSettingsSave(s))
		rt.Get("/api/readarr/profiles", s.apiReadarrProfiles())
		rt.Post("/api/readarr/profiles", s.apiReadarrProfiles())
		rt.Post("/api/readarr/sync", s.apiReadarrSync())
		// Debug endpoint for admins to inspect runtime Readarr settings (API keys redacted)
		rt.Get("/api/readarr/debug", s.apiReadarrDebug())
	})
}

type settingsUI struct{ tpl *template.Template }

func (u *settingsUI) handleSettings(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.settings.Get()
		data := map[string]any{
			"Cfg":                        cfg,
			"UserName":                   s.userName(r),
			"IsAdmin":                    true,
			"CSRFToken":                  s.getCSRFToken(r),
			"ReadarrSync":                s.readarrSyncView(),
			"DiscoveryLanguageOptions":   discoveryLanguageOptions,
			"DiscoveryLanguageSelection": discoveryLanguageSelectedMap(cfg.Discovery.Languages),
		}
		_ = u.tpl.ExecuteTemplate(w, "settings.html", data)
	}
}

func (u *settingsUI) handleSettingsSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()
		ebooksBase := strings.TrimSpace(r.FormValue("ra_ebooks_base"))
		ebooksKey := preserveSecretField(cur.Readarr.Ebooks.APIKey, ebooksBase, r.FormValue("ra_ebooks_key"))
		audioBase := strings.TrimSpace(r.FormValue("ra_audio_base"))
		audioKey := preserveSecretField(cur.Readarr.Audiobooks.APIKey, audioBase, r.FormValue("ra_audio_key"))
		// General
		cur.Debug = (r.FormValue("debug") == "on")
		cur.ServerURL = strings.TrimSpace(r.FormValue("server_url"))
		cur.Readarr.Ebooks.BaseURL = ebooksBase
		cur.Readarr.Ebooks.APIKey = ebooksKey
		cur.Readarr.Ebooks.InsecureSkipVerify = (r.FormValue("ra_ebooks_insecure") == "on")
		cur.Readarr.Audiobooks.BaseURL = audioBase
		cur.Readarr.Audiobooks.APIKey = audioKey
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
		cur.Discovery.Languages = config.NormalizeDiscoveryLanguages(r.Form["discovery_languages"])
		_ = s.settings.Update(&cur)
		// Propagate debug flag to provider packages that use package-level Debug variables
		providers.Debug = cur.Debug
		_ = s.initOIDC()
		http.Redirect(w, r, "/settings", http.StatusFound)
	}
}

// OAuth settings are handled as part of the main settings form (/settings/save).

func readarrTruthy(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "1", "true", "on", "yes":
		return true
	default:
		return false
	}
}

func preserveSecretField(existing, submittedBase, submittedSecret string) string {
	secret := strings.TrimSpace(submittedSecret)
	if secret != "" || strings.TrimSpace(submittedBase) == "" {
		return secret
	}
	return existing
}

func readarrProbeMessage(err error) string {
	if err == nil {
		return ""
	}
	msg := strings.ToLower(strings.TrimSpace(err.Error()))
	switch {
	case strings.Contains(msg, "401"), strings.Contains(msg, "403"),
		strings.Contains(msg, "unauthorized"), strings.Contains(msg, "forbidden"),
		strings.Contains(msg, "api key"):
		return "Could not connect to Readarr. Check the API key."
	case strings.Contains(msg, "x509"), strings.Contains(msg, "tls"),
		strings.Contains(msg, "certificate"), strings.Contains(msg, "handshake"):
		return "Could not connect to Readarr. Check the certificate or enable Skip TLS verification if you trust it."
	case strings.Contains(msg, "404"), strings.Contains(msg, "no such host"),
		strings.Contains(msg, "connection refused"), strings.Contains(msg, "timeout"),
		strings.Contains(msg, "deadline exceeded"), strings.Contains(msg, "dial tcp"):
		return "Could not connect to Readarr. Check the Base URL and that the server is reachable."
	default:
		return "Could not connect to Readarr. Check the Base URL, API key, and TLS setting."
	}
}

// apiReadarrProfiles returns quality profiles for a given instance (ebooks|audiobooks)
func (s *Server) apiReadarrProfiles() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		kind := r.FormValue("kind")
		var inst providers.ReadarrInstance
		cfg := s.settings.Get()
		switch kind {
		case "ebooks":
			c := cfg.Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, InsecureSkipVerify: c.InsecureSkipVerify}
		case "audiobooks":
			c := cfg.Readarr.Audiobooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, InsecureSkipVerify: c.InsecureSkipVerify}
		default:
			http.Error(w, "missing kind", http.StatusBadRequest)
			return
		}
		if readarrTruthy(r.FormValue("use_overrides")) {
			submittedBase := strings.TrimSpace(r.FormValue("base_url"))
			inst.BaseURL = submittedBase
			inst.APIKey = preserveSecretField(inst.APIKey, submittedBase, r.FormValue("api_key"))
			inst.InsecureSkipVerify = readarrTruthy(r.FormValue("insecure"))
		}
		if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
			http.Error(w, "Readarr is not fully configured yet. Add the Base URL and API key first.", http.StatusBadRequest)
			return
		}
		ra := providers.NewReadarrWithDB(inst, s.db.SQL())
		// Use the by-id fetcher as requested
		qps, err := ra.GetQualityProfilesByID(r.Context())
		if err != nil {
			http.Error(w, readarrProbeMessage(err), http.StatusBadGateway)
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

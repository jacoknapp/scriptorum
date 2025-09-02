package httpapi

import (
	"context"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/jacoknapp/scriptorum/internal/providers"
)

var stepFlags = map[string]bool{"admin": false, "oauth": false, "abs": false, "rebooks": false, "raudio": false}

func (s *Server) mountSetup(r chi.Router) {
	u := &setupUI{tpl: template.Must(template.ParseFS(setupFS, "web/setup/*.html"))}
	r.Get("/setup", u.handleSetupHome(s))
	r.Post("/setup/save", u.handleSetupSave(s))
	r.Post("/setup/finish", u.handleSetupFinish(s))
	r.Get("/setup/can-advance/{n}", u.handleCanAdvance(s))
	r.Get("/setup/test/oauth", u.handleTestOAuth(s))
	r.Get("/setup/test/abs", u.handleTestABS(s))
	r.Get("/setup/test/readarr", u.handleTestReadarr(s))
	r.Get("/setup/step/{n}", u.handleStep(s))
}

type setupUI struct{ tpl *template.Template }

func (u *setupUI) handleSetupHome(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { _ = u.tpl.ExecuteTemplate(w, "wizard.html", nil) }
}

func (u *setupUI) handleSetupSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()
		admin := strings.TrimSpace(r.FormValue("admin_email"))
		if admin != "" && !containsInsensitive(cur.Admins.Emails, admin) {
			cur.Admins.Emails = append(cur.Admins.Emails, admin)
		}
		cur.OAuth.Enabled = r.FormValue("oauth_enabled") == "on"
		cur.OAuth.Issuer = r.FormValue("oauth_issuer")
		cur.OAuth.ClientID = r.FormValue("oauth_client_id")
		cur.OAuth.ClientSecret = r.FormValue("oauth_client_secret")
		cur.OAuth.RedirectURL = r.FormValue("oauth_redirect")
		cur.Audiobookshelf.Enabled = r.FormValue("abs_enabled") == "on"
		cur.Audiobookshelf.BaseURL = r.FormValue("abs_base")
		cur.Audiobookshelf.Token = r.FormValue("abs_token")
		cur.Readarr.Ebooks.BaseURL = r.FormValue("ra_ebooks_base")
		cur.Readarr.Ebooks.APIKey = r.FormValue("ra_ebooks_key")
		cur.Readarr.Audiobooks.BaseURL = r.FormValue("ra_audio_base")
		cur.Readarr.Audiobooks.APIKey = r.FormValue("ra_audio_key")
		_ = s.settings.Update(&cur)
		stepFlags["admin"] = len(cur.Admins.Emails) > 0
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true})
	}
}

func (u *setupUI) handleTestOAuth(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.settings.Get().OAuth.Enabled {
			stepFlags["oauth"] = true
			writeProbe(w, true, "disabled")
			return
		}
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		err := s.initOIDC()
		ok := err == nil
		stepFlags["oauth"] = ok
		writeProbe(w, ok, errString(err))
	}
}

func (u *setupUI) handleTestABS(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cfg := s.settings.Get().Audiobookshelf
		abs := providers.NewABS(cfg.BaseURL, cfg.Token, cfg.SearchEndpoint)
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		err := abs.Ping(ctx)
		ok := err == nil
		stepFlags["abs"] = ok
		writeProbe(w, ok, errString(err))
	}
}

func (u *setupUI) handleTestReadarr(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		var inst providers.ReadarrInstance
		if tag == "ebooks" {
			c := s.settings.Get().Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint}
		} else {
			c := s.settings.Get().Readarr.Audiobooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint}
		}
		ra := providers.NewReadarr(inst)
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		err := ra.PingLookup(ctx)
		ok := err == nil
		if tag == "ebooks" {
			stepFlags["rebooks"] = ok
		} else {
			stepFlags["raudio"] = ok
		}
		writeProbe(w, ok, errString(err))
	}
}

func writeProbe(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "error": errMsg})
}
func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func (u *setupUI) handleSetupFinish(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		cur := *s.settings.Get()
		if len(cur.Admins.Emails) == 0 {
			http.Error(w, "admin required", 400)
			return
		}
		if cur.OAuth.Enabled && (cur.OAuth.Issuer == "" || cur.OAuth.ClientID == "" || cur.OAuth.ClientSecret == "" || cur.OAuth.RedirectURL == "") {
			http.Error(w, "oauth incomplete", 400)
			return
		}
		cur.Setup.Completed = true
		_ = s.settings.Update(&cur)
		http.Redirect(w, r, "/", http.StatusFound)
	}
}

func (u *setupUI) handleCanAdvance(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := chi.URLParam(r, "n")
		ok := false
		switch n {
		case "1":
			ok = stepFlags["admin"]
		case "2":
			ok = stepFlags["oauth"] || !s.settings.Get().OAuth.Enabled
		case "3":
			ok = stepFlags["abs"] || !s.settings.Get().Audiobookshelf.Enabled
		case "4":
			ok = stepFlags["rebooks"] && stepFlags["raudio"]
		case "5":
			ok = true
		}
		writeProbe(w, ok, "")
	}
}

func containsInsensitive(a []string, v string) bool {
	for _, e := range a {
		if strings.EqualFold(e, v) {
			return true
		}
	}
	return false
}

func (u *setupUI) handleStep(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		n := chi.URLParam(r, "n")
		switch n {
		case "1":
			_ = u.tpl.ExecuteTemplate(w, "step_admin.html", nil)
		case "2":
			_ = u.tpl.ExecuteTemplate(w, "step_oauth.html", s.settings.Get())
		case "3":
			_ = u.tpl.ExecuteTemplate(w, "step_abs.html", s.settings.Get())
		case "4":
			_ = u.tpl.ExecuteTemplate(w, "step_readarr.html", s.settings.Get())
		case "5":
			_ = u.tpl.ExecuteTemplate(w, "step_finish.html", nil)
		default:
			http.Error(w, "unknown", 404)
		}
	}
}

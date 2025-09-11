package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"html/template"
	"net/http"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

var stepFlags = map[string]bool{"admin": false, "oauth": false, "rebooks": false, "raudio": false}

func (s *Server) mountSetup(r chi.Router) {
	// If setup is already completed, don't register the setup routes so
	// the wizard cannot be reached.
	if !s.needsSetup() {
		return
	}
	u := &setupUI{tpl: template.Must(template.ParseFS(setupFS, "web/setup/*.html"))}
	// Mount under /setup and apply the setupGate so the wizard is only accessible when needed
	r.Route("/setup", func(rr chi.Router) {
		rr.Use(s.setupGate)
		rr.Get("/", u.handleSetupHome(s))
		rr.Post("/save", u.handleSetupSave(s))
		rr.Post("/finish", u.handleSetupFinish(s))
		rr.Get("/can-advance/{n}", u.handleCanAdvance(s))
		rr.Get("/test/oauth", u.handleTestOAuth(s))
		rr.Get("/test/readarr", u.handleTestReadarr(s))
		rr.Get("/step/{n}", u.handleStep(s))
	})
}

type setupUI struct{ tpl *template.Template }

func (u *setupUI) handleSetupHome(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) { _ = u.tpl.ExecuteTemplate(w, "wizard.html", nil) }
}

func (u *setupUI) handleSetupSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()
		// Ensure we have a config salt for password hashing
		if strings.TrimSpace(cur.Auth.Salt) == "" {
			cur.Auth.Salt = genSalt()
		}
		// Local admin user (username/password) creation
		adminUser := strings.TrimSpace(r.FormValue("admin_username"))
		adminPass := r.FormValue("admin_password")
		if adminUser != "" && adminPass != "" {
			// Hash with config salt and store
			hash, err := s.hashPassword(adminPass, cur.Auth.Salt)
			if err == nil {
				_, _ = s.db.CreateUser(r.Context(), adminUser, hash, true)
			}
		}
		cur.OAuth.Enabled = r.FormValue("oauth_enabled") == "on"
		cur.OAuth.Issuer = r.FormValue("oauth_issuer")
		cur.OAuth.ClientID = r.FormValue("oauth_client_id")
		cur.OAuth.ClientSecret = r.FormValue("oauth_client_secret")
		cur.OAuth.RedirectURL = r.FormValue("oauth_redirect")
		cur.Readarr.Ebooks.BaseURL = r.FormValue("ra_ebooks_base")
		cur.Readarr.Ebooks.APIKey = r.FormValue("ra_ebooks_key")
		cur.Readarr.Ebooks.InsecureSkipVerify = r.FormValue("ra_ebooks_insecure") == "on"
		cur.Readarr.Audiobooks.BaseURL = r.FormValue("ra_audio_base")
		cur.Readarr.Audiobooks.APIKey = r.FormValue("ra_audio_key")
		cur.Readarr.Audiobooks.InsecureSkipVerify = r.FormValue("ra_audio_insecure") == "on"
		_ = s.settings.Update(&cur)
		// Admin step satisfied if at least one local admin user exists
		if n, err := s.db.CountAdmins(r.Context()); err == nil && n > 0 {
			stepFlags["admin"] = true
		} else {
			stepFlags["admin"] = false
		}
		// HTMX: trigger a refresh of gating and reload the current step; no content body
		w.Header().Set("HX-Trigger", "setup-saved")
		w.WriteHeader(http.StatusNoContent)
	}
}

func (u *setupUI) handleTestOAuth(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if !s.settings.Get().OAuth.Enabled {
			stepFlags["oauth"] = true
			w.Header().Set("HX-Trigger", "setup-saved")
			writeProbeHTML(w, true, "disabled")
			return
		}
		err := s.initOIDC()
		ok := err == nil
		stepFlags["oauth"] = ok
		w.Header().Set("HX-Trigger", "setup-saved")
		writeProbeHTML(w, ok, errString(err))
	}
}

// ABS test removed

func (u *setupUI) handleTestReadarr(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		tag := r.URL.Query().Get("tag")
		var inst providers.ReadarrInstance
		if tag == "ebooks" {
			c := s.settings.Get().Readarr.Ebooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, InsecureSkipVerify: c.InsecureSkipVerify}
		} else {
			c := s.settings.Get().Readarr.Audiobooks
			inst = providers.ReadarrInstance{BaseURL: c.BaseURL, APIKey: c.APIKey, LookupEndpoint: c.LookupEndpoint, InsecureSkipVerify: c.InsecureSkipVerify}
		}
		ra := providers.NewReadarrWithDB(inst, s.db.SQL())
		ctx, cancel := context.WithTimeout(r.Context(), 6*time.Second)
		defer cancel()
		err := ra.PingLookup(ctx)
		ok := err == nil
		if tag == "ebooks" {
			stepFlags["rebooks"] = ok
		} else {
			stepFlags["raudio"] = ok
		}
		w.Header().Set("HX-Trigger", "setup-saved")
		writeProbeHTML(w, ok, errString(err))
	}
}

func writeProbeJSON(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]any{"ok": ok, "error": errMsg})
}
func writeProbeHTML(w http.ResponseWriter, ok bool, errMsg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	if ok {
		w.Write([]byte(`<span class="text-green-700">OK</span>`))
		return
	}
	if errMsg == "" {
		errMsg = "failed"
	}
	w.Write([]byte(`<span class="text-red-700">Error: ` + template.HTMLEscapeString(errMsg) + `</span>`))
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
		// Require at least one admin: either a local admin user or a configured admin username in settings
		if n, err := s.db.CountAdmins(r.Context()); err != nil || n == 0 {
			if len(cur.Admins.Usernames) == 0 {
				http.Error(w, "admin required", 400)
				return
			}
		}
		if cur.OAuth.Enabled && (cur.OAuth.Issuer == "" || cur.OAuth.ClientID == "" || cur.OAuth.ClientSecret == "" || cur.OAuth.RedirectURL == "") {
			http.Error(w, "oauth incomplete", 400)
			return
		}
		cur.Setup.Completed = true
		_ = s.settings.Update(&cur)
		http.Redirect(w, r, "/login", http.StatusFound)
	}
}

// genSalt returns a URL-safe random string suitable as a password pepper/salt
func genSalt() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		// fallback to timestamp-ish bytes if RNG fails (unlikely)
		return base64.RawURLEncoding.EncodeToString([]byte(time.Now().Format(time.RFC3339Nano)))
	}
	return base64.RawURLEncoding.EncodeToString(b)
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
			ok = stepFlags["rebooks"] && stepFlags["raudio"]
		case "4":
			ok = true
		}
		writeProbeJSON(w, ok, "")
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
			_ = u.tpl.ExecuteTemplate(w, "step_readarr.html", s.settings.Get())
		case "4":
			_ = u.tpl.ExecuteTemplate(w, "step_finish.html", nil)
		default:
			http.Error(w, "unknown", 404)
		}
	}
}

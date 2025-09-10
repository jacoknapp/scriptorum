package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"github.com/go-chi/chi/v5"
	"golang.org/x/crypto/bcrypt"
	"golang.org/x/oauth2"
)

type oidcMgr struct {
	enabled      bool
	issuer       string
	clientID     string
	clientSecret string
	redirectURL  string
	// cookieName is intentionally not configurable via user settings; server controls session cookie name
	cookieName string
	provider   *oidc.Provider
	verifier   *oidc.IDTokenVerifier
	config     oauth2.Config
}

func (s *Server) initOIDC() error {
	cfg := s.settings.Get()
	s.oidc = &oidcMgr{
		enabled:      cfg.OAuth.Enabled,
		issuer:       cfg.OAuth.Issuer,
		clientID:     cfg.OAuth.ClientID,
		clientSecret: cfg.OAuth.ClientSecret,
		redirectURL:  cfg.OAuth.RedirectURL,
		cookieName:   "scriptorum_session",
	}
	if !s.oidc.enabled {
		return nil
	}
	p, err := oidc.NewProvider(context.Background(), s.oidc.issuer)
	if err != nil {
		return err
	}
	s.oidc.provider = p
	s.oidc.verifier = p.Verifier(&oidc.Config{ClientID: s.oidc.clientID})
	s.oidc.config = oauth2.Config{
		ClientID:     s.oidc.clientID,
		ClientSecret: s.oidc.clientSecret,
		Endpoint:     p.Endpoint(),
		RedirectURL:  s.oidc.redirectURL,
		Scopes:       append([]string{"openid", "email", "profile"}, cfg.OAuth.Scopes...),
	}
	return nil
}

func defaultIf(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

type session struct {
	Email string `json:"email"`
	Name  string `json:"name"`
	Admin bool   `json:"admin"`
	Exp   int64  `json:"exp"`
}

func (s *Server) setSession(w http.ResponseWriter, sess *session) {
	b, _ := json.Marshal(sess)
	sig := s.sign(b)
	cookie := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig)
	http.SetCookie(w, &http.Cookie{Name: "scriptorum_session", Value: cookie, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
}

func (s *Server) getSession(r *http.Request) *session {
	c, err := r.Cookie("scriptorum_session")
	if err != nil || c.Value == "" {
		return nil
	}
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 {
		return nil
	}
	payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !hmac.Equal(sig, s.sign(payload)) {
		return nil
	}
	var sess session
	if err := json.Unmarshal(payload, &sess); err != nil {
		return nil
	}
	if time.Now().Unix() > sess.Exp {
		return nil
	}
	return &sess
}

func (s *Server) sign(b []byte) []byte {
	// Use the server auth salt (config.Auth.Salt) as the HMAC key for session signing
	cfg := s.settings.Get()
	key := defaultIf(cfg.Auth.Salt, "changeme")
	h := hmac.New(sha256.New, []byte(key))
	h.Write(b)
	return h.Sum(nil)
}

func (s *Server) mountAuth(r chi.Router) {
	r.Get("/login", s.handleLogin)
	r.Post("/login", s.handleLocalLogin)
	r.Get("/oauth/callback", s.handleCallback)
	r.Get("/logout", s.handleLogout)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.enabled {
		// Render a tiny login form
		s.renderLoginForm(w, "", "")
		return
	}
	state := "st"
	url := s.oidc.config.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

// renderLoginForm renders the local login page. If msg is non-empty it is shown above the form.
func (s *Server) renderLoginForm(w http.ResponseWriter, username, msg string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	escUser := html.EscapeString(username)
	msgHTML := ""
	if strings.TrimSpace(msg) != "" {
		msgHTML = fmt.Sprintf("<p style=\"color:red\">%s</p>", html.EscapeString(msg))
	}
	w.Write([]byte(`<!doctype html><html><head><title>Login</title><link rel="stylesheet" href="https://cdn.jsdelivr.net/npm/@picocss/pico@2/css/pico.min.css"></head><body><main class="container"><h3>Login</h3>` + msgHTML + `<form method="post" action="/login"><label>Username<input name="username" required value="` + escUser + `"></label><label>Password<input type="password" name="password" required></label><button type="submit">Sign in</button></form></main></body></html>`))
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.enabled {
		http.Redirect(w, r, "/", http.StatusFound)
		return
	}
	code := r.URL.Query().Get("code")
	oauth2Token, err := s.oidc.config.Exchange(r.Context(), code)
	if err != nil {
		http.Error(w, "exchange: "+err.Error(), 500)
		return
	}
	idToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok {
		http.Error(w, "no id_token", 400)
		return
	}
	token, err := s.oidc.verifier.Verify(r.Context(), idToken)
	if err != nil {
		http.Error(w, "verify: "+err.Error(), http.StatusUnauthorized)
		return
	}
	var claims struct{ Email, Name string }
	_ = token.Claims(&claims)

	email := strings.ToLower(claims.Email)
	// Optional allowlists
	cfg := s.settings.Get()
	allowed := true
	if len(cfg.OAuth.AllowDomains) > 0 {
		allowed = false
		for _, d := range cfg.OAuth.AllowDomains {
			if strings.HasSuffix(strings.ToLower(email), "@"+strings.ToLower(strings.TrimSpace(d))) {
				allowed = true
				break
			}
		}
	}
	if allowed && len(cfg.OAuth.AllowEmails) > 0 {
		allowed = false
		for _, e := range cfg.OAuth.AllowEmails {
			if strings.EqualFold(strings.TrimSpace(e), email) {
				allowed = true
				break
			}
		}
	}
	if !allowed {
		http.Error(w, "email not allowed", http.StatusForbidden)
		return
	}

	// Auto-provision users if enabled
	if cfg.OAuth.AutoCreateUsers {
		if _, err := s.db.GetUserByUsername(r.Context(), email); err != nil {
			// create with random/empty password hash; password not used for OAuth logins
			randHash := "$2a$10$scriptorum.oauth.autocreate.dummyhash012345678901234567890"
			_, _ = s.db.CreateUser(r.Context(), email, randHash, s.isAdminEmail(email))
		}
	}

	sess := &session{Email: email, Name: defaultIf(claims.Name, email), Admin: s.isAdminEmail(email), Exp: time.Now().Add(24 * time.Hour).Unix()}
	s.setSession(w, sess)
	http.Redirect(w, r, "/search", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: "scriptorum_session", Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/login", http.StatusFound)
}

func (s *Server) isAdminEmail(e string) bool {
	for _, a := range s.settings.Get().Admins.Emails {
		if strings.EqualFold(a, e) {
			return true
		}
	}
	return false
}

// Local auth
func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	if s.oidc != nil && s.oidc.enabled {
		http.Redirect(w, r, "/login", http.StatusFound)
		return
	}
	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		s.renderLoginForm(w, username, "invalid information")
		return
	}
	u, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		s.renderLoginForm(w, username, "invalid information")
		return
	}
	if err := s.comparePassword(u.Hash, password, s.settings.Get().Auth.Salt); err != nil {
		s.renderLoginForm(w, username, "invalid information")
		return
	}
	sess := &session{Email: u.Username, Name: u.Username, Admin: u.IsAdmin, Exp: time.Now().Add(24 * time.Hour).Unix()}
	s.setSession(w, sess)
	http.Redirect(w, r, "/search", http.StatusFound)
}

func (s *Server) hashPassword(password, salt string) (string, error) {
	peppered := password
	if strings.TrimSpace(salt) != "" {
		peppered = salt + ":" + password
	}
	h, err := bcrypt.GenerateFromPassword([]byte(peppered), bcrypt.DefaultCost)
	return string(h), err
}

func (s *Server) comparePassword(hash, password, salt string) error {
	peppered := password
	if strings.TrimSpace(salt) != "" {
		peppered = salt + ":" + password
	}
	if err := bcrypt.CompareHashAndPassword([]byte(hash), []byte(peppered)); err != nil {
		return errors.New("mismatch")
	}
	return nil
}

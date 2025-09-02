package httpapi

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"strings"
	"time"

	"github.com/coreos/go-oidc/v3/oidc"
	"golang.org/x/oauth2"
)

type oidcMgr struct {
	enabled bool
	issuer string
	clientID string
	clientSecret string
	redirectURL string
	cookieName string
	cookieSecret string
	provider *oidc.Provider
	verifier *oidc.IDTokenVerifier
	config oauth2.Config
}

func (s *Server) initOIDC() error {
	cfg := s.cfg
	s.oidc = &oidcMgr{
		enabled: cfg.OAuth.Enabled,
		issuer: cfg.OAuth.Issuer,
		clientID: cfg.OAuth.ClientID,
		clientSecret: cfg.OAuth.ClientSecret,
		redirectURL: cfg.OAuth.RedirectURL,
		cookieName: defaultIf(cfg.OAuth.CookieName, "scriptorum_session"),
		cookieSecret: cfg.OAuth.CookieSecret,
	}
	if !s.oidc.enabled { return nil }
	p, err := oidc.NewProvider(context.Background(), s.oidc.issuer)
	if err != nil { return err }
	s.oidc.provider = p
	s.oidc.verifier = p.Verifier(&oidc.Config{ClientID: s.oidc.clientID})
	s.oidc.config = oauth2.Config{
		ClientID: s.oidc.clientID,
		ClientSecret: s.oidc.clientSecret,
		Endpoint: p.Endpoint(),
		RedirectURL: s.oidc.redirectURL,
		Scopes: append([]string{"openid","email","profile"}, s.cfg.OAuth.Scopes...),
	}
	return nil
}

func defaultIf(v, d string) string { if strings.TrimSpace(v)=="" { return d }; return v }

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
	http.SetCookie(w, &http.Cookie{
		Name: s.oidc.cookieName, Value: cookie, Path: "/", HttpOnly: true, Secure: s.cfg.OAuth.CookieSecure, SameSite: http.SameSiteLaxMode,
	})
}

func (s *Server) getSession(r *http.Request) *session {
	c, err := r.Cookie(s.oidc.cookieName)
	if err != nil || c.Value == "" { return nil }
	parts := strings.Split(c.Value, ".")
	if len(parts) != 2 { return nil }
	payload, _ := base64.RawURLEncoding.DecodeString(parts[0])
	sig, _ := base64.RawURLEncoding.DecodeString(parts[1])
	if !hmac.Equal(sig, s.sign(payload)) { return nil }
	var sess session
	if err := json.Unmarshal(payload, &sess); err != nil { return nil }
	if time.Now().Unix() > sess.Exp { return nil }
	return &sess
}

func (s *Server) sign(b []byte) []byte {
	h := hmac.New(sha256.New, []byte(defaultIf(s.cfg.OAuth.CookieSecret, "changeme")))
	h.Write(b)
	return h.Sum(nil)
}

func (s *Server) mountAuth(r chi.Router) {
	r.Get("/login", s.handleLogin)
	r.Get("/oauth/callback", s.handleCallback)
	r.Get("/logout", s.handleLogout)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.enabled { http.Redirect(w, r, "/", http.StatusFound); return }
	state := "st"
	url := s.oidc.config.AuthCodeURL(state)
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleCallback(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.enabled { http.Redirect(w, r, "/", http.StatusFound); return }
	code := r.URL.Query().Get("code")
	oauth2Token, err := s.oidc.config.Exchange(r.Context(), code)
	if err != nil { http.Error(w, "exchange: "+err.Error(), 500); return }
	idToken, ok := oauth2Token.Extra("id_token").(string)
	if !ok { http.Error(w, "no id_token", 400); return }
	token, err := s.oidc.verifier.Verify(r.Context(), idToken)
	if err != nil { http.Error(w, "verify: "+err.Error(), 401); return }
	var claims struct{ Email, Name string }
	_ = token.Claims(&claims)

	email := strings.ToLower(claims.Email)
	sess := &session{ Email: email, Name: claims.Name, Admin: s.isAdminEmail(email), Exp: time.Now().Add(24*time.Hour).Unix() }
	s.setSession(w, sess)
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{Name: s.oidc.cookieName, Value: "", Path: "/", MaxAge: -1})
	http.Redirect(w, r, "/", http.StatusFound)
}

func (s *Server) isAdminEmail(e string) bool {
	for _, a := range s.cfg.Admins.Emails { if strings.EqualFold(a, e) { return true } }
	return false
}

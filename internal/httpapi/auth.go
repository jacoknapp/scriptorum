package httpapi

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/rand"
	sha256pkg "crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"html"
	"io"
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
	// Basic validation
	if strings.TrimSpace(s.oidc.issuer) == "" || strings.TrimSpace(s.oidc.clientID) == "" || strings.TrimSpace(s.oidc.redirectURL) == "" {
		fmt.Printf("OIDC disabled: missing issuer/client_id/redirect_url in config.\n")
		s.oidc.enabled = false
		return nil
	}
	// Use a discovery client with sane timeouts
	discCtx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})
	p, err := oidc.NewProvider(discCtx, s.oidc.issuer)
	if err != nil {
		fmt.Printf("OIDC disabled: discovery failed for issuer %s: %v\n", s.oidc.issuer, err)
		s.oidc.enabled = false
		return nil
	}
	s.oidc.provider = p
	s.oidc.verifier = p.Verifier(&oidc.Config{ClientID: s.oidc.clientID})
	// Build unique scopes
	scopeMap := make(map[string]bool)
	for _, s := range []string{"openid", "email", "profile"} {
		scopeMap[s] = true
	}
	for _, s := range cfg.OAuth.Scopes {
		scopeMap[s] = true
	}
	var scopes []string
	for s := range scopeMap {
		scopes = append(scopes, s)
	}
	// Use the provider endpoints as-is (some providers require trailing slashes)
	ep := p.Endpoint()
	// Allow explicit overrides from config if set
	if strings.TrimSpace(cfg.OAuth.AuthURL) != "" {
		ep.AuthURL = cfg.OAuth.AuthURL
	}
	if strings.TrimSpace(cfg.OAuth.TokenURL) != "" {
		ep.TokenURL = cfg.OAuth.TokenURL
	}
	s.oidc.config = oauth2.Config{
		ClientID:     s.oidc.clientID,
		ClientSecret: s.oidc.clientSecret,
		Endpoint:     ep,
		RedirectURL:  s.oidc.redirectURL,
		Scopes:       scopes,
	}
	// Set client auth style: public clients must send client_id in params; confidential in header
	if strings.TrimSpace(s.oidc.clientSecret) == "" {
		s.oidc.config.Endpoint.AuthStyle = oauth2.AuthStyleInParams
	} else {
		s.oidc.config.Endpoint.AuthStyle = oauth2.AuthStyleInHeader
	}
	fmt.Printf("OIDC provider endpoints: auth=%s token=%s\n", ep.AuthURL, ep.TokenURL)
	fmt.Printf("OAuth redirect URL configured: %s\n", s.oidc.redirectURL)
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
	h := hmac.New(sha256pkg.New, []byte(key))
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
	// Generate a random state and PKCE verifier/challenge for this auth request.
	state, err := randomToken(16)
	if err != nil {
		http.Error(w, "failed to create state", http.StatusInternalServerError)
		return
	}
	verifier, challenge, err := generatePKCE()
	if err != nil {
		http.Error(w, "failed to create pkce", http.StatusInternalServerError)
		return
	}

	// Store state and verifier in cookies so we can validate and send verifier on callback
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: state, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})
	http.SetCookie(w, &http.Cookie{Name: "oauth_pkce", Value: verifier, Path: "/", HttpOnly: true, SameSite: http.SameSiteLaxMode})

	url := s.oidc.config.AuthCodeURL(state, oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	fmt.Printf("OIDC auth URL: %s\n", url)
	http.Redirect(w, r, url, http.StatusFound)
}

// randomToken returns a base64-url-encoded random token of n bytes.
func randomToken(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(b), nil
}

// generatePKCE creates a code_verifier and its S256 code_challenge.
func generatePKCE() (verifier string, challenge string, err error) {
	v, err := randomToken(32)
	if err != nil {
		return "", "", err
	}
	sum := sha256pkg.Sum256([]byte(v))
	c := base64.RawURLEncoding.EncodeToString(sum[:])
	return v, c, nil
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
	// Validate state to prevent CSRF
	qstate := r.URL.Query().Get("state")
	sc, err := r.Cookie("oauth_state")
	if err != nil || sc.Value == "" || qstate == "" || sc.Value != qstate {
		http.Error(w, "invalid state", http.StatusBadRequest)
		return
	}
	// Read PKCE verifier from cookie (if present) and clear the cookie
	var verifier string
	if pc, err := r.Cookie("oauth_pkce"); err == nil && pc.Value != "" {
		verifier = pc.Value
		// clear cookie
		http.SetCookie(w, &http.Cookie{Name: "oauth_pkce", Value: "", Path: "/", MaxAge: -1})
	}
	// clear state cookie as well
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: "", Path: "/", MaxAge: -1})

	// Pass code_verifier when using PKCE
	var oauth2Token *oauth2.Token
	opts := []oauth2.AuthCodeOption{}
	if verifier != "" {
		opts = append(opts, oauth2.SetAuthURLParam("code_verifier", verifier))
	}
	// Use an HTTP client that does not follow redirects so we can see if the provider issues a 30x
	// (following a 302 might convert POST to GET, leading to 405 on /token endpoints)
	ctx := r.Context()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	// Wrap transport to log method and URL and a small peek of the body
	base := http.DefaultTransport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		if strings.Contains(req.URL.Path, "/token") {
			// Ensure common headers are present (some edge proxies/WAFs require them)
			if req.Header.Get("User-Agent") == "" {
				req.Header.Set("User-Agent", "curl/8.5.0")
			}
			if req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", "application/json")
			}
			var bodyCopy []byte
			if req.Body != nil {
				bodyCopy, _ = io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(bodyCopy))
			}
			fmt.Printf("OAuth HTTP request: %s %s\n", req.Method, req.URL.String())
			if len(bodyCopy) > 0 {
				if len(bodyCopy) > 512 {
					bodyCopy = bodyCopy[:512]
				}
				fmt.Printf("OAuth HTTP body: %s\n", string(bodyCopy))
			}
			if ct := req.Header.Get("Content-Type"); ct != "" {
				fmt.Printf("OAuth HTTP content-type: %s\n", ct)
			}
		}
		resp, err := base.RoundTrip(req)
		if err == nil && resp != nil && strings.Contains(req.URL.Path, "/token") {
			fmt.Printf("OAuth HTTP response: %s\n", resp.Status)
			if a := resp.Header.Get("Allow"); a != "" {
				fmt.Printf("OAuth HTTP Allow: %s\n", a)
			}
			if s := resp.Header.Get("Server"); s != "" {
				fmt.Printf("OAuth HTTP Server: %s\n", s)
			}
		}
		return resp, err
	})
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	oauth2Token, err = s.oidc.config.Exchange(ctx, code, opts...)
	if err != nil {
		if re, ok := err.(*oauth2.RetrieveError); ok {
			if re.Response != nil {
				fmt.Printf("OIDC token exchange HTTP status: %s\n", re.Response.Status)
				if loc := re.Response.Header.Get("Location"); loc != "" {
					fmt.Printf("OIDC token exchange redirect location: %s\n", loc)
				}
			}
			fmt.Printf("OIDC token exchange body: %s\n", string(re.Body))
		}
		http.Error(w, "exchange failed", http.StatusInternalServerError)
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

// No manual token exchange; rely on oauth2 client

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

// roundTripperFunc is a helper to build http.RoundTripper from a function
type roundTripperFunc func(*http.Request) (*http.Response, error)

func (f roundTripperFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

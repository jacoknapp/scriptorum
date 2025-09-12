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
	"html/template"
	"io"
	"net/http"
	"net/url"
	"sort"
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
	cookieName   string
	cookieDomain string
	cookieSecure bool
	cookieSecret string
	provider     *oidc.Provider
	verifier     *oidc.IDTokenVerifier
	config       oauth2.Config
}

func (s *Server) initOIDC() error {
	cfg := s.settings.Get()
	s.oidc = &oidcMgr{
		enabled:      cfg.OAuth.Enabled,
		issuer:       cfg.OAuth.Issuer,
		clientID:     cfg.OAuth.ClientID,
		clientSecret: cfg.OAuth.ClientSecret,
		redirectURL:  cfg.OAuth.RedirectURL,
		cookieName:   defaultIf(cfg.OAuth.CookieName, "scriptorum_session"),
		cookieDomain: cfg.OAuth.CookieDomain,
		cookieSecure: cfg.OAuth.CookieSecure,
		cookieSecret: defaultIf(cfg.OAuth.CookieSecret, cfg.Auth.Salt),
	}
	if !s.oidc.enabled {
		return nil
	}

	// Normalize issuer URL to handle common misconfigurations
	issuer := strings.TrimSpace(s.oidc.issuer)
	if issuer == "" {
		fmt.Printf("OIDC disabled: missing issuer in config.\n")
		s.oidc.enabled = false
		return nil
	}

	// Remove trailing slash from issuer to avoid issuer mismatch errors
	issuer = strings.TrimSuffix(issuer, "/")
	s.oidc.issuer = issuer

	// Basic validation
	if strings.TrimSpace(s.oidc.clientID) == "" || strings.TrimSpace(s.oidc.redirectURL) == "" {
		fmt.Printf("OIDC disabled: missing client_id/redirect_url in config.\n")
		s.oidc.enabled = false
		return nil
	}
	// Use a discovery client with sane timeouts
	discCtx := context.WithValue(context.Background(), oauth2.HTTPClient, &http.Client{Timeout: 10 * time.Second})

	// Attempt OIDC discovery with issuer normalization
	p, err := oidc.NewProvider(discCtx, issuer)
	if err != nil {
		// Try with trailing slash if the first attempt failed
		if !strings.HasSuffix(issuer, "/") {
			fmt.Printf("OIDC discovery failed for %s, trying with trailing slash: %v\n", issuer, err)
			altIssuer := issuer + "/"
			p, err = oidc.NewProvider(discCtx, altIssuer)
			if err == nil {
				fmt.Printf("OIDC discovery succeeded with trailing slash, updating issuer to: %s\n", altIssuer)
				s.oidc.issuer = altIssuer
				issuer = altIssuer
			}
		}

		if err != nil {
			fmt.Printf("OIDC disabled: discovery failed for issuer %s: %v\n", issuer, err)
			s.oidc.enabled = false
			return nil
		}
	}
	s.oidc.provider = p
	// Create verifier with flexible issuer checking to handle common mismatch issues
	s.oidc.verifier = p.Verifier(&oidc.Config{
		ClientID:             s.oidc.clientID,
		SkipIssuerCheck:      false,                               // We'll handle issuer normalization during discovery instead
		SupportedSigningAlgs: []string{"RS256", "ES256", "PS256"}, // Common signing algorithms
	})
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
	// Use provider endpoints from discovery (preferred) with optional overrides
	ep := p.Endpoint()

	// Validate discovered endpoints are using HTTPS in production
	if !strings.HasPrefix(ep.AuthURL, "https://") && !strings.Contains(s.oidc.redirectURL, "localhost") {
		fmt.Printf("Warning: OAuth authorization URL is not HTTPS in production: %s\n", ep.AuthURL)
	}
	if !strings.HasPrefix(ep.TokenURL, "https://") && !strings.Contains(s.oidc.redirectURL, "localhost") {
		fmt.Printf("Warning: OAuth token URL is not HTTPS in production: %s\n", ep.TokenURL)
	}

	// Allow explicit overrides from config if set (use with caution)
	if strings.TrimSpace(cfg.OAuth.AuthURL) != "" {
		fmt.Printf("Using config override for auth URL: %s\n", cfg.OAuth.AuthURL)
		ep.AuthURL = cfg.OAuth.AuthURL
	}
	if strings.TrimSpace(cfg.OAuth.TokenURL) != "" {
		fmt.Printf("Using config override for token URL: %s\n", cfg.OAuth.TokenURL)
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
	Username string `json:"username"`
	Name     string `json:"name"`
	Admin    bool   `json:"admin"`
	Exp      int64  `json:"exp"`
}

func (s *Server) setSession(w http.ResponseWriter, sess *session) {
	b, _ := json.Marshal(sess)
	sig := s.sign(b)
	cookie := base64.RawURLEncoding.EncodeToString(b) + "." + base64.RawURLEncoding.EncodeToString(sig)

	// Configure cookie with production-ready settings
	cookieName := defaultIf(s.oidc.cookieName, "scriptorum_session")
	httpCookie := &http.Cookie{
		Name:     cookieName,
		Value:    cookie,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	// Apply production cookie settings if configured
	if s.oidc != nil {
		if s.oidc.cookieDomain != "" {
			httpCookie.Domain = s.oidc.cookieDomain
		}
		if s.oidc.cookieSecure {
			httpCookie.Secure = true
		}
	}

	http.SetCookie(w, httpCookie)
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

	// Debug logging for session retrieval (only log occasionally to avoid spam)
	cfg := s.settings.Get()
	if cfg.Debug && time.Now().Unix()%30 == 0 { // Log every ~30 seconds when accessed
		fmt.Printf("DEBUG: Session active - username: %s, admin: %t\n", sess.Username, sess.Admin)
	}

	return &sess
}

func (s *Server) sign(b []byte) []byte {
	// Use the configured cookie secret if available, otherwise fall back to auth salt
	var key string
	if s.oidc != nil && s.oidc.cookieSecret != "" {
		key = s.oidc.cookieSecret
	} else {
		cfg := s.settings.Get()
		key = defaultIf(cfg.Auth.Salt, "changeme")
	}
	h := hmac.New(sha256pkg.New, []byte(key))
	h.Write(b)
	return h.Sum(nil)
}

func (s *Server) mountAuth(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}
	authUI := struct{ tpl *template.Template }{
		tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html")),
	}

	r.Get("/login", s.handleWelcome(authUI.tpl))
	r.Post("/login", s.handleLocalLogin)
	r.Get("/oauth/login", s.handleOAuthLogin)
	r.Get("/oauth/callback", s.handleCallback)
	r.Get("/logout", s.handleLogout)
}

// handleWelcome shows the welcome page with login options
func (s *Server) handleWelcome(tpl *template.Template) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		// Check if we're coming from logout (no auto-redirect to OAuth)
		fromLogout := r.URL.Query().Get("from_logout") == "true"

		// If OAuth is enabled and not coming from logout, and not forcing local login, redirect to OAuth
		if s.oidc != nil && s.oidc.enabled && !fromLogout && r.FormValue("force_local") != "true" {
			// Check if this is a natural visit (not from logout) - auto-redirect to OAuth
			if r.URL.Query().Get("force_welcome") != "true" {
				http.Redirect(w, r, "/oauth/login", http.StatusFound)
				return
			}
		}

		data := map[string]interface{}{
			"OAuthEnabled": s.oidc != nil && s.oidc.enabled,
			"LoginError":   r.URL.Query().Get("error"),
			"Username":     r.URL.Query().Get("username"),
			"CurrentYear":  time.Now().Year(),
		}

		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		if err := tpl.ExecuteTemplate(w, "welcome.html", data); err != nil {
			http.Error(w, "template error", http.StatusInternalServerError)
			return
		}
	}
}

// handleOAuthLogin initiates OAuth login flow
func (s *Server) handleOAuthLogin(w http.ResponseWriter, r *http.Request) {
	if !s.oidc.enabled {
		// OAuth not enabled, redirect to welcome page
		http.Redirect(w, r, "/login?error=OAuth+not+enabled", http.StatusFound)
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
	stateCookie := &http.Cookie{
		Name:     "oauth_state",
		Value:    state,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}
	pkceCookie := &http.Cookie{
		Name:     "oauth_pkce",
		Value:    verifier,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
	}

	// Apply production cookie settings if configured
	if s.oidc.cookieDomain != "" {
		stateCookie.Domain = s.oidc.cookieDomain
		pkceCookie.Domain = s.oidc.cookieDomain
	}
	if s.oidc.cookieSecure {
		stateCookie.Secure = true
		pkceCookie.Secure = true
	}

	http.SetCookie(w, stateCookie)
	http.SetCookie(w, pkceCookie)

	url := s.oidc.config.AuthCodeURL(state, oauth2.SetAuthURLParam("code_challenge", challenge), oauth2.SetAuthURLParam("code_challenge_method", "S256"))
	fmt.Printf("OIDC auth URL: %s\n", url)
	http.Redirect(w, r, url, http.StatusFound)
}

func (s *Server) handleLogin(w http.ResponseWriter, r *http.Request) {
	// This is the old handler - keeping for compatibility but it should redirect to welcome
	http.Redirect(w, r, "/login?force_welcome=true", http.StatusFound)
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

	// Use an HTTP client that does not follow redirects and can modify token requests for PKCE
	ctx := r.Context()
	client := &http.Client{CheckRedirect: func(req *http.Request, via []*http.Request) error { return http.ErrUseLastResponse }}
	// Wrap transport to add PKCE parameters and log requests
	base := http.DefaultTransport
	client.Transport = roundTripperFunc(func(req *http.Request) (*http.Response, error) {
		// For token exchange requests, add PKCE code_verifier if present
		if strings.Contains(req.URL.Path, "/token") && req.Method == "POST" {
			// Read existing body
			var bodyBytes []byte
			if req.Body != nil {
				bodyBytes, _ = io.ReadAll(req.Body)
				req.Body.Close()
			}

			// Parse form data and modify as needed
			values, err := url.ParseQuery(string(bodyBytes))
			if err == nil {
				// Add PKCE code_verifier if present
				if verifier != "" {
					values.Set("code_verifier", verifier)
				}

				// For public clients (no client_secret), ensure client_secret is not sent
				cfg := s.settings.Get()
				if strings.TrimSpace(cfg.OAuth.ClientSecret) == "" {
					values.Del("client_secret")
					// Ensure client_id is present for public clients
					if clientID := strings.TrimSpace(cfg.OAuth.ClientID); clientID != "" {
						values.Set("client_id", clientID)
					}
				}

				newBody := values.Encode()
				req.Body = io.NopCloser(strings.NewReader(newBody))
				req.ContentLength = int64(len(newBody))
			}
		}
		if strings.Contains(req.URL.Path, "/token") {
			// Ensure common headers are present (some edge proxies/WAFs require them)
			if req.Header.Get("User-Agent") == "" {
				req.Header.Set("User-Agent", "curl/8.5.0")
			}
			if req.Header.Get("Accept") == "" {
				req.Header.Set("Accept", "application/json")
			}
			// Read body for logging (body may have been modified above for PKCE/public client)
			var bodyCopy []byte
			if req.Body != nil {
				bodyCopy, _ = io.ReadAll(req.Body)
				req.Body = io.NopCloser(bytes.NewReader(bodyCopy))
			}
			// Full request dump
			fmt.Printf("OAuth HTTP request: %s %s\n", req.Method, req.URL.String())
			fmt.Printf("OAuth HTTP proto: %s\n", req.Proto)
			if req.Host != "" {
				fmt.Printf("OAuth HTTP host: %s\n", req.Host)
			}
			if req.ContentLength >= 0 {
				fmt.Printf("OAuth HTTP content-length: %d\n", req.ContentLength)
			}
			// Headers (sorted for stable output)
			keys := make([]string, 0, len(req.Header))
			for k := range req.Header {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				// join multiple values with "; "
				fmt.Printf("OAuth HTTP header: %s: %s\n", k, strings.Join(req.Header[k], "; "))
			}
			if len(bodyCopy) > 0 {
				fmt.Printf("OAuth HTTP body (len %d): %s\n", len(bodyCopy), string(bodyCopy))
				// If it's form-encoded, also print parsed keys for clarity
				if strings.Contains(strings.ToLower(req.Header.Get("Content-Type")), "application/x-www-form-urlencoded") {
					if vals, err := url.ParseQuery(string(bodyCopy)); err == nil {
						b := &bytes.Buffer{}
						for _, k := range func() []string {
							ks := make([]string, 0, len(vals))
							for k := range vals {
								ks = append(ks, k)
							}
							sort.Strings(ks)
							return ks
						}() {
							for _, v := range vals[k] {
								sv := v
								if len(sv) > 512 {
									sv = sv[:512] + "…"
								}
								fmt.Fprintf(b, "%s=%s\n", k, sv)
							}
						}
						fmt.Printf("OAuth HTTP body (parsed):\n%s", b.String())
					}
				}
			}
			// Helpful curl reproduction
			curl := &bytes.Buffer{}
			fmt.Fprintf(curl, "curl -i -X %s '%s'", req.Method, req.URL.String())
			for _, k := range keys {
				for _, v := range req.Header[k] {
					// Quote single quotes inside value
					vv := strings.ReplaceAll(v, "'", "'\\''")
					fmt.Fprintf(curl, " -H '%s: %s'", k, vv)
				}
			}
			if len(bodyCopy) > 0 {
				b := strings.ReplaceAll(string(bodyCopy), "'", "'\\''")
				fmt.Fprintf(curl, " --data '%s'", b)
			}
			fmt.Printf("OAuth HTTP curl: %s\n", curl.String())
		}
		resp, err := base.RoundTrip(req)
		if err == nil && resp != nil && strings.Contains(req.URL.Path, "/token") {
			fmt.Printf("OAuth HTTP response: %s\n", resp.Status)
			// Dump all response headers
			rkeys := make([]string, 0, len(resp.Header))
			for k := range resp.Header {
				rkeys = append(rkeys, k)
			}
			sort.Strings(rkeys)
			for _, k := range rkeys {
				fmt.Printf("OAuth HTTP resp header: %s: %s\n", k, strings.Join(resp.Header[k], "; "))
			}
			// Peek at response body (without consuming it) up to 4KB
			if resp.Body != nil {
				var snip []byte
				snip, err = io.ReadAll(io.LimitReader(resp.Body, 4096))
				if err == nil {
					rest, _ := io.ReadAll(resp.Body) // likely empty, but ensure drain
					// restore body
					combined := append(append([]byte{}, snip...), rest...)
					resp.Body = io.NopCloser(bytes.NewReader(combined))
					if len(combined) > 0 {
						fmt.Printf("OAuth HTTP resp body (len %d; first 4KB): %s\n", len(combined), string(snip))
					}
				} else {
					// restore to non-nil empty
					resp.Body = io.NopCloser(bytes.NewReader(nil))
				}
			}
		}
		return resp, err
	})
	ctx = context.WithValue(ctx, oauth2.HTTPClient, client)
	oauth2Token, err = s.oidc.config.Exchange(ctx, code)
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
	var claims map[string]any
	_ = token.Claims(&claims)

	// Debug logging for OAuth claims when debug is enabled
	tempCfg := s.settings.Get()
	if tempCfg.Debug {
		fmt.Printf("DEBUG: OAuth claims received:\n")
		for k, v := range claims {
			if k == "preferred_username" || k == "email" || k == "name" || k == tempCfg.OAuth.UsernameClaim {
				fmt.Printf("  %s: %v\n", k, v)
			}
		}
	}

	// Choose a stable username from OIDC claims; use configured claim if present, else preferred_username, else sanitized name
	cfg := s.settings.Get()
	var username string
	if c := strings.TrimSpace(cfg.OAuth.UsernameClaim); c != "" {
		if v, ok := claims[c]; ok {
			username = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		}
	}
	if username == "" {
		if v, ok := claims["preferred_username"]; ok {
			username = strings.ToLower(strings.TrimSpace(fmt.Sprint(v)))
		}
	}
	if username == "" {
		// Fallback to name without spaces/symbols if provided
		n := strings.ToLower(strings.TrimSpace(fmt.Sprint(claims["name"])))
		n = strings.ReplaceAll(n, " ", "")
		username = n
	}
	if username == "" {
		http.Error(w, "no username in claims", http.StatusForbidden)
		return
	}
	// Email/domain allowlists removed; access is controlled by presence of username and (optionally) admin mapping

	// Auto-provision users if enabled (use username)
	if cfg.OAuth.AutoCreateUsers {
		if _, err := s.db.GetUserByUsername(r.Context(), username); err != nil {
			// create with random/empty password hash; password not used for OAuth logins
			randHash := "$2a$10$scriptorum.oauth.autocreate.dummyhash012345678901234567890"
			_, _ = s.db.CreateUser(r.Context(), username, randHash, s.isAdminUsername(username))
		}
	}

	disp := fmt.Sprint(claims["name"])
	if strings.TrimSpace(disp) == "" {
		disp = username
	}

	// Debug logging for OAuth authentication
	if cfg.Debug {
		isAdmin := s.isAdminUsername(username)
		adminUsernames := s.settings.Get().Admins.Usernames
		fmt.Printf("DEBUG: OAuth user authenticated - username: %s, display_name: %s, admin: %t\n",
			username, disp, isAdmin)
		fmt.Printf("DEBUG: Admin usernames configured: %v\n", adminUsernames)
	}

	sess := &session{Username: username, Name: disp, Admin: s.isAdminUsername(username), Exp: time.Now().Add(24 * time.Hour).Unix()}
	s.setSession(w, sess)
	http.Redirect(w, r, "/search", http.StatusFound)
}

// No manual token exchange; rely on oauth2 client

func (s *Server) handleLogout(w http.ResponseWriter, r *http.Request) {
	// Clear the main session cookie
	sessionCookieName := "scriptorum_session"
	if s.oidc != nil && s.oidc.cookieName != "" {
		sessionCookieName = s.oidc.cookieName
	}
	http.SetCookie(w, &http.Cookie{Name: sessionCookieName, Value: "", Path: "/", MaxAge: -1})

	// Clear OAuth-related cookies
	http.SetCookie(w, &http.Cookie{Name: "oauth_state", Value: "", Path: "/", MaxAge: -1})
	http.SetCookie(w, &http.Cookie{Name: "oauth_pkce", Value: "", Path: "/", MaxAge: -1})

	http.Redirect(w, r, "/login?from_logout=true", http.StatusFound)
}

func (s *Server) isAdminUsername(u string) bool {
	for _, a := range s.settings.Get().Admins.Usernames {
		if strings.EqualFold(a, u) {
			return true
		}
	}
	return false
}

// Local auth
func (s *Server) handleLocalLogin(w http.ResponseWriter, r *http.Request) {
	// Check if we're forcing local login or if OAuth is disabled
	forceLocal := r.FormValue("force_local") == "true"
	if s.oidc != nil && s.oidc.enabled && !forceLocal {
		// OAuth is enabled and not forcing local - redirect to welcome page
		http.Redirect(w, r, "/login?force_welcome=true", http.StatusFound)
		return
	}

	_ = r.ParseForm()
	username := strings.TrimSpace(r.FormValue("username"))
	password := r.FormValue("password")
	if username == "" || password == "" {
		http.Redirect(w, r, "/login?error=Invalid+credentials&username="+url.QueryEscape(username)+"&force_welcome=true", http.StatusFound)
		return
	}
	u, err := s.db.GetUserByUsername(r.Context(), username)
	if err != nil {
		http.Redirect(w, r, "/login?error=Invalid+credentials&username="+url.QueryEscape(username)+"&force_welcome=true", http.StatusFound)
		return
	}
	if err := s.comparePassword(u.Hash, password, s.settings.Get().Auth.Salt); err != nil {
		http.Redirect(w, r, "/login?error=Invalid+credentials&username="+url.QueryEscape(username)+"&force_welcome=true", http.StatusFound)
		return
	}

	// Debug logging for local authentication
	cfg := s.settings.Get()
	if cfg.Debug {
		fmt.Printf("DEBUG: Local user authenticated - username: %s, admin: %t\n", u.Username, u.IsAdmin)
	}

	sess := &session{Username: u.Username, Name: u.Username, Admin: u.IsAdmin, Exp: time.Now().Add(24 * time.Hour).Unix()}
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

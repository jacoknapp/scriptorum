package httpapi

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders middleware adds comprehensive security headers
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy - restrictive policy that allows HTMX and inline styles
		csp := "default-src 'self'; " +
			// htmx is vendored under /static; inline scripts/handlers in templates
			// still require 'unsafe-inline' (removing it needs a nonce refactor).
			"script-src 'self' 'unsafe-inline'; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdn.jsdelivr.net; " +
			"font-src 'self' https://fonts.gstatic.com https://cdn.jsdelivr.net; " +
			"img-src 'self' data: https:; " +
			"connect-src 'self'; " +
			"form-action 'self'; " +
			"frame-ancestors 'none'; " +
			"base-uri 'self';"

		// Security Headers
		w.Header().Set("Content-Security-Policy", csp)
		w.Header().Set("X-Frame-Options", "DENY")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("X-XSS-Protection", "1; mode=block")
		w.Header().Set("Referrer-Policy", "strict-origin-when-cross-origin")
		w.Header().Set("Permissions-Policy", "camera=(), microphone=(), geolocation=(), payment=()")

		// HSTS - only set if using HTTPS
		if r.TLS != nil || r.Header.Get("X-Forwarded-Proto") == "https" {
			w.Header().Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains; preload")
		}

		next.ServeHTTP(w, r)
	})
}

func (s *Server) dynamicNoStore(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if !strings.HasPrefix(r.URL.Path, "/static/") {
			w.Header().Set("Cache-Control", "no-store, no-cache, must-revalidate, private")
			w.Header().Set("Pragma", "no-cache")
			w.Header().Set("Expires", "0")
			w.Header().Add("Vary", "Cookie")
			w.Header().Add("Vary", "HX-Request")
		}
		next.ServeHTTP(w, r)
	})
}

// CSRF Protection
const csrfTokenTTL = time.Hour

type csrfTokenInfo struct {
	created time.Time
	session string
}

type csrfManager struct {
	mu     sync.RWMutex
	tokens map[string]csrfTokenInfo
	secret []byte
}

func newCSRFManager() *csrfManager {
	secret := make([]byte, 32)
	rand.Read(secret)
	return &csrfManager{
		tokens: make(map[string]csrfTokenInfo),
		secret: secret,
	}
}

func (c *csrfManager) generateToken(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.cleanupLocked()

	// Generate new token bound to the issuing session.
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	c.tokens[token] = csrfTokenInfo{created: time.Now(), session: sessionID}
	return token
}

// validateToken returns true only when the token exists, is unexpired, and was
// issued to the same session that is now presenting it.
func (c *csrfManager) validateToken(token, sessionID string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	info, exists := c.tokens[token]
	if !exists {
		return false
	}
	if time.Since(info.created) > csrfTokenTTL {
		return false
	}
	return info.session == sessionID
}

// cleanupLocked removes expired tokens. Caller must hold c.mu.
func (c *csrfManager) cleanupLocked() {
	cutoff := time.Now().Add(-csrfTokenTTL)
	for token, info := range c.tokens {
		if info.created.Before(cutoff) {
			delete(c.tokens, token)
		}
	}
}

func (c *csrfManager) cleanup() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.cleanupLocked()
}

// CSRF middleware
func (s *Server) csrfProtection(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Skip CSRF for safe methods and auth endpoints
		if r.Method == "GET" || r.Method == "HEAD" || r.Method == "OPTIONS" ||
			strings.HasPrefix(r.URL.Path, "/oauth/") ||
			strings.HasPrefix(r.URL.Path, "/login") ||
			strings.HasPrefix(r.URL.Path, "/logout") ||
			strings.HasPrefix(r.URL.Path, "/static/") ||
			r.URL.Path == "/healthz" {
			next.ServeHTTP(w, r)
			return
		}

		var token string
		if token = r.Header.Get("X-CSRF-Token"); token == "" {
			token = r.FormValue("_csrf_token")
		}

		if token != "" && s.csrf.validateToken(token, s.csrfSessionID(r)) {
			next.ServeHTTP(w, r)
			return
		}

		// Permit same-origin state-changing requests as a fallback. This keeps
		// regular browser forms and same-origin JS clients working even when a
		// token is missing, while no longer trusting HX-Request by itself.
		if sameOriginRequest(r) {
			next.ServeHTTP(w, r)
			return
		}

		http.Error(w, "csrf validation failed", http.StatusForbidden)
	})
}

func sameOriginRequest(r *http.Request) bool {
	if origin := r.Header.Get("Origin"); origin != "" {
		if u, err := url.Parse(origin); err == nil && u.Host == r.Host {
			return true
		}
	}
	if ref := r.Header.Get("Referer"); ref != "" {
		if u, err := url.Parse(ref); err == nil && u.Host == r.Host {
			return true
		}
	}
	return false
}

// Rate limiting
type rateLimiter struct {
	mu       sync.RWMutex
	requests map[string][]time.Time
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{
		requests: make(map[string][]time.Time),
	}
}

func (rl *rateLimiter) allow(key string, maxRequests int, window time.Duration) bool {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	now := time.Now()
	cutoff := now.Add(-window)

	// Get existing requests for this key
	requests := rl.requests[key]

	// Remove expired entries
	var validRequests []time.Time
	for _, req := range requests {
		if req.After(cutoff) {
			validRequests = append(validRequests, req)
		}
	}

	// Check if we're over the limit
	if len(validRequests) >= maxRequests {
		rl.requests[key] = validRequests
		return false
	}

	// Add current request
	validRequests = append(validRequests, now)
	rl.requests[key] = validRequests

	return true
}

// cleanup evicts keys whose timestamps are all older than maxAge so the map does
// not grow unbounded across distinct client IPs and paths.
func (rl *rateLimiter) cleanup(maxAge time.Duration) {
	rl.mu.Lock()
	defer rl.mu.Unlock()

	cutoff := time.Now().Add(-maxAge)
	for key, requests := range rl.requests {
		var valid []time.Time
		for _, req := range requests {
			if req.After(cutoff) {
				valid = append(valid, req)
			}
		}
		if len(valid) == 0 {
			delete(rl.requests, key)
		} else {
			rl.requests[key] = valid
		}
	}
}

// runSecurityJanitor periodically purges expired CSRF tokens and stale
// rate-limiter entries until the context is cancelled.
func (s *Server) runSecurityJanitor(ctx context.Context) {
	ticker := time.NewTicker(10 * time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.csrf.cleanup()
			// Largest rate-limit window is 15 minutes; drop anything older.
			s.rateLimiter.cleanup(15 * time.Minute)
			s.pruneAuditEvents(ctx)
		}
	}
}

// pruneAuditEvents enforces the configured audit retention policy. A retention
// of 0 (the default) keeps events forever.
func (s *Server) pruneAuditEvents(ctx context.Context) {
	days := s.settings.Get().Audit.RetentionDays
	if days <= 0 {
		return
	}
	cutoff := time.Now().Add(-time.Duration(days) * 24 * time.Hour)
	if n, err := s.db.PruneAuditEvents(ctx, cutoff); err != nil {
		fmt.Printf("audit: prune failed: %v\n", err)
	} else if n > 0 {
		fmt.Printf("audit: pruned %d event(s) older than %d day(s)\n", n, days)
	}
}

// Rate limiting middleware
func (s *Server) rateLimiting(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ip := clientIP(r)

		// Different limits for different endpoints
		var maxRequests int
		var window time.Duration

		switch {
		case strings.HasPrefix(r.URL.Path, "/login") || strings.HasPrefix(r.URL.Path, "/oauth/"):
			// Stricter limits for auth endpoints
			maxRequests = 10
			window = 15 * time.Minute
		case strings.HasPrefix(r.URL.Path, "/api/"):
			// API endpoints
			maxRequests = 100
			window = 5 * time.Minute
		default:
			// General endpoints
			maxRequests = 200
			window = 5 * time.Minute
		}

		key := fmt.Sprintf("%s:%s", ip, r.URL.Path)

		if !s.rateLimiter.allow(key, maxRequests, window) {
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// csrfSessionID derives a stable identifier for the requester used to bind CSRF
// tokens to a session: the authenticated username when available, otherwise the
// client IP for anonymous requests.
func (s *Server) csrfSessionID(r *http.Request) string {
	if user := r.Context().Value(ctxUser); user != nil {
		if session, ok := user.(*session); ok && session.Username != "" {
			return "user:" + session.Username
		}
	}
	return "ip:" + clientIP(r)
}

// Helper function to generate CSRF token for templates
func (s *Server) getCSRFToken(r *http.Request) string {
	return s.csrf.generateToken(s.csrfSessionID(r))
}

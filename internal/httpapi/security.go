package httpapi

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"net/http"
	"strings"
	"sync"
	"time"
)

// SecurityHeaders middleware adds comprehensive security headers
func (s *Server) securityHeaders(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Content Security Policy - restrictive policy that allows HTMX and inline styles
		csp := "default-src 'self'; " +
			"script-src 'self' 'unsafe-inline' 'unsafe-eval' https://unpkg.com/htmx.org@1.9.12 https://cdn.tailwindcss.com; " +
			"style-src 'self' 'unsafe-inline' https://fonts.googleapis.com https://cdn.jsdelivr.net https://cdn.tailwindcss.com; " +
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

// CSRF Protection
type csrfManager struct {
	mu     sync.RWMutex
	tokens map[string]time.Time
	secret []byte
}

func newCSRFManager() *csrfManager {
	secret := make([]byte, 32)
	rand.Read(secret)
	return &csrfManager{
		tokens: make(map[string]time.Time),
		secret: secret,
	}
}

func (c *csrfManager) generateToken(sessionID string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Clean up expired tokens (older than 1 hour)
	cutoff := time.Now().Add(-time.Hour)
	for token, created := range c.tokens {
		if created.Before(cutoff) {
			delete(c.tokens, token)
		}
	}

	// Generate new token
	tokenBytes := make([]byte, 32)
	rand.Read(tokenBytes)
	token := base64.URLEncoding.EncodeToString(tokenBytes)

	c.tokens[token] = time.Now()
	return token
}

func (c *csrfManager) validateToken(token string) bool {
	c.mu.RLock()
	defer c.mu.RUnlock()

	created, exists := c.tokens[token]
	if !exists {
		return false
	}

	// Check if token is expired (1 hour)
	if time.Since(created) > time.Hour {
		return false
	}

	return true
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

		// Check for CSRF token in form, header, or HTMX header
		var token string
		if token = r.FormValue("_csrf_token"); token == "" {
			if token = r.Header.Get("X-CSRF-Token"); token == "" {
				token = r.Header.Get("X-Requested-With") // HTMX sets this
			}
		}

		// For HTMX requests, we can be more lenient and just check the HX-Request header
		if r.Header.Get("HX-Request") == "true" {
			// HTMX requests from same origin are considered safe due to CORS
			next.ServeHTTP(w, r)
			return
		}

		// Validate and consume CSRF token for regular form submissions
		if token == "" {
			http.Error(w, "CSRF token invalid or missing", http.StatusForbidden)
			return
		}

		if !s.csrf.validateToken(token) {
			http.Error(w, "CSRF token invalid or missing", http.StatusForbidden)
			return
		}

		next.ServeHTTP(w, r)
	})
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

// Rate limiting middleware
func (s *Server) rateLimiting(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Get client IP
		clientIP := r.Header.Get("X-Forwarded-For")
		if clientIP == "" {
			clientIP = r.Header.Get("X-Real-IP")
		}
		if clientIP == "" {
			clientIP = r.RemoteAddr
		}

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

		key := fmt.Sprintf("%s:%s", clientIP, r.URL.Path)

		if !s.rateLimiter.allow(key, maxRequests, window) {
			http.Error(w, "Rate limit exceeded. Please try again later.", http.StatusTooManyRequests)
			return
		}

		next.ServeHTTP(w, r)
	})
}

// Helper function to generate CSRF token for templates
func (s *Server) getCSRFToken(r *http.Request) string {
	// Use session ID or IP as identifier
	sessionID := "anonymous"
	if user := r.Context().Value(ctxUser); user != nil {
		if session, ok := user.(*session); ok {
			sessionID = session.Username
		}
	}
	if sessionID == "anonymous" {
		sessionID = r.RemoteAddr
	}

	return s.csrf.generateToken(sessionID)
}

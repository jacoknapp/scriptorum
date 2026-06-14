package httpapi

import (
	"context"
	"net"
	"net/http"
	"strings"
)

type ctxKey string

const ctxUser ctxKey = "user"

// clientIP resolves the originating client IP for a request. Scriptorum is
// typically self-hosted behind a single reverse proxy, so the proxy-supplied
// forwarding headers are trusted when present. The port is always stripped so
// the value is stable across a client's connections (important for rate-limit
// and CSRF keying).
func clientIP(r *http.Request) string {
	if xff := r.Header.Get("X-Forwarded-For"); xff != "" {
		// The left-most entry is the original client.
		if ip := strings.TrimSpace(strings.Split(xff, ",")[0]); ip != "" {
			return ip
		}
	}
	if xr := strings.TrimSpace(r.Header.Get("X-Real-IP")); xr != "" {
		return xr
	}
	if host, _, err := net.SplitHostPort(r.RemoteAddr); err == nil {
		return host
	}
	return r.RemoteAddr
}

func (s *Server) withUser(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var u *session
		if s.oidc != nil {
			u = s.getSession(r)
		}
		ctx := r.Context()
		if u != nil {
			ctx = context.WithValue(ctx, ctxUser, u)
		}
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func (s *Server) requireLogin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Context().Value(ctxUser) == nil {
			if isAsyncRequest(r) {
				w.Header().Set("HX-Redirect", "/login")
				w.Header().Set("X-Login-Required", "true")
				http.Error(w, "authentication required", http.StatusUnauthorized)
				return
			}
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
	}
}

func isAsyncRequest(r *http.Request) bool {
	if strings.EqualFold(r.Header.Get("HX-Request"), "true") {
		return true
	}
	if strings.EqualFold(r.Header.Get("X-Requested-With"), "XMLHttpRequest") {
		return true
	}
	if strings.HasPrefix(r.URL.Path, "/api/") || strings.HasPrefix(r.URL.Path, "/ui/") {
		return true
	}
	switch strings.ToLower(r.Header.Get("Sec-Fetch-Mode")) {
	case "cors", "same-origin":
		return true
	default:
		return false
	}
}

func (s *Server) requireAdmin(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		u, _ := r.Context().Value(ctxUser).(*session)
		if u == nil || !u.Admin {
			http.Error(w, "forbidden", http.StatusForbidden)
			return
		}
		next(w, r)
	}
}

package httpapi

import (
	"context"
	"net/http"
	"strings"
)

type ctxKey string

const ctxUser ctxKey = "user"

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

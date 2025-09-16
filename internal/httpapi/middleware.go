package httpapi

import (
	"context"
	"net/http"
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
			http.Redirect(w, r, "/login", http.StatusFound)
			return
		}
		next(w, r)
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

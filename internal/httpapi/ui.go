package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountUI(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}
	u := &ui{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router) {
		rt.Use(s.withUser)
		rt.Get("/", func(w http.ResponseWriter, r *http.Request) {
			if ses := s.getSession(r); ses == nil {
				http.Redirect(w, r, "/login", http.StatusFound)
				return
			}
			u.handleHome(s)(w, r)
		})
		rt.Get("/dashboard", s.requireLogin(u.handleDashboard(s)))
	})
	r.Get("/ui/requests/table", s.requireLogin(u.handleRequestsTable(s)))
}

type ui struct{ tpl *template.Template }

func (u *ui) handleHome(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		name, isAdmin := "", false
		if ses, ok := r.Context().Value(ctxUser).(*session); ok && ses != nil {
			name, isAdmin = ses.Name, ses.Admin
		}
		data := map[string]any{"UserName": name, "IsAdmin": isAdmin}
		_ = u.tpl.ExecuteTemplate(w, "home.html", data)
	}
}

func (u *ui) handleDashboard(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		ses := r.Context().Value(ctxUser).(*session)
		data := map[string]any{"UserName": ses.Name, "IsAdmin": ses.Admin}
		_ = u.tpl.ExecuteTemplate(w, "dashboard.html", data)
	}
}

func (u *ui) handleRequestsTable(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		items, _ := s.db.ListRequests(r.Context(), "", 200)
		ses := r.Context().Value(ctxUser).(*session)
		data := map[string]any{"Items": items, "IsAdmin": ses.Admin}
		_ = u.tpl.ExecuteTemplate(w, "requests_table.html", data)
	}
}

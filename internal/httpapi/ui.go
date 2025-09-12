package httpapi

import (
	"encoding/json"
	"html/template"
	"net/http"
	"strconv"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountUI(r chi.Router) {
	funcMap := template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}
	u := &ui{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router) {
		rt.Use(s.withUser)
		rt.Get("/", s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
			ses := r.Context().Value(ctxUser).(*session)
			mine := ""
			if ses == nil || !ses.Admin {
				mine = s.userEmail(r)
			}
			items, _ := s.db.ListRequests(r.Context(), mine, 200)
			data := map[string]any{"UserName": s.userName(r), "IsAdmin": ses != nil && ses.Admin, "Items": items, "FallbackAll": false}
			_ = u.tpl.ExecuteTemplate(w, "requests.html", data)
		}))
		rt.Get("/dashboard", s.requireLogin(u.handleDashboard(s)))
		rt.Get("/search", s.requireLogin(u.handleHome(s)))
		rt.Get("/requests", s.requireLogin(func(w http.ResponseWriter, r *http.Request) {
			ses := r.Context().Value(ctxUser).(*session)
			mine := ""
			if ses == nil || !ses.Admin {
				mine = s.userEmail(r)
			}
			items, _ := s.db.ListRequests(r.Context(), mine, 200)
			data := map[string]any{"UserName": s.userName(r), "IsAdmin": ses != nil && ses.Admin, "Items": items, "FallbackAll": false}
			_ = u.tpl.ExecuteTemplate(w, "requests.html", data)
		}))
		rt.HandleFunc("/users", s.requireAdmin(u.handleUsers(s)))
	})
	r.Get("/ui/requests/table", s.requireLogin(u.handleRequestsTable(s)))
	r.Group(func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler { return s.requireAdmin(next.ServeHTTP) })
		rt.Get("/users/delete", func(w http.ResponseWriter, r *http.Request) {
			if id := r.URL.Query().Get("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					_ = s.db.DeleteUser(r.Context(), n)
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
		rt.Post("/users/edit", func(w http.ResponseWriter, r *http.Request) {
			_ = r.ParseForm()
			idStr := r.FormValue("user_id")
			password := r.FormValue("password")
			confirmPassword := r.FormValue("confirm_password")
			admin := r.FormValue("is_admin") == "on"

			if idStr != "" {
				if id, err := strconv.ParseInt(idStr, 10, 64); err == nil {
					// Update admin status
					_ = s.db.SetUserAdmin(r.Context(), id, admin)

					// Update password if provided and confirmed
					if password != "" && password == confirmPassword {
						hash, _ := s.hashPassword(password, s.settings.Get().Auth.Salt)
						_ = s.db.UpdateUserPassword(r.Context(), id, hash)
					}
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
		rt.Get("/users/toggle", func(w http.ResponseWriter, r *http.Request) {
			if id := r.URL.Query().Get("id"); id != "" {
				if n, err := strconv.ParseInt(id, 10, 64); err == nil {
					users, _ := s.db.ListUsers(r.Context())
					for _, u := range users {
						if u.ID == n {
							_ = s.db.SetUserAdmin(r.Context(), n, !u.IsAdmin)
							break
						}
					}
				}
			}
			http.Redirect(w, r, "/users", http.StatusFound)
		})
	})
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
		ses := r.Context().Value(ctxUser).(*session)
		mine := ""
		if ses == nil || !ses.Admin {
			mine = s.userEmail(r)
		}
		items, _ := s.db.ListRequests(r.Context(), mine, 200)
		data := map[string]any{"Items": items, "IsAdmin": ses != nil && ses.Admin, "FallbackAll": false}
		_ = u.tpl.ExecuteTemplate(w, "requests_table", data)
	}
}

func (u *ui) handleUsers(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			_ = r.ParseForm()
			username := strings.TrimSpace(r.FormValue("username"))
			password := r.FormValue("password")
			admin := r.FormValue("is_admin") == "on"
			if username != "" && password != "" {
				hash, _ := s.hashPassword(password, s.settings.Get().Auth.Salt)
				_, _ = s.db.CreateUser(r.Context(), username, hash, admin)
			}
			http.Redirect(w, r, "/users", http.StatusFound)
			return
		}
		users, _ := s.db.ListUsers(r.Context())
		data := map[string]any{"UserName": s.userName(r), "IsAdmin": true, "Users": users}
		_ = u.tpl.ExecuteTemplate(w, "users.html", data)
	}
}

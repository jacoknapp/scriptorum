package httpapi

import (
	"html/template"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
)

func (s *Server) mountSettings(r chi.Router) {
	u := &settingsUI{tpl: template.Must(template.ParseFS(tplFS, "web/templates/*.html"))}
	r.Group(func(rt chi.Router){
		rt.Use(s.requireAdmin)
		rt.Get("/settings", u.handleSettings(s))
		rt.Post("/settings/save", u.handleSettingsSave(s))
	})
}

type settingsUI struct{ tpl *template.Template }

func (u *settingsUI) handleSettings(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{ "Cfg": s.settings.Get(), "UserName": s.userName(r), "IsAdmin": true }
		_ = u.tpl.ExecuteTemplate(w, "settings.html", data)
	}
}

func (u *settingsUI) handleSettingsSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()
		cur.Audiobookshelf.BaseURL = strings.TrimSpace(r.FormValue("abs_base"))
		cur.Audiobookshelf.Token   = strings.TrimSpace(r.FormValue("abs_token"))
		cur.Readarr.Ebooks.BaseURL = strings.TrimSpace(r.FormValue("ra_ebooks_base"))
		cur.Readarr.Ebooks.APIKey  = strings.TrimSpace(r.FormValue("ra_ebooks_key"))
		cur.Readarr.Audiobooks.BaseURL = strings.TrimSpace(r.FormValue("ra_audio_base"))
		cur.Readarr.Audiobooks.APIKey  = strings.TrimSpace(r.FormValue("ra_audio_key"))
		_ = s.settings.Update(&cur)
		http.Redirect(w, r, "/settings", http.StatusFound)
	}
}

func (s *Server) userName(r *http.Request) string {
	u, _ := r.Context().Value(ctxUser).(*session)
	if u == nil { return "" }
	return u.Name
}

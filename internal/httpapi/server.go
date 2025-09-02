package httpapi

import (
	"embed"
	"encoding/json"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"github.com/jacoknapp/scriptorum/internal/config"
	"github.com/jacoknapp/scriptorum/internal/db"
	"github.com/jacoknapp/scriptorum/internal/settings"
)

//go:embed web/static/*
var staticFS embed.FS

//go:embed web/templates/*
var tplFS embed.FS

//go:embed web/setup/*
var setupFS embed.FS

type Server struct {
	cfg      *config.Config
	db       *db.DB
	cfgPath  string
	settings *settings.Store
	chi      *chi.Mux
	oidc     *oidcMgr
}

func NewServer(cfg *config.Config, database *db.DB, cfgPath string) *Server {
	s := &Server{cfg: cfg, db: database, cfgPath: cfgPath, settings: settings.New(cfgPath, cfg), chi: chi.NewRouter()}
	_ = s.initOIDC()
	return s
}

func (s *Server) Router() http.Handler {
	r := s.chi
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)

	s.mountAuth(r)
	s.mountSetup(r)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("ok")) })
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(staticFS))))

	r.Group(func(rt chi.Router) {
		rt.Use(s.setupGate)
		s.mountUI(rt)
		s.mountAPI(rt)
		s.mountSearch(rt)
		s.mountSettings(rt)
	})

	return r
}

func (s *Server) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/setup") || r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/oauth") || strings.HasPrefix(r.URL.Path, "/login") || strings.HasPrefix(r.URL.Path, "/logout") {
			next.ServeHTTP(w, r)
			return
		}
		if s.needsSetup() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) needsSetup() bool {
	cur := s.settings.Get()
	if cur == nil {
		return true
	}
	if len(cur.Admins.Emails) == 0 {
		return true
	}
	if cur.OAuth.Enabled && (cur.OAuth.Issuer == "" || cur.OAuth.ClientID == "" || cur.OAuth.ClientSecret == "" || cur.OAuth.RedirectURL == "") {
		return true
	}
	if cur.Readarr.Ebooks.BaseURL == "" || cur.Readarr.Audiobooks.BaseURL == "" {
		return true
	}
	if !cur.Setup.Completed {
		return true
	}
	return false
}

func writeJSON(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

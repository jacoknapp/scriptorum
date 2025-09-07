package httpapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/settings"
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
	r.Use(s.withUser)

	s.mountAuth(r)
	s.mountSetup(r)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("ok")) })
	// Serve embedded static files under /static from the web/static folder
	sub, _ := fs.Sub(staticFS, "web/static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

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
		if strings.HasPrefix(r.URL.Path, "/setup") {
			if !s.needsSetup() {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
			return
		}
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/oauth") || strings.HasPrefix(r.URL.Path, "/login") || strings.HasPrefix(r.URL.Path, "/logout") {
			next.ServeHTTP(w, r)
			return
		}
		// NOTE: Previously we forced a redirect to /setup when the app
		// reported it needed setup. That made it impossible to visit other
		// management pages while setup was marked incomplete. Allow normal
		// routing to continue so admins can re-run the setup manually at
		// /setup without being forcibly redirected there on every request.
		next.ServeHTTP(w, r)
	})
}

func (s *Server) needsSetup() bool {
	// Prefer reading the on-disk config so manual edits to the config file
	// (for example toggling setup.completed) are respected immediately
	// without needing to restart the server.
	cur, err := config.Load(s.cfgPath)
	if err != nil {
		// Fall back to in-memory settings if loading fails.
		cur = s.settings.Get()
		if cur == nil {
			return true
		}
	}
	// If setup was marked completed, don't force the wizard again.
	if cur.Setup.Completed {
		return false
	}
	// setup not completed -> allow the wizard to run (so admins can re-run it)
	return true
}

func writeJSON(w http.ResponseWriter, v any, code int) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

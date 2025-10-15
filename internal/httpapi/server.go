package httpapi

import (
	"embed"
	"encoding/json"
	"io/fs"
	"net/http"
	"strings"
	"sync"

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
	cfg            *config.Config
	db             *db.DB
	cfgPath        string
	settings       *settings.Store
	chi            *chi.Mux
	oidc           *oidcMgr
	csrf           *csrfManager
	rateLimiter    *rateLimiter
	disableCSRF    bool // For testing purposes
	approvalTokens map[string]approvalTokenData
	tokenMutex     sync.RWMutex
}

func NewServer(cfg *config.Config, database *db.DB, cfgPath string) *Server {
	s := &Server{
		cfg:            cfg,
		db:             database,
		cfgPath:        cfgPath,
		settings:       settings.New(cfgPath, cfg),
		chi:            chi.NewRouter(),
		csrf:           newCSRFManager(),
		rateLimiter:    newRateLimiter(),
		approvalTokens: make(map[string]approvalTokenData),
	}
	_ = s.initOIDC()
	return s
}

func (s *Server) Router() http.Handler {
	r := s.chi

	// Add security middleware first
	r.Use(s.securityHeaders)
	r.Use(s.rateLimiting)
	r.Use(middleware.RequestID, middleware.RealIP, middleware.Logger, middleware.Recoverer)
	r.Use(s.withUser)
	if !s.disableCSRF {
		r.Use(s.csrfProtection)
	}

	s.mountAuth(r)
	s.mountSetup(r)

	r.Get("/healthz", func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(200); w.Write([]byte("ok")) })

	// If setup is needed, redirect the root path to the setup wizard so
	// first-time users are guided through initial configuration.
	r.Get("/", func(w http.ResponseWriter, r *http.Request) {
		if s.needsSetup() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}
		http.Redirect(w, r, "/login", http.StatusFound)
	})
	// Serve embedded static files under /static from the web/static folder
	sub, _ := fs.Sub(staticFS, "web/static")
	r.Handle("/static/*", http.StripPrefix("/static/", http.FileServer(http.FS(sub))))

	// Protect the main application routes behind authentication by default.
	// Individual sub-mounts may still apply admin middleware where needed.
	r.Group(func(rt chi.Router) {
		rt.Use(s.setupGate)
		// Require login for everything inside this group; allow OPTIONS
		// preflight requests through so CORS checks succeed without forcing
		// an authentication redirect.
		rt.Use(func(next http.Handler) http.Handler {
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.Method == http.MethodOptions {
					// Allow preflight to proceed without login
					next.ServeHTTP(w, r)
					return
				}
				// Use the existing requireLogin wrapper to authenticate other methods
				s.requireLogin(func(w2 http.ResponseWriter, r2 *http.Request) {
					next.ServeHTTP(w2, r2)
				})(w, r)
			})
		})
		s.mountUI(rt)
		s.mountAPI(rt)
		s.mountSearch(rt)
		s.mountSettings(rt)
		s.mountNotifications(rt)
	})

	// Public approval token endpoint for one-click approvals from notifications
	// Keep this public so emailed/ntfy approval links can be used without login.
	r.Get("/approve/{token}", s.handleApprovalToken)

	return r
}

func (s *Server) setupGate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// If the request is for the setup wizard itself, allow it when setup is needed,
		// otherwise redirect back to root when setup already completed.
		if strings.HasPrefix(r.URL.Path, "/setup") {
			if !s.needsSetup() {
				http.Redirect(w, r, "/", http.StatusFound)
				return
			}
			next.ServeHTTP(w, r)
			return
		}

		// Always allow health, static assets, oauth and auth endpoints to pass through.
		if r.URL.Path == "/healthz" || strings.HasPrefix(r.URL.Path, "/static/") || strings.HasPrefix(r.URL.Path, "/oauth") || strings.HasPrefix(r.URL.Path, "/login") || strings.HasPrefix(r.URL.Path, "/logout") {
			next.ServeHTTP(w, r)
			return
		}

		// If setup is not completed, redirect all other requests to the setup wizard.
		if s.needsSetup() {
			http.Redirect(w, r, "/setup", http.StatusFound)
			return
		}

		// Default: allow normal routing
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

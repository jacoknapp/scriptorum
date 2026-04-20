package main

import (
	"context"
	"log"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"gitea.knapp/jacoknapp/scriptorum/internal/bootstrap"
	"gitea.knapp/jacoknapp/scriptorum/internal/httpapi"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
)

var (
	ensureFirstRunFn = bootstrap.EnsureFirstRun
	newServerFn      = httpapi.NewServer
	listenAndServeFn = func(server *http.Server) error { return server.ListenAndServe() }
	shutdownServerFn = func(server *http.Server, ctx context.Context) error { return server.Shutdown(ctx) }
	notifyContextFn  = signal.NotifyContext
	logFatalfFn      = log.Fatalf
)

func main() {
	run()
}

func run() {
	// Prefer repository-local data directory when running from the repo root
	cfgPath := getenv("SCRIPTORUM_CONFIG_PATH", "data/scriptorum.yaml")
	dbPath := getenv("SCRIPTORUM_DB_PATH", "data/scriptorum.db")

	ctx := context.Background()
	cfg, database, err := ensureFirstRunFn(ctx, cfgPath, dbPath)
	if err != nil {
		logFatalfFn("bootstrap: %v", err)
	}
	defer database.Close()

	// Propagate debug setting to provider packages that may print directly
	providers.Debug = cfg.Debug

	srv := newServerFn(cfg, database, cfgPath)
	appCtx, cancelApp := context.WithCancel(context.Background())
	defer cancelApp()
	srv.StartBackgroundTasks(appCtx)
	server := &http.Server{Addr: cfg.HTTP.Listen, Handler: srv.Router()}

	go func() {
		log.Printf("listening on %s", cfg.HTTP.Listen)
		if err := listenAndServeFn(server); err != nil && err != http.ErrServerClosed {
			logFatalfFn("http: %v", err)
		}
	}()

	stopCtx, stop := notifyContextFn(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-stopCtx.Done()
	cancelApp()
	_ = shutdownServerFn(server, context.Background())
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

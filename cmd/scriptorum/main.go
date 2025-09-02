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
)

func main() {
	cfgPath := getenv("SCRIPTORUM_CONFIG_PATH", "/data/scriptorum.yaml")
	dbPath := getenv("SCRIPTORUM_DB_PATH", "/data/scriptorum.db")

	ctx := context.Background()
	cfg, database, err := bootstrap.EnsureFirstRun(ctx, cfgPath, dbPath)
	if err != nil {
		log.Fatalf("bootstrap: %v", err)
	}
	defer database.Close()

	srv := httpapi.NewServer(cfg, database, cfgPath)
	server := &http.Server{Addr: cfg.HTTP.Listen, Handler: srv.Router()}

	go func() {
		log.Printf("listening on %s", cfg.HTTP.Listen)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			log.Fatalf("http: %v", err)
		}
	}()

	stopCtx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()
	<-stopCtx.Done()
	_ = server.Shutdown(context.Background())
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

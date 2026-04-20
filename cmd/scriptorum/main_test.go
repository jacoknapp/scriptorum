package main

import (
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func resetMainDeps() {
	ensureFirstRunFn = bootstrapEnsureFirstRun
	newServerFn = httpapiNewServer
	listenAndServeFn = func(server *http.Server) error { return server.ListenAndServe() }
	shutdownServerFn = func(server *http.Server, ctx context.Context) error { return server.Shutdown(ctx) }
	notifyContextFn = signalNotifyContext
	logFatalfFn = logFatalf
}

var (
	bootstrapEnsureFirstRun = ensureFirstRunFn
	httpapiNewServer        = newServerFn
	signalNotifyContext     = notifyContextFn
	logFatalf               = logFatalfFn
)

func TestGetenvReturnsDefaultWhenUnset(t *testing.T) {
	t.Setenv("__SCRIPTORUM_TEST_ENV__", "")
	if got := getenv("__SCRIPTORUM_TEST_ENV__", "fallback"); got != "fallback" {
		t.Fatalf("want fallback got %q", got)
	}
}

func TestGetenvReturnsValueWhenSet(t *testing.T) {
	t.Setenv("__SCRIPTORUM_TEST_ENV__", "value")
	if got := getenv("__SCRIPTORUM_TEST_ENV__", "fallback"); got != "value" {
		t.Fatalf("want value got %q", got)
	}
}

func TestRunHandlesBootstrapError(t *testing.T) {
	t.Cleanup(resetMainDeps)

	ensureFirstRunFn = func(ctx context.Context, cfgPath, dbPath string) (*config.Config, *db.DB, error) {
		return nil, nil, errors.New("boom")
	}
	logFatalfFn = func(format string, args ...any) {
		panic("fatal")
	}

	defer func() {
		if r := recover(); r == nil {
			t.Fatal("expected panic from log fatal")
		}
	}()
	run()
}

func TestRunStartsAndShutsDown(t *testing.T) {
	t.Cleanup(resetMainDeps)

	tdir := t.TempDir()
	database, err := db.Open(filepath.Join(tdir, "scriptorum.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{}
	cfg.HTTP.Listen = "127.0.0.1:0"

	ensureFirstRunFn = func(ctx context.Context, cfgPath, dbPath string) (*config.Config, *db.DB, error) {
		return cfg, database, nil
	}
	listenAndServeFn = func(server *http.Server) error { return http.ErrServerClosed }
	shutdownServerFn = func(server *http.Server, ctx context.Context) error { return nil }
	notifyContextFn = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}

	run()
}

func TestMainCallsRun(t *testing.T) {
	t.Cleanup(resetMainDeps)

	tdir := t.TempDir()
	database, err := db.Open(filepath.Join(tdir, "scriptorum-main.db"))
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	if err := database.Migrate(context.Background()); err != nil {
		t.Fatalf("migrate: %v", err)
	}

	cfg := &config.Config{}
	cfg.HTTP.Listen = "127.0.0.1:0"

	ensureFirstRunFn = func(ctx context.Context, cfgPath, dbPath string) (*config.Config, *db.DB, error) {
		return cfg, database, nil
	}
	listenAndServeFn = func(server *http.Server) error { return http.ErrServerClosed }
	shutdownServerFn = func(server *http.Server, ctx context.Context) error { return nil }
	notifyContextFn = func(parent context.Context, signals ...os.Signal) (context.Context, context.CancelFunc) {
		ctx, cancel := context.WithCancel(parent)
		cancel()
		return ctx, func() {}
	}

	main()
}

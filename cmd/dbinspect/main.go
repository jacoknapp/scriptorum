package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"
	"path/filepath"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	_ "modernc.org/sqlite"
)

func main() {
	cfgPath := getenv("SCRIPTORUM_CONFIG_PATH", filepath.FromSlash("/data/scriptorum.yaml"))
	var dbPath string

	if fileExists(cfgPath) {
		cfg, err := config.Load(cfgPath)
		if err != nil {
			log.Fatalf("load config: %v", err)
		}
		dbPath = cfg.DB.Path
		if dbPath == "" {
			dbPath = filepath.FromSlash("/data/scriptorum.db")
		}
	} else {
		dbPath = getenv("SCRIPTORUM_DB_PATH", filepath.FromSlash("/data/scriptorum.db"))
	}

	if !fileExists(dbPath) {
		fmt.Printf("DB not found at %s\n", dbPath)
		return
	}

	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	var n int
	if err := db.QueryRow("SELECT COUNT(1) FROM requests").Scan(&n); err != nil {
		log.Fatalf("count: %v", err)
	}
	fmt.Printf("requests: %d\n", n)
	if n > 0 {
		rows, err := db.Query("SELECT id, title, requester_email, status, format FROM requests ORDER BY id DESC LIMIT 20")
		if err != nil {
			log.Fatalf("query: %v", err)
		}
		defer rows.Close()
		for rows.Next() {
			var id int64
			var title, email, status, format string
			_ = rows.Scan(&id, &title, &email, &status, &format)
			fmt.Printf("- #%d [%s] %s — %s (%s)\n", id, format, title, status, email)
		}
	}
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func fileExists(p string) bool { st, err := os.Stat(p); return err == nil && !st.IsDir() }

package main

import (
	"database/sql"
	"fmt"
	"log"
	"os"

	_ "modernc.org/sqlite"
)

func main() {
	dbPath := "data/scriptorum.db"
	if len(os.Args) > 1 {
		dbPath = os.Args[1]
	}
	dsn := fmt.Sprintf("file:%s?_pragma=busy_timeout(10000)&_pragma=journal_mode(WAL)", dbPath)
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		log.Fatalf("open db: %v", err)
	}
	defer db.Close()

	rows, err := db.Query("SELECT id, username, is_admin, created_at FROM users ORDER BY id DESC")
	if err != nil {
		log.Fatalf("query users: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		var username string
		var isAdmin int
		var created string
		_ = rows.Scan(&id, &username, &isAdmin, &created)
		fmt.Printf("%d\t%s\tadmin=%d\tcreated=%s\n", id, username, isAdmin, created)
	}
}

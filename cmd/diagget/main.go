package main

import (
	"bytes"
	"database/sql"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	dbpkg "gitea.knapp/jacoknapp/scriptorum/internal/db"
)

type reqRow struct {
	ID      int64
	Format  string
	Payload string
}

func main() {
	// Flags to override detection
	cfgFlag := flag.String("cfg", "", "path to scriptorum.yaml")
	dbFlag := flag.String("db", "", "path to scriptorum.db (overrides cfg DB path)")
	kindFlag := flag.String("kind", "ebooks", "which Readarr instance to use when not inferring from DB (ebooks|audiobooks)")
	payloadStr := flag.String("payload", "", "payload JSON string to use for GET (overrides DB)")
	payloadFile := flag.String("payload-file", "", "path to a JSON file containing the payload to use for GET (overrides DB)")
	flag.Parse()

	// Locate config file: prefer ./data/scriptorum.yaml, else ./scriptorum/data/scriptorum.yaml, else flag
	var cfgPath string
	if *cfgFlag != "" {
		cfgPath = *cfgFlag
	} else {
		cwd, _ := os.Getwd()
		cand1 := filepath.Join(cwd, "data", "scriptorum.yaml")
		cand2 := filepath.Join(cwd, "scriptorum", "data", "scriptorum.yaml")
		if _, err := os.Stat(cand1); err == nil {
			cfgPath = cand1
		} else if _, err := os.Stat(cand2); err == nil {
			cfgPath = cand2
		} else {
			fatalf("could not find config; tried %s and %s (use -cfg)", cand1, cand2)
		}
	}
	cfg, err := config.Load(cfgPath)
	if err != nil {
		fatalf("load config: %v", err)
	}

	dbPath := cfg.DB.Path
	if *dbFlag != "" {
		dbPath = *dbFlag
	}
	d, err := dbpkg.Open(dbPath)
	if err != nil {
		fatalf("open db: %v", err)
	}
	defer d.Close()

	var row *reqRow
	// Prepare payload from flags if provided
	var payloadJSON string
	if *payloadFile != "" {
		b, rerr := os.ReadFile(*payloadFile)
		if rerr != nil {
			fatalf("read payload file: %v", rerr)
		}
		payloadJSON = string(b)
	} else if strings.TrimSpace(*payloadStr) != "" {
		payloadJSON = *payloadStr
	} else {
		row, err = latestWithPayload(d.SQL())
		if err != nil {
			// Fallback: show a few recent requests to aid debugging
			fmt.Fprintf(os.Stderr, "no stored payload rows: %v\n", err)
			if list, lerr := listRecent(d.SQL(), 5); lerr == nil {
				fmt.Println("Recent requests (id, format, hasPayload):")
				for _, it := range list {
					fmt.Printf("- %d, %s, %v\n", it.ID, it.Format, strings.TrimSpace(it.Payload) != "")
				}
			}
			os.Exit(1)
		}
		if strings.TrimSpace(row.Payload) == "" {
			if list, lerr := listRecent(d.SQL(), 5); lerr == nil {
				fmt.Println("Recent requests (id, format, hasPayload):")
				for _, it := range list {
					fmt.Printf("- %d, %s, %v\n", it.ID, it.Format, strings.TrimSpace(it.Payload) != "")
				}
			}
			fatalf("latest request has no readarr payload")
		}
		payloadJSON = row.Payload
	}

	// Choose instance
	inst := cfg.Readarr.Ebooks
	if row != nil {
		if strings.EqualFold(row.Format, "audiobook") && strings.TrimSpace(cfg.Readarr.Audiobooks.BaseURL) != "" {
			inst = cfg.Readarr.Audiobooks
		}
	} else if strings.EqualFold(*kindFlag, "audiobooks") || strings.EqualFold(*kindFlag, "audiobook") {
		inst = cfg.Readarr.Audiobooks
	}
	base := strings.TrimRight(inst.BaseURL, "/")
	ep := inst.AddEndpoint
	if strings.TrimSpace(ep) == "" {
		ep = "/api/v1/book"
	}
	u := base + ep
	if strings.Contains(u, "?") {
		u += "&apikey=" + url.QueryEscape(inst.APIKey)
	} else {
		u += "?apikey=" + url.QueryEscape(inst.APIKey)
	}

	if row != nil {
		fmt.Printf("Using request id=%d format=%s\n", row.ID, row.Format)
	} else {
		fmt.Printf("Using instance kind=%s (from flag)\n", *kindFlag)
	}
	fmt.Println("Payload:")
	fmt.Println(payloadJSON)

	req, _ := http.NewRequest(http.MethodGet, u, bytes.NewReader([]byte(payloadJSON)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", inst.APIKey)
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "Scriptorum/diagget")

	cl := &http.Client{}
	resp, err := cl.Do(req)
	if err != nil {
		fatalf("GET error: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	fmt.Printf("GET status: %s\n", resp.Status)
	// pretty print if JSON
	var js any
	if json.Unmarshal(body, &js) == nil {
		pp, _ := json.MarshalIndent(js, "", "  ")
		fmt.Println(string(pp))
	} else {
		fmt.Println(string(body))
	}
}

func latestWithPayload(db *sql.DB) (*reqRow, error) {
	q := `SELECT id, format, readarr_request FROM requests WHERE readarr_request IS NOT NULL ORDER BY id DESC LIMIT 1`
	r := &reqRow{}
	var payload sql.NullString
	if err := db.QueryRow(q).Scan(&r.ID, &r.Format, &payload); err != nil {
		return nil, err
	}
	if payload.Valid {
		r.Payload = payload.String
	}
	return r, nil
}

func listRecent(db *sql.DB, n int) ([]*reqRow, error) {
	if n <= 0 {
		n = 5
	}
	q := `SELECT id, format, readarr_request FROM requests ORDER BY id DESC LIMIT ?`
	rows, err := db.Query(q, n)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*reqRow
	for rows.Next() {
		var r reqRow
		var payload sql.NullString
		if err := rows.Scan(&r.ID, &r.Format, &payload); err != nil {
			return nil, err
		}
		if payload.Valid {
			r.Payload = payload.String
		}
		out = append(out, &r)
	}
	return out, nil
}

func fatalf(f string, a ...any) {
	fmt.Fprintf(os.Stderr, f+"\n", a...)
	os.Exit(1)
}

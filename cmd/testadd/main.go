package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	yaml "gopkg.in/yaml.v3"
)

// Experimental test harness: lookup a book, try to resolve/create an author,
// then attempt to add via AddBookRaw with a small variant sweep, stopping on
// first success.
func main() {
	ctx := context.Background()
	base := getenv("TESTREADARR_BASE", "")
	key := getenv("TESTREADARR_KEY", "")
	if base == "" || key == "" {
		if cbase, ckey, err := readFromConfig(); err == nil {
			if base == "" {
				base = cbase
			}
			if key == "" {
				key = ckey
			}
		} else {
			log.Printf("config read warning: %v", err)
		}
	}
	if base == "" {
		base = "https://readarr.knapp"
	}

	inst := providers.ReadarrInstance{
		BaseURL:                 base,
		APIKey:                  key,
		LookupEndpoint:          "/api/v1/book/lookup",
		AddEndpoint:             "/api/v1/book",
		AddMethod:               "POST",
		DefaultQualityProfileID: 2,
		DefaultRootFolderPath:   "/books/audiobooks",
		DefaultTags:             []string{"requested-by-abs", "audiobook"},
	}
	ra := providers.NewReadarr(inst)

	// Print server lists and pick a server quality profile if available
	var chosenQualityID any = nil
	if qps, err := ra.GetQualityProfiles(ctx); err == nil {
		fmt.Println("QualityProfiles:")
		for id, name := range qps {
			fmt.Printf("  %d: %s\n", id, name)
			if chosenQualityID == nil {
				chosenQualityID = id
			}
		}
	} else {
		fmt.Printf("Failed to fetch quality profiles: %v\n", err)
	}
	var serverRoot string
	if rfs, err := ra.GetRootFolders(ctx); err == nil {
		fmt.Println("RootFolders:")
		for _, p := range rfs {
			fmt.Printf("  %s\n", p)
		}
		if len(rfs) > 0 {
			serverRoot = rfs[0]
		}
	} else {
		fmt.Printf("Failed to fetch root folders: %v\n", err)
	}

	term := getenv("TERM", "eternal blaze")
	fmt.Printf("Lookup term: %s\n", term)
	books, err := ra.LookupByTerm(ctx, term)
	if err != nil {
		log.Fatalf("lookup failed: %v", err)
	}
	if len(books) == 0 {
		// Fallback: try OpenLibrary to find an ISBN for the term + forced author, then re-query Readarr
		if candFromOL, ok := enrichFromOpenLibrary(ctx, term+" ilona andrews", ra); ok {
			books = []providers.LookupBook{candFromOL}
		} else {
			log.Fatalf("no lookup results for term %q", term)
		}
	}

	// Select candidate (best-effort). If selector fails, pick first result.
	cand, ok := ra.SelectCandidate(books, "", "", "")
	if !ok {
		// Build a candidate map from first book
		b := books[0]
		author := map[string]any{}
		if b.Author != nil {
			author = b.Author
		} else if len(b.Authors) > 0 {
			author = b.Authors[0]
		} else if b.AuthorId > 0 {
			author["id"] = b.AuthorId
		} else if b.AuthorTitle != "" {
			author["name"] = b.AuthorTitle
		}

		cand = providers.Candidate{
			"title":            b.Title,
			"titleSlug":        b.TitleSlug,
			"author":           author,
			"editions":         b.Editions,
			"foreignBookId":    b.ForeignBookId,
			"foreignEditionId": b.ForeignEditionId,
		}
	}

	// If candidate lacks foreign ids, try to enrich once more via OpenLibrary using title+author
	if (fmt.Sprint(cand["foreignBookId"]) == "" || fmt.Sprint(cand["foreignEditionId"]) == "") && fmt.Sprint(cand["title"]) != "" {
		if candFromOL, ok := enrichFromOpenLibrary(ctx, fmt.Sprint(cand["title"])+" ilona andrews", ra); ok {
			// Merge key fields
			cand["title"] = candFromOL.Title
			cand["titleSlug"] = candFromOL.TitleSlug
			cand["foreignBookId"] = candFromOL.ForeignBookId
			cand["foreignEditionId"] = candFromOL.ForeignEditionId
			cand["editions"] = candFromOL.Editions
		}
	}

	// Force the author to the requested value per user instruction.
	// This ensures we attempt to resolve/create an author record for Ilona Andrews.
	cand["author"] = map[string]any{"name": "ilona andrews"}
	// Remove any pre-populated numeric authorId so the resolution flow runs.
	delete(cand, "authorId")

	// Enrich/resolve author id when possible. Prefer to create/find a numeric authorId
	if a, ok := cand["author"].(map[string]any); ok {
		if _, hasID := cand["authorId"]; !hasID {
			if _, has := a["id"]; !has {
				if nm, _ := a["name"].(string); nm != "" {
					// Try to find existing author id by name
					if aid, err := ra.FindAuthorIDByName(ctx, nm); err == nil && aid != 0 {
						cand["authorId"] = aid
						delete(cand, "author")
					} else {
						// Try to lookup foreign author id (string) then import
						if fid := ra.LookupForeignAuthorIDString(ctx, nm); fid != "" {
							if newID, err := ra.ImportAuthor(ctx, fid); err == nil && newID > 0 {
								cand["authorId"] = newID
								delete(cand, "author")
							}
						}
						// As a last resort try to create an author record
						if _, hasID2 := cand["authorId"]; !hasID2 {
							if aid2, cerr := ra.CreateAuthor(ctx, nm); cerr == nil && aid2 != 0 {
								cand["authorId"] = aid2
								delete(cand, "author")
							}
						}
					}
				}
			}
		}
	}

	// Ensure editions present: include exactly one monitored edition targeting the chosen foreignEditionId
	cand["editions"] = buildMonitoredEditions(cand)

	// Determine chosen root
	chosenRoot := inst.DefaultRootFolderPath
	if serverRoot != "" {
		chosenRoot = serverRoot
	}

	// Try a direct add using AddBookRaw with the current candidate
	basePayload := map[string]any{
		"title":             cand["title"],
		"titleSlug":         cand["titleSlug"],
		"editions":          cand["editions"],
		"foreignBookId":     cand["foreignBookId"],
		"foreignEditionId":  cand["foreignEditionId"],
		"rootFolderPath":    chosenRoot,
		"monitored":         true,
		"metadataProfileId": 1,
		"addOptions":        map[string]any{"addType": "automatic", "monitor": "all", "monitored": true, "booksToMonitor": []any{}, "searchForMissingBooks": true, "searchForNewBook": true},
		"tags":              inst.DefaultTags,
	}
	if aid, ok := cand["authorId"]; ok {
		basePayload["authorId"] = aid
		delete(basePayload, "author")
	} else if a, ok := cand["author"].(map[string]any); ok {
		basePayload["author"] = a
	} else if at, ok := cand["authorTitle"]; ok {
		basePayload["authorTitle"] = at
	}
	// Always include a server-picked qualityProfileId if available to avoid validator NRE
	if chosenQualityID != nil {
		basePayload["qualityProfileId"] = chosenQualityID
	} else {
		basePayload["qualityProfileId"] = inst.DefaultQualityProfileID
	}

	// Ensure basePayload.editions has one monitored entry
	ensureEditions(basePayload)
	b, _ := json.Marshal(basePayload)
	fmt.Printf("Attempting direct add payload: %s\n", string(b))
	sent, resp, err := ra.AddBookRaw(ctx, b)
	if err == nil {
		fmt.Printf("Add succeeded. Sent: %s\nResponse: %s\n", string(sent), string(resp))
		return
	}
	fmt.Printf("Direct add failed: %v\nResponse: %s\n", err, string(resp))

	// Run variant sweep: quality present/omit, addType automatic/manual, include author object or authorTitle, root choices
	qOptions := []any{chosenQualityID, nil}
	addTypes := []string{"automatic", "manual"}
	includeAuthorObj := []bool{false, true}
	authorForeignPresent := []bool{false, true}
	rootChoices := []string{chosenRoot, inst.DefaultRootFolderPath}

	variantIdx := 0
	for _, q := range qOptions {
		for _, at := range addTypes {
			for _, incAuth := range includeAuthorObj {
				for _, authForeign := range authorForeignPresent {
					for _, rt := range rootChoices {
						variantIdx++
						v := map[string]any{}
						v["title"] = cand["title"]
						v["titleSlug"] = cand["titleSlug"]
						v["editions"] = buildMonitoredEditions(cand)
						v["foreignBookId"] = cand["foreignBookId"]
						v["foreignEditionId"] = cand["foreignEditionId"]
						v["rootFolderPath"] = rt
						v["monitored"] = true
						v["metadataProfileId"] = 1
						v["addOptions"] = map[string]any{"addType": at, "monitor": "all", "monitored": true, "booksToMonitor": []any{}, "searchForMissingBooks": (at == "automatic"), "searchForNewBook": (at == "automatic")}
						v["tags"] = inst.DefaultTags

						if q != nil {
							v["qualityProfileId"] = q
						}

						// Prefer to send a numeric authorId when we already resolved/created it.
						if aid, hasAID := cand["authorId"]; hasAID {
							v["authorId"] = aid
						} else if incAuth {
							authObj := map[string]any{}
							if name, ok := cand["authorTitle"].(string); ok {
								authObj["name"] = name
							} else if a, ok := cand["author"].(map[string]any); ok {
								if n, _ := a["name"].(string); n != "" {
									authObj["name"] = n
								}
							}
							if authForeign {
								if nm, _ := authObj["name"].(string); nm != "" {
									if fid := ra.LookupForeignAuthorIDString(ctx, nm); fid != "" {
										authObj["foreignAuthorId"] = fid
									} else {
										authObj["foreignAuthorId"] = strings.ReplaceAll(strings.TrimSpace(nm), " ", "-")
									}
								}
							}
							authObj["rootFolderPath"] = rt
							v["author"] = authObj
						} else {
							if atitle, ok := cand["authorTitle"]; ok {
								v["authorTitle"] = atitle
							}
						}

						// Ensure editions is a non-nil empty array to avoid server ArgumentNullException
						ensureEditions(v)
						pb, _ := json.Marshal(v)
						fmt.Printf("\n--- Variant %d payload: %s\n", variantIdx, string(pb))
						_, rresp, rerr := ra.AddBookRaw(ctx, pb)
						if rerr != nil {
							fmt.Printf("Variant %d error: %v\nResponse: %s\n", variantIdx, rerr, string(rresp))
						} else {
							fmt.Printf("Variant %d success. Response: %s\n", variantIdx, string(rresp))
							return
						}
						// small delay to avoid hammering server
						time.Sleep(400 * time.Millisecond)
					}
				}
			}
		}
	}

	log.Fatalf("all variants failed after %d attempts", variantIdx)
}

func getenv(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}

func readFromConfig() (string, string, error) {
	try := []string{"scriptorum.yaml", filepath.Join("..", "scriptorum.yaml")}
	for _, p := range try {
		if b, err := os.ReadFile(p); err == nil {
			var cfg map[string]any
			if err := yaml.Unmarshal(b, &cfg); err != nil {
				return "", "", err
			}
			base, _ := cfg["TESTREADARR_BASE"].(string)
			key, _ := cfg["TESTREADARR_KEY"].(string)
			return base, key, nil
		}
	}
	return "", "", fmt.Errorf("config not found")
}

// ensureEditions coerces a nil or absent editions field to an empty array so
// JSON marshaling emits [] instead of null. This avoids server-side null
// dereference when the API expects an enumerable.
func ensureEditions(m map[string]any) {
	if m == nil {
		return
	}
	if v, ok := m["editions"]; !ok || v == nil {
		m["editions"] = []any{}
	}
}

// enrichFromOpenLibrary tries OpenLibrary search to find an ISBN for the query,
// then re-queries Readarr by that ISBN and returns a single lookup book.
func enrichFromOpenLibrary(ctx context.Context, q string, ra *providers.Readarr) (providers.LookupBook, bool) {
	ol := providers.NewOpenLibrary()
	items, err := ol.Search(ctx, strings.TrimSpace(q), 5, 1)
	if err != nil || len(items) == 0 {
		return providers.LookupBook{}, false
	}
	var isbn string
	// prefer ISBN13
	for _, it := range items {
		if s := strings.TrimSpace(it.ISBN13); s != "" {
			isbn = s
			break
		}
		if s := strings.TrimSpace(it.ISBN10); s != "" && isbn == "" {
			isbn = s
			// don't break; keep looking for an ISBN13 in later items
		}
	}
	if isbn == "" {
		return providers.LookupBook{}, false
	}
	books, err := ra.LookupByTerm(ctx, isbn)
	if err != nil || len(books) == 0 {
		return providers.LookupBook{}, false
	}
	// Return the first candidate; caller can re-run selection if needed
	return books[0], true
}

// buildMonitoredEditions returns a minimal editions array with exactly one
// monitored edition targeting the candidate's foreignEditionId.
func buildMonitoredEditions(cand map[string]any) []any {
	fe := strings.TrimSpace(fmt.Sprint(cand["foreignEditionId"]))
	if fe == "" {
		return []any{}
	}
	ed := map[string]any{
		"foreignEditionId": fe,
		"monitored":        true,
	}
	return []any{ed}
}

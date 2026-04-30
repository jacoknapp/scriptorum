package providers

import (
	"bytes"
	"context"
	"crypto/tls"
	"database/sql"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"text/template"
	"time"
)

const (
	readarrLookupEndpoint     = "/api/v1/book/lookup"
	readarrAddEndpoint        = "/api/v1/book"
	readarrAddMethod          = "POST"
	readarrAddPayloadTemplate = `{
				"id": {{ if (index .Candidate "id") }}{{ toJSON (index .Candidate "id") }}{{ else }}0{{ end }},
				"title": {{ toJSON (index .Candidate "title") }},
				"authorTitle": {{ toJSON (index .Candidate "authorTitle") }},
				"seriesTitle": {{ toJSON (index .Candidate "seriesTitle") }},
				"disambiguation": {{ toJSON (index .Candidate "disambiguation") }},
				"overview": {{ toJSON (index .Candidate "overview") }},
				"authorId": {{ toJSON (index .Candidate "authorId") }},
				"foreignBookId": {{ toJSON (index .Candidate "foreignBookId") }},
				"foreignEditionId": {{ toJSON (index .Candidate "foreignEditionId") }},
				"titleSlug": {{ toJSON (index .Candidate "titleSlug") }},
				"monitored": {{ if (index .Candidate "monitored") }}{{ toJSON (index .Candidate "monitored") }}{{ else }}true{{ end }},
				"anyEditionOk": {{ if (index .Candidate "anyEditionOk") }}{{ toJSON (index .Candidate "anyEditionOk") }}{{ else }}true{{ end }},
				"ratings": {{ if (index .Candidate "ratings") }}{{ toJSON (index .Candidate "ratings") }}{{ else }}{"votes":0,"value":0}{{ end }},
				"releaseDate": {{ toJSON (index .Candidate "releaseDate") }},
				"pageCount": {{ if (index .Candidate "pageCount") }}{{ toJSON (index .Candidate "pageCount") }}{{ else }}0{{ end }},
				"genres": {{ if (index .Candidate "genres") }}{{ toJSON (index .Candidate "genres") }}{{ else }}[]{{ end }},
				"author": {{ toJSON (index .Candidate "author") }},
				"images": {{ if (index .Candidate "images") }}{{ toJSON (index .Candidate "images") }}{{ else }}[]{{ end }},
				"links": {{ if (index .Candidate "links") }}{{ toJSON (index .Candidate "links") }}{{ else }}[]{{ end }},
				"statistics": {{ if (index .Candidate "statistics") }}{{ toJSON (index .Candidate "statistics") }}{{ else }}{"bookFileCount":0,"bookCount":0,"totalBookCount":0,"sizeOnDisk":0}{{ end }},
				"added": {{ toJSON (index .Candidate "added") }},
				"addOptions": {
					"addType": {{ if and (index .Candidate "addOptions") (index (index .Candidate "addOptions") "addType") }}{{ toJSON (index (index .Candidate "addOptions") "addType") }}{{ else }}"automatic"{{ end }},
					"searchForNewBook": {{ if and (index .Candidate "addOptions") (index (index .Candidate "addOptions") "searchForNewBook") }}{{ toJSON (index (index .Candidate "addOptions") "searchForNewBook") }}{{ else }}true{{ end }},
					"monitor": "all",
					"monitored": true,
					"booksToMonitor": [],
					"searchForMissingBooks": {{ if .Opts.SearchForMissing }}true{{ else }}false{{ end }}
				},
				"remoteCover": {{ toJSON (index .Candidate "remoteCover") }},
				"lastSearchTime": {{ toJSON (index .Candidate "lastSearchTime") }},
				"editions": {{ if (index .Candidate "editions") }}{{ toJSON (index .Candidate "editions") }}{{ else }}[]{{ end }},
				"qualityProfileId": {{ if .Opts.QualityProfileID }}{{ .Opts.QualityProfileID }}{{ else }}{{ .Inst.DefaultQualityProfileID }}{{ end }},
				"rootFolderPath": "{{ .Opts.RootFolderPath }}",
				"tags": {{ toJSON .Opts.Tags }}
			}`
)

const apiVersionPrefix = "/api/v1"

type LookupBook struct {
	Title     string         `json:"title"`
	TitleSlug string         `json:"titleSlug"`
	Author    map[string]any `json:"author"`
	// Some Readarr responses include an `authors` array instead of a single `author` object.
	// Decode both so we can find an author id in either location.
	Authors []map[string]any `json:"authors"`
	// Readarr lookup may include authorId and authorTitle instead of author object
	AuthorId         int              `json:"authorId"`
	AuthorTitle      string           `json:"authorTitle"`
	ForeignBookId    string           `json:"foreignBookId"`
	ForeignEditionId string           `json:"foreignEditionId"`
	Identifiers      []map[string]any `json:"identifiers"`
	Editions         []any            `json:"editions"`
	RemoteCover      string           `json:"remoteCover"`
	RemotePoster     string           `json:"remotePoster"`
	CoverUrl         string           `json:"coverUrl"`
	Images           []struct {
		CoverType string `json:"coverType"`
		Url       string `json:"url"`
		RemoteUrl string `json:"remoteUrl"`
	} `json:"images"`
	// Additional enriched fields from Readarr API
	SeriesTitle    string                 `json:"seriesTitle"`
	Disambiguation string                 `json:"disambiguation"`
	Monitored      bool                   `json:"monitored"`
	AnyEditionOk   bool                   `json:"anyEditionOk"`
	Ratings        map[string]interface{} `json:"ratings"`
	ReleaseDate    string                 `json:"releaseDate"`
	PageCount      int                    `json:"pageCount"`
	Genres         []string               `json:"genres"`
	Links          []map[string]any       `json:"links"`
	Added          string                 `json:"added"`
	LastSearchTime string                 `json:"lastSearchTime"`
	Grabbed        bool                   `json:"grabbed"`
	ID             int                    `json:"id"`
	// Description/summary fields that might be available
	Description string `json:"description"`
	Overview    string `json:"overview"`
	Synopsis    string `json:"synopsis"`
	Summary     string `json:"summary"`
	Details     string `json:"details"`
	About       string `json:"about"`
}

type ReadarrInstance struct {
	BaseURL                 string
	APIKey                  string
	DefaultQualityProfileID int
	DefaultRootFolderPath   string
	DefaultTags             []string
	InsecureSkipVerify      bool
}

type Readarr struct {
	inst ReadarrInstance
	cl   *http.Client
	db   *sql.DB // Database connection for caching
}

// Debug enables printing of debug messages from this package when true.
// It is intended to be set by the application at startup based on user config.
var Debug bool = false

// sanitizeAndEnrichPayload applies defaults, shapes editions, normalizes authorId, and enriches the nested author object.
func (r *Readarr) sanitizeAndEnrichPayload(ctx context.Context, pmap map[string]any, opts AddOpts) map[string]any {
	if pmap == nil {
		return pmap
	}
	// Defaults
	// Resolve a valid quality profile id, preferring opts override, then configured/default, and validate against server
	resolveQID := func() int {
		// Helper to check existence of an id on server
		exists := func(id int) bool {
			if id == 0 {
				return false
			}
			if qps, err := r.fetchQualityProfiles(ctx); err == nil {
				if _, ok := qps[id]; ok {
					return true
				}
			}
			return false
		}
		if opts.QualityProfileID != 0 && exists(opts.QualityProfileID) {
			return opts.QualityProfileID
		}
		if qid := r.getValidQualityProfileID(ctx); qid != 0 {
			return qid
		}
		// last resort, try configured default even if not validated
		if r.inst.DefaultQualityProfileID != 0 {
			return r.inst.DefaultQualityProfileID
		}
		return 0
	}
	resolvedQID := resolveQID()
	// Keep top-level field consistent (some servers ignore this, but set it anyway)
	if resolvedQID != 0 {
		pmap["qualityProfileId"] = resolvedQID
	} else if pmap["qualityProfileId"] == nil || fmt.Sprint(pmap["qualityProfileId"]) == "" || fmt.Sprint(pmap["qualityProfileId"]) == "0" {
		// if still empty, clear it
		delete(pmap, "qualityProfileId")
	}
	if pmap["metadataProfileId"] == nil || fmt.Sprint(pmap["metadataProfileId"]) == "" || fmt.Sprint(pmap["metadataProfileId"]) == "0" {
		pmap["metadataProfileId"] = 1
	}
	if pmap["rootFolderPath"] == nil || fmt.Sprint(pmap["rootFolderPath"]) == "" {
		rp := r.getValidRootFolderPath(ctx, opts.RootFolderPath)
		if rp == "" {
			rp = r.getValidRootFolderPath(ctx, "")
		}
		if rp == "" {
			rp = r.inst.DefaultRootFolderPath
		}
		if rp != "" {
			pmap["rootFolderPath"] = rp
		}
	}
	if _, ok := pmap["monitored"]; !ok {
		pmap["monitored"] = true
	}
	if _, ok := pmap["addOptions"]; !ok {
		pmap["addOptions"] = map[string]any{"addType": "automatic", "monitor": "all", "monitored": true, "booksToMonitor": []any{}, "searchForMissingBooks": true, "searchForNewBook": true}
	} else {
		// Keep the selected book monitored when it is added.
		if ao, ok := pmap["addOptions"].(map[string]any); ok {
			ao["monitor"] = "all"
			ao["monitored"] = true
			pmap["addOptions"] = ao
		}
	}
	if pmap["tags"] == nil && len(r.inst.DefaultTags) > 0 {
		pmap["tags"] = r.inst.DefaultTags
	}

	// Editions shape
	if _, ok := pmap["editions"]; !ok || pmap["editions"] == nil {
		pmap["editions"] = []any{}
	}
	if eds, ok := pmap["editions"].([]any); ok && len(eds) == 0 {
		if fe := strings.TrimSpace(fmt.Sprint(pmap["foreignEditionId"])); fe != "" {
			pmap["editions"] = []any{map[string]any{"foreignEditionId": fe, "monitored": true}}
		}
	}

	// Nested author enrichment
	// Helper: convert various tag shapes (e.g., []string, []any) to []int when possible
	tagsToInts := func(v any) ([]int, bool) {
		if v == nil {
			return nil, false
		}
		out := []int{}
		switch t := v.(type) {
		case []int:
			if len(t) == 0 {
				return nil, false
			}
			return t, true
		case []any:
			for _, e := range t {
				switch x := e.(type) {
				case float64:
					out = append(out, int(x))
				case int:
					out = append(out, x)
				case string:
					if i, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
						out = append(out, i)
					}
				}
			}
		case []string:
			for _, s := range t {
				if i, err := strconv.Atoi(strings.TrimSpace(s)); err == nil {
					out = append(out, i)
				}
			}
		case string:
			// single string value
			if i, err := strconv.Atoi(strings.TrimSpace(t)); err == nil {
				out = append(out, i)
			}
		default:
			// try reflection-ish fallback for slices of numeric types encoded as []interface{}
			if sl, ok := t.([]interface{}); ok {
				for _, e := range sl {
					switch x := e.(type) {
					case float64:
						out = append(out, int(x))
					case int:
						out = append(out, x)
					case string:
						if i, err := strconv.Atoi(strings.TrimSpace(x)); err == nil {
							out = append(out, i)
						}
					}
				}
			}
		}
		if len(out) == 0 {
			return nil, false
		}
		return out, true
	}

	if av, ok := pmap["author"]; ok {
		// If author is explicitly null or not an object, treat as missing so we can inject later
		if av == nil {
			delete(pmap, "author")
		} else if am, ok2 := av.(map[string]any); ok2 {
			// Ensure qualityProfileId is present on the author (where Readarr expects it)
			if resolvedQID != 0 {
				// Overwrite when missing or invalid/zero
				if am["qualityProfileId"] == nil || fmt.Sprint(am["qualityProfileId"]) == "" || fmt.Sprint(am["qualityProfileId"]) == "0" {
					am["qualityProfileId"] = resolvedQID
				} else {
					// If set but not a valid int, coerce/replace
					switch v := am["qualityProfileId"].(type) {
					case float64:
						if int(v) == 0 {
							am["qualityProfileId"] = resolvedQID
						}
					case int:
						if v == 0 {
							am["qualityProfileId"] = resolvedQID
						}
					case string:
						if s := strings.TrimSpace(v); s == "" || s == "0" {
							am["qualityProfileId"] = resolvedQID
						}
					default:
						am["qualityProfileId"] = resolvedQID
					}
				}
			}
			if am["rootFolderPath"] == nil || am["rootFolderPath"] == "" {
				rp := r.getValidRootFolderPath(ctx, opts.RootFolderPath)
				if rp == "" {
					rp = r.getValidRootFolderPath(ctx, "")
				}
				if rp != "" {
					am["rootFolderPath"] = rp
				}
			}
			// Mirror into nested author.value when payload uses that shape
			if vm, ok := am["value"].(map[string]any); ok {
				// qualityProfileId
				if resolvedQID != 0 {
					if vm["qualityProfileId"] == nil || fmt.Sprint(vm["qualityProfileId"]) == "" || fmt.Sprint(vm["qualityProfileId"]) == "0" {
						vm["qualityProfileId"] = resolvedQID
					}
				}
				// metadataProfileId (default to 1 like top-level author)
				if vm["metadataProfileId"] == nil || fmt.Sprint(vm["metadataProfileId"]) == "" || fmt.Sprint(vm["metadataProfileId"]) == "0" {
					vm["metadataProfileId"] = 1
				}
				// rootFolderPath
				if vm["rootFolderPath"] == nil || fmt.Sprint(vm["rootFolderPath"]) == "" {
					rp := r.getValidRootFolderPath(ctx, opts.RootFolderPath)
					if rp == "" {
						rp = r.getValidRootFolderPath(ctx, "")
					}
					if rp != "" {
						vm["rootFolderPath"] = rp
					}
				}
				// Ensure addOptions.monitor is enforced in the nested value shape as well
				if _, ok := vm["addOptions"].(map[string]any); !ok {
					vm["addOptions"] = map[string]any{
						"monitor":        "none",
						"booksToMonitor": []any{},
						"monitored":      true,
					}
				} else {
					if vamo, ok := vm["addOptions"].(map[string]any); ok {
						vamo["monitor"] = "none"
						vm["addOptions"] = vamo
					}
				}
				// Also set author.value.monitorNewItems for Readarr variants expecting it at the author level
				vm["monitorNewItems"] = "none"
				am["value"] = vm
			}
			if am["foreignAuthorId"] == nil || am["foreignAuthorId"] == "" {
				if nm, _ := am["name"].(string); nm != "" {
					if Debug {
						fmt.Printf("DEBUG: Author missing foreignAuthorId, trying to resolve name='%s'\n", nm)
					}
					if fid := r.LookupForeignAuthorIDString(ctx, nm); fid != "" {
						if Debug {
							fmt.Printf("DEBUG: Found foreignAuthorId via lookup: %s\n", fid)
						}
						am["foreignAuthorId"] = fid
					} else {
						if Debug {
							fmt.Printf("DEBUG: Lookup failed, creating synthetic foreignAuthorId\n")
						}
						// Instead of using a fake foreign ID, try to find an existing author with a similar name
						// and use their foreign ID, or create a synthetic one based on the author name
						cleanedName := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(nm)), " ", "-")
						syntheticId := "local-" + cleanedName
						am["foreignAuthorId"] = syntheticId
						if Debug {
							fmt.Printf("DEBUG: Set synthetic foreignAuthorId: %s\n", syntheticId)
						}
					}
				} else if idv, ok := am["id"]; ok {
					// No name; fetch details by id
					var aid int
					switch t := idv.(type) {
					case float64:
						aid = int(t)
					case int:
						aid = t
					case string:
						if i, e := strconv.Atoi(strings.TrimSpace(t)); e == nil {
							aid = i
						}
					}
					if aid > 0 {
						if details, err := r.GetAuthorByID(ctx, aid); err == nil && details != nil {
							if fid, _ := details["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
								am["foreignAuthorId"] = fid
							}
							if nm2, _ := details["name"].(string); nm2 != "" {
								am["name"] = nm2
							}
						}
					}
				}
			}
			// Ensure author metadataProfileId
			if am["metadataProfileId"] == nil || fmt.Sprint(am["metadataProfileId"]) == "" || fmt.Sprint(am["metadataProfileId"]) == "0" {
				am["metadataProfileId"] = 1
			}
			// Ensure author tags, prefer payload/top-level tags when present
			if am["tags"] == nil {
				if tv, ok := pmap["tags"]; ok && tv != nil {
					if ints, ok2 := tagsToInts(tv); ok2 {
						am["tags"] = ints
					}
				} else if len(r.inst.DefaultTags) > 0 {
					if ints, ok2 := tagsToInts(r.inst.DefaultTags); ok2 {
						am["tags"] = ints
					}
				}
			}
			// Ensure author addOptions block exists
			if _, ok := am["addOptions"].(map[string]any); !ok {
				am["addOptions"] = map[string]any{
					// Ensure we do not auto-monitor author books when sending via Scriptorum
					"monitor":               "none",
					"booksToMonitor":        []any{},
					"monitored":             true,
					"searchForMissingBooks": opts.SearchForMissing,
				}
			} else {
				// If author.addOptions exists, override monitor to "none" to avoid enabling monitoring of new books
				if aao, ok := am["addOptions"].(map[string]any); ok {
					aao["monitor"] = "none"
					am["addOptions"] = aao
				}
			}
			// Ensure author.monitorNewItems exists at top-level of the author object
			am["monitorNewItems"] = "none"
			pmap["author"] = am
		}
	}

	// Normalize authorId
	if av, ok := pmap["authorId"]; ok {
		remove := false
		switch v := av.(type) {
		case nil:
			remove = true
		case float64:
			pmap["authorId"] = int(v)
		case int:
		case string:
			s := strings.TrimSpace(v)
			if s == "" || strings.EqualFold(s, "null") {
				remove = true
			} else if i, e := strconv.Atoi(s); e == nil {
				pmap["authorId"] = i
			} else {
				remove = true
			}
		default:
			remove = true
		}
		if remove {
			delete(pmap, "authorId")
		}
	}

	// Inject author from authorId if missing
	if _, hasAuthor := pmap["author"]; !hasAuthor {
		if aid, ok := pmap["authorId"].(int); ok && aid > 0 {
			am := map[string]any{"id": aid}
			if details, err := r.GetAuthorByID(ctx, aid); err == nil && details != nil {
				if fid, _ := details["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
					am["foreignAuthorId"] = fid
				}
				if nm, _ := details["name"].(string); nm != "" {
					am["name"] = nm
				}
			}
			if resolvedQID != 0 {
				am["qualityProfileId"] = resolvedQID
			}
			am["metadataProfileId"] = 1
			if rp := r.getValidRootFolderPath(ctx, ""); rp != "" {
				am["rootFolderPath"] = rp
			}
			// Carry over tags/addOptions to author
			if tv, ok := pmap["tags"]; ok && tv != nil {
				if ints, ok2 := tagsToInts(tv); ok2 {
					am["tags"] = ints
				}
			} else if len(r.inst.DefaultTags) > 0 {
				if ints, ok2 := tagsToInts(r.inst.DefaultTags); ok2 {
					am["tags"] = ints
				}
			}
			if _, ok := am["addOptions"].(map[string]any); !ok {
				am["addOptions"] = map[string]any{
					// When injecting author from authorId, avoid enabling monitoring of all author books
					"monitor":               "none",
					"booksToMonitor":        []any{},
					"monitored":             true,
					"searchForMissingBooks": opts.SearchForMissing,
				}
			} else {
				// Ensure injected author addOptions also force monitor to "none"
				if aao, ok := am["addOptions"].(map[string]any); ok {
					aao["monitor"] = "none"
					am["addOptions"] = aao
				}
			}
			pmap["author"] = am
		}
	}

	return pmap
}

func NewReadarrWithDB(i ReadarrInstance, db *sql.DB) *Readarr {
	r := &Readarr{inst: normalize(i), cl: &http.Client{Timeout: 12 * time.Second}, db: db}
	if r.inst.InsecureSkipVerify {
		tr := &http.Transport{TLSClientConfig: &tls.Config{InsecureSkipVerify: true}}
		r.cl = &http.Client{Timeout: 12 * time.Second, Transport: tr}
	}
	if db != nil {
		r.initCacheTables()
	}
	return r
}

func normalize(i ReadarrInstance) ReadarrInstance {
	i.BaseURL = strings.TrimSpace(i.BaseURL)
	// If user provided a host:port without scheme, default to http.
	if i.BaseURL != "" && !strings.HasPrefix(i.BaseURL, "http://") && !strings.HasPrefix(i.BaseURL, "https://") {
		i.BaseURL = "http://" + i.BaseURL
	}
	i.BaseURL = strings.TrimRight(i.BaseURL, "/")
	return i
}

func (r *Readarr) requestURL(path string, query url.Values) string {
	u := r.inst.BaseURL + path
	if len(query) == 0 {
		return u
	}
	encoded := query.Encode()
	if encoded == "" {
		return u
	}
	if strings.Contains(u, "?") {
		return u + "&" + encoded
	}
	return u + "?" + encoded
}

func (r *Readarr) newRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, string, error) {
	u := r.requestURL(path, query)
	req, err := http.NewRequestWithContext(ctx, method, u, body)
	if err != nil {
		return nil, u, err
	}
	if strings.TrimSpace(r.inst.APIKey) != "" {
		req.Header.Set("X-Api-Key", r.inst.APIKey)
	}
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")
	return req, u, nil
}

func (r *Readarr) newJSONRequest(ctx context.Context, method, path string, query url.Values, body io.Reader) (*http.Request, string, error) {
	req, u, err := r.newRequest(ctx, method, path, query, body)
	if err != nil {
		return nil, u, err
	}
	req.Header.Set("Content-Type", "application/json")
	return req, u, nil
}

func (r *Readarr) PingLookup(ctx context.Context) error {
	req, u, err := r.newRequest(ctx, http.MethodGet, readarrLookupEndpoint, url.Values{"term": {"test"}}, nil)
	if err != nil {
		return err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return readarrHTTPError("lookup ping failed", u, r.inst.APIKey, resp, body)
	}
	return nil
}

type Candidate map[string]any

type AddOpts struct {
	QualityProfileID int
	RootFolderPath   string
	SearchForMissing bool
	Tags             any
}

// LookupForeignAuthorIDString queries Readarr author lookup endpoint and returns the foreignAuthorId string (empty when not found)
func (r *Readarr) LookupForeignAuthorIDString(ctx context.Context, name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	req, _, err := r.newRequest(ctx, http.MethodGet, "/api/v1/author/lookup", url.Values{"term": {name}}, nil)
	if err != nil {
		return ""
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return ""
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return ""
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return ""
	}
	for _, a := range arr {
		if nm, _ := a["name"].(string); nm != "" && strings.EqualFold(strings.TrimSpace(nm), name) {
			if fid, _ := a["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
				return fid
			}
		}
	}
	// fallback to first with foreignAuthorId
	for _, a := range arr {
		if fid, _ := a["foreignAuthorId"].(string); strings.TrimSpace(fid) != "" {
			return fid
		}
	}
	return ""
}

// GetAuthorByID fetches an author object from Readarr and returns it as a map
func (r *Readarr) GetAuthorByID(ctx context.Context, id int) (map[string]any, error) {
	if id <= 0 {
		return nil, fmt.Errorf("invalid author id")
	}
	req, u, err := r.newRequest(ctx, http.MethodGet, fmt.Sprintf("/api/v1/author/%d", id), nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, readarrHTTPError("get author failed", u, r.inst.APIKey, resp, body)
	}
	body, _ := io.ReadAll(resp.Body)
	var out map[string]any
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (r *Readarr) AddBook(ctx context.Context, candidate Candidate, opts AddOpts) ([]byte, []byte, error) {
	tpl, err := template.New("payload").Funcs(template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}).Parse(readarrAddPayloadTemplate)
	if err != nil {
		return nil, nil, err
	}
	buf := &bytes.Buffer{}
	if err := tpl.Execute(buf, map[string]any{"Candidate": candidate, "Opts": opts, "Inst": r.inst}); err != nil {
		return nil, nil, err
	}

	payload := buf.Bytes()

	// Parse, sanitize, and enrich JSON payload consistently
	var pmap map[string]any
	if err := json.Unmarshal(payload, &pmap); err == nil {
		pmap = r.sanitizeAndEnrichPayload(context.Background(), pmap, opts)
		if b, err := json.Marshal(pmap); err == nil {
			payload = b
		}
	}
	req, u, err := r.newJSONRequest(ctx, readarrAddMethod, readarrAddEndpoint, url.Values{"includeAllAuthorBooks": {"false"}}, bytes.NewReader(payload))
	if err != nil {
		return payload, nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return payload, nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return payload, respBody, readarrHTTPError("add book failed", u, r.inst.APIKey, resp, respBody)
	}
	return payload, respBody, nil
}

// AddBookRaw accepts a raw JSON payload (full Readarr book schema), performs
// the same sanitization as AddBook (notably removing null authorId), and
// POSTs it to the configured AddEndpoint. Returns the sent payload and the
// Readarr response body.
func (r *Readarr) AddBookRaw(ctx context.Context, raw json.RawMessage) ([]byte, []byte, error) {
	// Sanitize authorId like AddBook
	var pmap map[string]any
	payload := raw
	if err := json.Unmarshal(raw, &pmap); err == nil {
		pmap = r.sanitizeAndEnrichPayload(context.Background(), pmap, AddOpts{})
		if b, err := json.Marshal(pmap); err == nil {
			payload = b
		}
	}

	req, u, err := r.newJSONRequest(ctx, readarrAddMethod, readarrAddEndpoint, nil, bytes.NewReader(payload))
	if err != nil {
		return payload, nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return payload, nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return payload, respBody, readarrHTTPError("add book (raw) failed", u, r.inst.APIKey, resp, respBody)
	}
	return payload, respBody, nil
}

// GetBookByAddPayload resolves a matching catalog book for the payload that would
// be sent for creation. Readarr's catalog endpoint does not accept the add payload
// directly, so we list the current books and match by foreign ids or title data.
func (r *Readarr) GetBookByAddPayload(ctx context.Context, payload []byte) (int, []byte, error) {
	matchQuery, err := decodeAddPayloadMatch(payload)
	if err != nil {
		return 0, nil, err
	}
	books, err := r.ListBooks(ctx)
	if err != nil {
		return 0, nil, err
	}
	for _, book := range books {
		if addPayloadMatchesCatalogBook(matchQuery, book) {
			body, err := json.Marshal(book)
			if err != nil {
				return book.ID, nil, nil
			}
			return book.ID, body, nil
		}
	}
	if Debug {
		fmt.Printf("DEBUG: GetBookByAddPayload: no catalog match found for payload: %s\n", string(payload))
	}
	return 0, nil, fmt.Errorf("existing book id not found in catalog")
}

type addPayloadMatch struct {
	ForeignBookID    string
	ForeignEditionID string
	Title            string
	AuthorTitle      string
}

func decodeAddPayloadMatch(payload []byte) (addPayloadMatch, error) {
	var raw map[string]any
	if err := json.Unmarshal(payload, &raw); err != nil {
		return addPayloadMatch{}, err
	}
	return addPayloadMatch{
		ForeignBookID:    readarrStringValue(raw, "foreignBookId"),
		ForeignEditionID: readarrStringValue(raw, "foreignEditionId"),
		Title:            readarrStringValue(raw, "title"),
		AuthorTitle:      readarrStringValue(raw, "authorTitle"),
	}, nil
}

func readarrStringValue(raw map[string]any, key string) string {
	v, ok := raw[key]
	if !ok || v == nil {
		return ""
	}
	return strings.TrimSpace(fmt.Sprint(v))
}

func addPayloadMatchesCatalogBook(match addPayloadMatch, book CatalogBook) bool {
	if match.ForeignBookID != "" && strings.EqualFold(strings.TrimSpace(book.ForeignBookId), match.ForeignBookID) {
		return true
	}
	if match.ForeignEditionID != "" {
		if strings.EqualFold(strings.TrimSpace(book.ForeignEditionId), match.ForeignEditionID) {
			return true
		}
		for _, rawEdition := range book.Editions {
			edition, ok := rawEdition.(map[string]any)
			if !ok {
				continue
			}
			if strings.EqualFold(strings.TrimSpace(fmt.Sprint(edition["foreignEditionId"])), match.ForeignEditionID) {
				return true
			}
		}
	}
	if match.Title != "" && strings.EqualFold(strings.TrimSpace(book.Title), match.Title) {
		return match.AuthorTitle == "" || strings.EqualFold(strings.TrimSpace(book.AuthorTitle), match.AuthorTitle)
	}
	return false
}

// MonitorBooks sends a PUT to /api/v1/book/monitor with the provided readarr ids
// and monitored flag.
func (r *Readarr) MonitorBooks(ctx context.Context, ids []int, monitored bool) ([]byte, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids provided")
	}
	// build payload
	payload := map[string]any{
		"bookIds":   ids,
		"monitored": monitored,
	}
	b, _ := json.Marshal(payload)
	req, u, err := r.newJSONRequest(ctx, http.MethodPut, "/api/v1/book/monitor", nil, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return body, readarrHTTPError("monitor update failed", u, r.inst.APIKey, resp, body)
	}
	return body, nil
}

// SearchBooks queues a Readarr book search command for the provided book ids.
func (r *Readarr) SearchBooks(ctx context.Context, ids []int) ([]byte, error) {
	if len(ids) == 0 {
		return nil, fmt.Errorf("no ids provided")
	}
	payload := map[string]any{
		"name":    "BookSearch",
		"bookIds": ids,
	}
	b, _ := json.Marshal(payload)
	req, u, err := r.newJSONRequest(ctx, http.MethodPost, "/api/v1/command", nil, bytes.NewReader(b))
	if err != nil {
		return nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if resp.StatusCode >= 400 {
		return body, readarrHTTPError("book search command failed", u, r.inst.APIKey, resp, body)
	}
	return body, nil
}

// ----- Lookup & matching (ISBN13 -> ISBN10 -> ASIN) -----

func (r *Readarr) LookupByTerm(ctx context.Context, term string) ([]LookupBook, error) {
	// Check cache first
	cacheKey := "lookup:" + strings.ToLower(term)
	if cached, found := r.getCachedData(cacheKey, "lookup"); found {
		var books []LookupBook
		if err := json.Unmarshal([]byte(cached), &books); err == nil {
			return books, nil
		}
	}

	req, u, err := r.newRequest(ctx, http.MethodGet, readarrLookupEndpoint, url.Values{"term": {term}}, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, readarrHTTPError("lookup failed", u, r.inst.APIKey, resp, body)
	}
	// Read the entire response body first
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %s", sanitizeReadarrText(err.Error(), r.inst.APIKey))
	}

	// Try to decode as JSON
	var arr []LookupBook
	if err := json.Unmarshal(body, &arr); err != nil {
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		// redact apikey in URL if present
		safeURL := redactAPIKey(u)
		return nil, fmt.Errorf("invalid JSON (ct=%s) (HTTP %s) from %s: %s", resp.Header.Get("Content-Type"), resp.Status, safeURL, bodyStr)
	}

	// Cache the results for 1 hour
	if data, err := json.Marshal(arr); err == nil {
		r.setCachedData(cacheKey, "lookup", string(data), time.Hour)
	}

	// Debug: dump the full JSON response
	if Debug {
		fmt.Printf("DEBUG: Full Readarr lookup JSON: %s\n", string(body))
	}
	return arr, nil
}

// GetBookDetails fetches detailed book information by ID from Readarr
func (r *Readarr) GetBookDetails(ctx context.Context, bookID int) (map[string]interface{}, error) {
	// Check cache first
	cacheKey := fmt.Sprintf("book_details:%d", bookID)
	if cached, found := r.getCachedData(cacheKey, "book_details"); found {
		var details map[string]interface{}
		if err := json.Unmarshal([]byte(cached), &details); err == nil {
			return details, nil
		}
	}

	// Try both endpoints - overview and regular book detail
	endpoints := []string{
		fmt.Sprintf("/api/v1/book/%d/overview", bookID),
		fmt.Sprintf("/api/v1/book/%d", bookID),
	}

	for _, endpoint := range endpoints {
		req, _, err := r.newRequest(ctx, http.MethodGet, endpoint, nil, nil)
		if err != nil {
			continue
		}

		resp, err := r.cl.Do(req)
		if err != nil {
			continue // Try next endpoint
		}
		defer resp.Body.Close()

		if resp.StatusCode >= 400 {
			continue // Try next endpoint
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			continue
		}

		var details map[string]interface{}
		if err := json.Unmarshal(body, &details); err != nil {
			continue
		}

		// Cache the results for 1 hour
		if data, err := json.Marshal(details); err == nil {
			r.setCachedData(cacheKey, "book_details", string(data), time.Hour)
		}

		// Debug: dump the full JSON response
		if Debug {
			fmt.Printf("DEBUG: Full Readarr book details JSON from %s: %s\n", endpoint, string(body))
		}

		return details, nil
	}

	return nil, fmt.Errorf("failed to fetch book details for ID %d", bookID)
}

// FindAuthorIDByName searches Readarr for an author by name. If found, returns the id.
// If no author is found, returns (0, nil). Returns a non-nil error only for transport/parse errors.
func (r *Readarr) FindAuthorIDByName(ctx context.Context, name string) (int, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, nil
	}

	// Check cache first
	if cachedID, found := r.getCachedAuthor(name); found {
		return cachedID, nil
	}

	// Use the author lookup endpoint
	req, u, err := r.newRequest(ctx, http.MethodGet, "/api/v1/author/lookup", url.Values{"term": {name}}, nil)
	if err != nil {
		return 0, err
	}

	resp, err := r.cl.Do(req)
	if err != nil {
		return 0, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return 0, readarrHTTPError("author lookup failed", u, r.inst.APIKey, resp, body)
	}
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0, fmt.Errorf("failed to read response body: %s", sanitizeReadarrText(err.Error(), r.inst.APIKey))
	}
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return 0, fmt.Errorf("invalid JSON from author lookup: %v", err)
	}

	var foundID int
	for _, a := range arr {
		if nm, _ := a["name"].(string); nm != "" && strings.EqualFold(strings.TrimSpace(nm), name) {
			if idv, ok := a["id"]; ok {
				switch v := idv.(type) {
				case float64:
					foundID = int(v)
				case int:
					foundID = v
				case int64:
					foundID = int(v)
				case string:
					if i, err := strconv.Atoi(v); err == nil {
						foundID = i
					}
				}
				if foundID > 0 {
					r.setCachedAuthor(name, foundID)
					return foundID, nil
				}
			}
		}
	}
	// no exact match found; if we have results, prefer the first with an id
	for _, a := range arr {
		if idv, ok := a["id"]; ok {
			switch v := idv.(type) {
			case float64:
				foundID = int(v)
			case int:
				foundID = v
			case int64:
				foundID = int(v)
			case string:
				if i, err := strconv.Atoi(v); err == nil {
					foundID = i
				}
			}
			if foundID > 0 {
				r.setCachedAuthor(name, foundID)
				return foundID, nil
			}
		}
	}
	return 0, nil
}

// redactAPIKey hides apikey query param values from logs/errors
func redactAPIKey(u string) string {
	if u == "" {
		return u
	}
	if strings.Contains(u, "apikey=") {
		// replace value after apikey=
		return regexp.MustCompile(`([?&]apikey=)[^&]+`).ReplaceAllString(u, "$1***")
	}
	return u
}

func sanitizeReadarrText(text, apiKey string) string {
	text = strings.TrimSpace(redactAPIKey(text))
	if apiKey == "" {
		return text
	}
	text = strings.ReplaceAll(text, apiKey, "***")
	if escaped := url.QueryEscape(apiKey); escaped != "" && escaped != apiKey {
		text = strings.ReplaceAll(text, escaped, "***")
	}
	return text
}

func readarrTransportError(u, apiKey string, err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("request to %s failed: %s", redactAPIKey(u), sanitizeReadarrText(err.Error(), apiKey))
}

func readarrHTTPError(prefix, u, apiKey string, resp *http.Response, body []byte) error {
	bodyStr := sanitizeReadarrText(string(body), apiKey)
	if len(bodyStr) > 200 {
		bodyStr = bodyStr[:200] + "..."
	}
	return fmt.Errorf("%s (HTTP %s) from %s: %s", prefix, resp.Status, redactAPIKey(u), bodyStr)
}

// fetchQualityProfiles queries Readarr for quality profiles and returns a map[id->name]
func (r *Readarr) fetchQualityProfiles(ctx context.Context) (map[int]string, error) {
	req, u, err := r.newRequest(ctx, http.MethodGet, "/api/v1/qualityprofile", nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, readarrHTTPError("quality profile lookup failed", u, r.inst.APIKey, resp, body)
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	out := make(map[int]string)
	for _, p := range arr {
		if idv, ok := p["id"]; ok {
			var id int
			switch v := idv.(type) {
			case float64:
				id = int(v)
			case int:
				id = v
			case int64:
				id = int(v)
			case string:
				if i, err := strconv.Atoi(v); err == nil {
					id = i
				}
			}
			if id == 0 {
				continue
			}
			name, _ := p["name"].(string)
			out[id] = name
		}
	}
	return out, nil
}

// fetchQualityProfileByID queries Readarr for a single quality profile by id.
// Returns (name, found, error). If Readarr returns 404 the profile is not found and found=false.
func (r *Readarr) fetchQualityProfileByID(ctx context.Context, id int) (string, bool, error) {
	req, u, err := r.newRequest(ctx, http.MethodGet, fmt.Sprintf("%s/qualityprofile/%d", apiVersionPrefix, id), nil, nil)
	if err != nil {
		return "", false, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return "", false, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusNotFound {
		return "", false, nil
	}
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return "", false, readarrHTTPError("quality profile lookup failed", u, r.inst.APIKey, resp, body)
	}
	body, _ := io.ReadAll(resp.Body)
	var obj map[string]any
	if err := json.Unmarshal(body, &obj); err != nil {
		return "", false, err
	}
	name, _ := obj["name"].(string)
	return name, true, nil
}

// GetQualityProfilesByID fetches quality profiles by querying the per-id endpoint
// starting at id=1 and counting up until a 404 is received.
func (r *Readarr) GetQualityProfilesByID(ctx context.Context) (map[int]string, error) {
	out := make(map[int]string)
	for id := 1; ; id++ {
		name, found, err := r.fetchQualityProfileByID(ctx, id)
		if err != nil {
			return nil, err
		}
		if !found {
			// stop when a 404 is encountered
			break
		}
		out[id] = name
	}
	return out, nil
}

// fetchRootFolders queries Readarr for root folders and returns a slice of paths
func (r *Readarr) fetchRootFolders(ctx context.Context) ([]string, error) {
	req, u, err := r.newRequest(ctx, http.MethodGet, "/api/v1/rootfolder", nil, nil)
	if err != nil {
		return nil, err
	}
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		body, _ := io.ReadAll(resp.Body)
		return nil, readarrHTTPError("root folder lookup failed", u, r.inst.APIKey, resp, body)
	}
	body, _ := io.ReadAll(resp.Body)
	var arr []map[string]any
	if err := json.Unmarshal(body, &arr); err != nil {
		return nil, err
	}
	out := []string{}
	for _, rj := range arr {
		if p, ok := rj["path"].(string); ok && p != "" {
			out = append(out, p)
		}
	}
	return out, nil
}

// getValidQualityProfileID returns a quality profile id to use: prefer configured DefaultQualityProfileID if present on server, otherwise return first available id
func (r *Readarr) getValidQualityProfileID(ctx context.Context) int {
	// prefer configured
	if r.inst.DefaultQualityProfileID != 0 {
		// verify it exists
		if qps, err := r.fetchQualityProfiles(ctx); err == nil {
			if _, ok := qps[r.inst.DefaultQualityProfileID]; ok {
				return r.inst.DefaultQualityProfileID
			}
			// fallback to first available
			for id := range qps {
				return id
			}
		}
	} else {
		if qps, err := r.fetchQualityProfiles(ctx); err == nil {
			for id := range qps {
				return id
			}
		}
	}
	return 0
}

// getValidRootFolderPath returns a root folder path to use: prefer provided override, otherwise configured DefaultRootFolderPath if it exists on server, otherwise first available
func (r *Readarr) getValidRootFolderPath(ctx context.Context, override string) string {
	if override != "" {
		// verify override exists
		if rfs, err := r.fetchRootFolders(ctx); err == nil {
			for _, p := range rfs {
				if p == override {
					return override
				}
			}
		}
	}
	if r.inst.DefaultRootFolderPath != "" {
		if rfs, err := r.fetchRootFolders(ctx); err == nil {
			for _, p := range rfs {
				if p == r.inst.DefaultRootFolderPath {
					return r.inst.DefaultRootFolderPath
				}
			}
		}
	}
	if rfs, err := r.fetchRootFolders(ctx); err == nil {
		if len(rfs) > 0 {
			return rfs[0]
		}
	}
	return ""
}

// ----- Database caching methods -----

func (r *Readarr) initCacheTables() {
	if r.db == nil {
		return
	}
	// Create cache tables if they don't exist
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_cache (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			cache_key TEXT UNIQUE NOT NULL,
			cache_type TEXT NOT NULL,
			data TEXT NOT NULL,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			expires_at DATETIME
		)
	`)
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_authors (
			id INTEGER PRIMARY KEY,
			name TEXT NOT NULL,
			readarr_id INTEGER,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP
		)
	`)
	r.db.Exec(`
		CREATE TABLE IF NOT EXISTS readarr_books (
			id INTEGER PRIMARY KEY AUTOINCREMENT,
			title TEXT NOT NULL,
			author_id INTEGER,
			isbn13 TEXT,
			isbn10 TEXT,
			asin TEXT,
			readarr_data TEXT,
			created_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			updated_at DATETIME DEFAULT CURRENT_TIMESTAMP,
			FOREIGN KEY (author_id) REFERENCES readarr_authors(id)
		)
	`)
}

func (r *Readarr) getCachedData(cacheKey, cacheType string) (string, bool) {
	if r.db == nil {
		return "", false
	}
	var data string
	err := r.db.QueryRow(`
		SELECT data FROM readarr_cache 
		WHERE cache_key = ? AND cache_type = ? AND (expires_at IS NULL OR expires_at > CURRENT_TIMESTAMP)
	`, cacheKey, cacheType).Scan(&data)
	if err != nil {
		return "", false
	}
	return data, true
}

func (r *Readarr) setCachedData(cacheKey, cacheType, data string, ttl time.Duration) {
	if r.db == nil {
		return
	}
	var expiresAt *time.Time
	if ttl > 0 {
		t := time.Now().Add(ttl)
		expiresAt = &t
	}
	r.db.Exec(`
		INSERT OR REPLACE INTO readarr_cache (cache_key, cache_type, data, expires_at)
		VALUES (?, ?, ?, ?)
	`, cacheKey, cacheType, data, expiresAt)
}

func (r *Readarr) getCachedAuthor(name string) (int, bool) {
	if r.db == nil {
		return 0, false
	}
	var readarrID int
	err := r.db.QueryRow(`
		SELECT readarr_id FROM readarr_authors 
		WHERE name = ? AND readarr_id IS NOT NULL
	`, strings.ToLower(name)).Scan(&readarrID)
	if err != nil {
		return 0, false
	}
	return readarrID, true
}

func (r *Readarr) setCachedAuthor(name string, readarrID int) {
	if r.db == nil {
		return
	}
	r.db.Exec(`
		INSERT OR REPLACE INTO readarr_authors (name, readarr_id, updated_at)
		VALUES (?, ?, CURRENT_TIMESTAMP)
	`, strings.ToLower(name), readarrID)
}

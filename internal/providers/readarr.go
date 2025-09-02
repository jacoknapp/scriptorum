package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strings"
	"text/template"
	"time"
)

const apiVersionPrefix = "/api/v1"

type ReadarrInstance struct {
	BaseURL                 string
	APIKey                  string
	LookupEndpoint          string
	AddEndpoint             string
	AddMethod               string
	AddPayloadTemplate      string
	DefaultQualityProfileID int
	DefaultRootFolderPath   string
	DefaultTags             []string
}

type Readarr struct {
	inst ReadarrInstance
	cl   *http.Client
}

func NewReadarr(i ReadarrInstance) *Readarr {
	return &Readarr{inst: normalize(i), cl: &http.Client{Timeout: 12 * time.Second}}
}

func normalize(i ReadarrInstance) ReadarrInstance {
	i.BaseURL = strings.TrimRight(i.BaseURL, "/")
	if !strings.Contains(i.LookupEndpoint, apiVersionPrefix) {
		i.LookupEndpoint = apiVersionPrefix + "/book/lookup"
	}
	if !strings.Contains(i.AddEndpoint, apiVersionPrefix) {
		i.AddEndpoint = apiVersionPrefix + "/book"
	}
	if strings.TrimSpace(i.AddMethod) == "" {
		i.AddMethod = http.MethodPost
	}
	return i
}

func (r *Readarr) PingLookup(ctx context.Context) error {
	u := r.inst.BaseURL + r.inst.LookupEndpoint + "?term=" + url.QueryEscape("test")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	resp, err := r.cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errStatus(resp.Status)
	}
	return nil
}

type Candidate map[string]any

type AddOpts struct {
	QualityProfileID int
	RootFolderPath   string
	SearchForMissing bool
	Tags             []string
}

func (r *Readarr) AddBook(ctx context.Context, candidate Candidate, opts AddOpts) ([]byte, []byte, error) {
	tpl, err := template.New("payload").Funcs(template.FuncMap{
		"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) },
	}).Parse(r.inst.AddPayloadTemplate)
	if err != nil {
		return nil, nil, err
	}
	buf := &bytes.Buffer{}
	if err := tpl.Execute(buf, map[string]any{"Candidate": candidate, "Opts": opts}); err != nil {
		return nil, nil, err
	}

	payload := buf.Bytes()
	u := r.inst.BaseURL + r.inst.AddEndpoint
	req, _ := http.NewRequestWithContext(ctx, r.inst.AddMethod, u, bytes.NewReader(payload))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	resp, err := r.cl.Do(req)
	if err != nil {
		return payload, nil, err
	}
	defer resp.Body.Close()
	respBody, _ := io.ReadAll(resp.Body)
	return payload, respBody, nil
}

// ----- Lookup & matching (ISBN13 -> ISBN10 -> ASIN) -----

type LookupBook struct {
	Title       string           `json:"title"`
	TitleSlug   string           `json:"titleSlug"`
	Author      map[string]any   `json:"author"`
	Identifiers []map[string]any `json:"identifiers"`
	Editions    []any            `json:"editions"`
}

func (r *Readarr) LookupByTerm(ctx context.Context, term string) ([]LookupBook, error) {
	u := r.inst.BaseURL + r.inst.LookupEndpoint + "?term=" + url.QueryEscape(term)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errStatus(resp.Status)
	}
	var arr []LookupBook
	dec := json.NewDecoder(resp.Body)
	if err := dec.Decode(&arr); err != nil {
		return nil, err
	}
	return arr, nil
}

var nonDigit = regexp.MustCompile(`[^0-9Xx]`)

func cleanISBN(s string) string { return strings.ToUpper(nonDigit.ReplaceAllString(s, "")) }
func cleanASIN(s string) string { return strings.ToUpper(strings.TrimSpace(s)) }

func hasIdent(b LookupBook, kind, value string) bool {
	v := strings.ToUpper(value)
	for _, id := range b.Identifiers {
		kt, _ := id["identifierType"].(string)
		vv, _ := id["value"].(string)
		if strings.EqualFold(kt, kind) && strings.ToUpper(vv) == v {
			return true
		}
	}
	return false
}

func (r *Readarr) SelectCandidate(list []LookupBook, isbn13, isbn10, asin string) (Candidate, bool) {
	c13 := cleanISBN(isbn13)
	c10 := cleanISBN(isbn10)
	ca := cleanASIN(asin)

	pick := func(test func(LookupBook) bool) (Candidate, bool) {
		for _, b := range list {
			if test(b) {
				return Candidate{"title": b.Title, "titleSlug": b.TitleSlug, "author": b.Author, "editions": b.Editions}, true
			}
		}
		return nil, false
	}

	if c13 != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ISBN13", c13) }); ok {
			return cand, true
		}
	}
	if c10 != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ISBN10", c10) }); ok {
			return cand, true
		}
	}
	if ca != "" {
		if cand, ok := pick(func(b LookupBook) bool { return hasIdent(b, "ASIN", ca) }); ok {
			return cand, true
		}
	}
	return nil, false
}

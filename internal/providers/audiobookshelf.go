package providers

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"text/template"
	"time"
)

type ABS struct {
	base           string
	token          string
	cl             *http.Client
	searchEndpoint string
}

func NewABS(base, token, searchEndpoint string) *ABS {
	nb := normalizeBase(base)
	return &ABS{base: strings.TrimRight(nb, "/"), token: token, searchEndpoint: defaultIfEmpty(searchEndpoint, "/api/search?query={{urlquery .Term}}"), cl: &http.Client{Timeout: 8 * time.Second}}
}

func defaultIfEmpty(v, d string) string {
	if strings.TrimSpace(v) == "" {
		return d
	}
	return v
}

func (a *ABS) Ping(ctx context.Context) error {
	if strings.TrimSpace(a.base) == "" {
		return errStatus("audiobookshelf base URL is empty; set the Base URL (e.g., http://host:port)")
	}
	// Use /api/authorize with POST as per ABS docs; requires Bearer token
	req, _ := http.NewRequestWithContext(ctx, http.MethodPost, a.base+"/api/authorize", nil)
	a.addAuth(req)
	resp, err := a.cl.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return errStatus(resp.Status)
	}
	return nil
}

type errStatus string

func (e errStatus) Error() string { return string(e) }

type absSearchResp struct {
	Results []struct{ Title, Author, Asin, Id string } `json:"results"`
}

func (a *ABS) HasTitle(ctx context.Context, term string) (bool, error) {
	u := a.renderSearchURL(term)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	a.addAuth(req)
	resp, err := a.cl.Do(req)
	if err != nil {
		return false, nil
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return false, nil
	}
	var r absSearchResp
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return len(r.Results) > 0, nil
}

func (a *ABS) addAuth(req *http.Request) {
	tok := strings.TrimSpace(a.token)
	if tok == "" {
		return
	}
	// ABS expects Authorization: Bearer <token>
	if !strings.HasPrefix(strings.ToLower(tok), "bearer ") {
		tok = "Bearer " + tok
	}
	req.Header.Set("Authorization", tok)
}

func (a *ABS) renderSearchURL(term string) string {
	ep := a.searchEndpoint
	// Render as Go template if it contains template markers
	if strings.Contains(ep, "{{") {
		tpl, err := template.New("abs_ep").Funcs(template.FuncMap{"urlquery": url.QueryEscape}).Parse(ep)
		if err == nil {
			var b bytes.Buffer
			_ = tpl.Execute(&b, map[string]any{"Term": term})
			ep = b.String()
		}
	} else {
		ep = strings.ReplaceAll(ep, "{term}", url.QueryEscape(term))
		ep = strings.ReplaceAll(ep, "{{q}}", url.QueryEscape(term))
	}
	if !strings.HasPrefix(ep, "/") {
		ep = "/" + ep
	}
	return a.base + ep
}

func normalizeBase(b string) string {
	s := strings.TrimSpace(b)
	if s == "" {
		return ""
	}
	if strings.HasPrefix(s, "http://") || strings.HasPrefix(s, "https://") {
		return s
	}
	// Assume http if scheme missing
	return "http://" + s
}

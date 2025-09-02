package providers

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"time"
)

type ABS struct{
	base string
	token string
	cl *http.Client
	searchEndpoint string
}

func NewABS(base, token, searchEndpoint string) *ABS {
	return &ABS{base: strings.TrimRight(base, "/"), token: token, searchEndpoint: defaultIfEmpty(searchEndpoint, "/api/search?query={{q}}"), cl: &http.Client{Timeout: 8*time.Second}}
}

func defaultIfEmpty(v, d string) string { if strings.TrimSpace(v)=="" { return d }; return v }

func (a *ABS) Ping(ctx context.Context) error {
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, a.base+"/api/about", nil)
	if a.token != "" { req.Header.Set("Authorization", "Bearer "+a.token) }
	resp, err := a.cl.Do(req); if err != nil { return err }
	defer resp.Body.Close()
	if resp.StatusCode >= 400 { return errStatus(resp.Status) }
	return nil
}

type errStatus string
func (e errStatus) Error() string { return string(e) }

type absSearchResp struct { Results []struct{ Title, Author, Asin, Id string } `json:"results"` }

func (a *ABS) HasTitle(ctx context.Context, term string) (bool, error) {
	u := a.base + "/api/search?query=" + strings.ReplaceAll(term, " ", "+")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if a.token != "" { req.Header.Set("Authorization","Bearer "+a.token) }
	resp, err := a.cl.Do(req); if err != nil { return false, nil }
	defer resp.Body.Close()
	if resp.StatusCode >= 400 { return false, nil }
	var r absSearchResp
	_ = json.NewDecoder(resp.Body).Decode(&r)
	return len(r.Results) > 0, nil
}

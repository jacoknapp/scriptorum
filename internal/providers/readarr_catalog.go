package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
)

type ReadarrStatistics struct {
	BookFileCount  int `json:"bookFileCount"`
	BookCount      int `json:"bookCount"`
	TotalBookCount int `json:"totalBookCount"`
}

type CatalogBook struct {
	LookupBook
	Statistics ReadarrStatistics `json:"statistics"`
}

func (r *Readarr) ListBooks(ctx context.Context) ([]CatalogBook, error) {
	u := r.inst.BaseURL + "/api/v1/book?apikey=" + url.QueryEscape(r.inst.APIKey)
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	req.Header.Set("X-Api-Key", r.inst.APIKey)
	req.Header.Set("User-Agent", "Scriptorum/1.0")
	req.Header.Set("Accept", "application/json")

	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode >= 400 {
		bodyStr := string(body)
		if len(bodyStr) > 200 {
			bodyStr = bodyStr[:200] + "..."
		}
		return nil, fmt.Errorf("list books failed (HTTP %s): %s", resp.Status, bodyStr)
	}

	var out []CatalogBook
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from catalog listing: %w", err)
	}
	return out, nil
}

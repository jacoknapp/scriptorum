package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
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
	req, u, err := r.newRequest(ctx, http.MethodGet, "/api/v1/book", nil, nil)
	if err != nil {
		return nil, err
	}

	resp, err := r.cl.Do(req)
	if err != nil {
		return nil, readarrTransportError(u, r.inst.APIKey, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("failed to read response body: %s", sanitizeReadarrText(err.Error(), r.inst.APIKey))
	}
	if resp.StatusCode >= 400 {
		return nil, readarrHTTPError("list books failed", u, r.inst.APIKey, resp, body)
	}

	var out []CatalogBook
	if err := json.Unmarshal(body, &out); err != nil {
		return nil, fmt.Errorf("invalid JSON from catalog listing: %w", err)
	}
	return out, nil
}

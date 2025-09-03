package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"time"
)

type OpenLibrary struct{ cl *http.Client }

func NewOpenLibrary() *OpenLibrary { return &OpenLibrary{cl: &http.Client{Timeout: 8 * time.Second}} }

type OLDoc struct {
	Title      string   `json:"title"`
	AuthorName []string `json:"author_name"`
	ISBN10     []string `json:"isbn"`
	ISBN13     []string `json:"isbn13"`
	CoverId    int      `json:"cover_i"`
}
type OLResp struct {
	Docs []OLDoc `json:"docs"`
}

type BookItem struct {
	ASIN        string
	Title       string
	Authors     []string
	ISBN10      string
	ISBN13      string
	CoverSmall  string
	CoverMedium string
}

func (ol *OpenLibrary) Search(ctx context.Context, q string, limit, page int) ([]BookItem, error) {
	if q == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 10
	}
	if page <= 0 {
		page = 1
	}
	u := "https://openlibrary.org/search.json?q=" + url.QueryEscape(q) + "&limit=" + strconv.Itoa(limit)
	if page > 1 {
		u += "&page=" + strconv.Itoa(page)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := ol.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out OLResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	var items []BookItem
	for _, d := range out.Docs {
		var i10, i13 string
		if len(d.ISBN10) > 0 {
			i10 = d.ISBN10[0]
		}
		if len(d.ISBN13) > 0 {
			i13 = d.ISBN13[0]
		}
		cover := ""
		if d.CoverId != 0 {
			cover = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", d.CoverId)
		}
		items = append(items, BookItem{Title: d.Title, Authors: d.AuthorName, ISBN10: i10, ISBN13: i13, CoverSmall: cover, CoverMedium: cover})
	}
	return items, nil
}

package providers

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"time"
)

type OpenLibrary struct {
	cl      *http.Client
	baseURL string
}

func NewOpenLibrary() *OpenLibrary {
	return &OpenLibrary{
		cl:      &http.Client{Timeout: 8 * time.Second},
		baseURL: "https://openlibrary.org",
	}
}

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

type OLSubjectAuthor struct {
	Name string `json:"name"`
}

type OLSubjectWork struct {
	Title           string            `json:"title"`
	Authors         []OLSubjectAuthor `json:"authors"`
	CoverID         int               `json:"cover_id"`
	CoverEditionKey string            `json:"cover_edition_key"`
}

type OLSubjectResp struct {
	Works []OLSubjectWork `json:"works"`
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
	u := ol.apiURL("/search.json?q=" + url.QueryEscape(q) + "&limit=" + strconv.Itoa(limit))
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

func (ol *OpenLibrary) SubjectWorks(ctx context.Context, subject string, limit int) ([]BookItem, error) {
	subject = strings.TrimSpace(subject)
	if subject == "" {
		return nil, nil
	}
	if limit <= 0 {
		limit = 6
	}
	u := ol.apiURL("/subjects/" + url.PathEscape(subject) + ".json?limit=" + strconv.Itoa(limit))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := ol.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out OLSubjectResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	items := make([]BookItem, 0, len(out.Works))
	for _, w := range out.Works {
		authors := make([]string, 0, len(w.Authors))
		for _, author := range w.Authors {
			if strings.TrimSpace(author.Name) != "" {
				authors = append(authors, author.Name)
			}
		}
		cover := ""
		if w.CoverID != 0 {
			cover = fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", w.CoverID)
		} else if strings.TrimSpace(w.CoverEditionKey) != "" {
			cover = "https://covers.openlibrary.org/b/olid/" + url.PathEscape(strings.TrimSpace(w.CoverEditionKey)) + "-M.jpg"
		}
		items = append(items, BookItem{Title: w.Title, Authors: authors, CoverSmall: cover, CoverMedium: cover})
	}
	return items, nil
}

func (ol *OpenLibrary) apiURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(ol.baseURL), "/")
	if base == "" {
		base = "https://openlibrary.org"
	}
	return base + path
}

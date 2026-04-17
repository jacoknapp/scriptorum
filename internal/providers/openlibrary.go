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
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	ISBN10           []string `json:"isbn"`
	ISBN13           []string `json:"isbn13"`
	CoverId          int      `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
}
type OLResp struct {
	Docs []OLDoc `json:"docs"`
}

type OLTrendingWork struct {
	Title            string   `json:"title"`
	AuthorName       []string `json:"author_name"`
	CoverID          int      `json:"cover_i"`
	CoverEditionKey  string   `json:"cover_edition_key"`
	FirstPublishYear int      `json:"first_publish_year"`
	Key              string   `json:"key"`
}

type OLTrendingResp struct {
	Works []OLTrendingWork `json:"works"`
}

type OLSubjectAuthor struct {
	Name string `json:"name"`
}

type OLSubjectWork struct {
	Title           string            `json:"title"`
	Authors         []OLSubjectAuthor `json:"authors"`
	CoverID         int               `json:"cover_id"`
	CoverEditionKey string            `json:"cover_edition_key"`
	Key             string            `json:"key"`
}

type OLSubjectResp struct {
	Works []OLSubjectWork `json:"works"`
}

type openLibraryTextValue struct {
	Value string `json:"value"`
}

type OLWorkDetailsResp struct {
	Key              string          `json:"key"`
	Title            string          `json:"title"`
	Description      json.RawMessage `json:"description"`
	Subjects         []string        `json:"subjects"`
	Covers           []int           `json:"covers"`
	FirstPublishDate string          `json:"first_publish_date"`
}

type OpenLibraryWorkDetails struct {
	Key              string
	Title            string
	Description      string
	Subjects         []string
	FirstPublishDate string
	CoverMedium      string
}

type BookItem struct {
	ASIN                  string
	Title                 string
	Authors               []string
	ISBN10                string
	ISBN13                string
	FirstPublishYear      int
	Description           string
	OpenLibraryWorkKey    string
	OpenLibraryEditionKey string
	CoverSmall            string
	CoverMedium           string
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
		cover := openLibraryCoverURL(d.CoverId, d.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 d.Title,
			Authors:               d.AuthorName,
			ISBN10:                i10,
			ISBN13:                i13,
			FirstPublishYear:      d.FirstPublishYear,
			OpenLibraryWorkKey:    d.Key,
			OpenLibraryEditionKey: d.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
	}
	return items, nil
}

func (ol *OpenLibrary) TrendingWorks(ctx context.Context, period string, limit int) ([]BookItem, error) {
	period = strings.ToLower(strings.TrimSpace(period))
	switch period {
	case "daily", "weekly", "monthly", "yearly", "all":
	default:
		period = "weekly"
	}
	if limit <= 0 {
		limit = 10
	}
	u := ol.apiURL("/trending/" + url.PathEscape(period) + ".json?limit=" + strconv.Itoa(limit))
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := ol.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out OLTrendingResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	items := make([]BookItem, 0, len(out.Works))
	for _, w := range out.Works {
		cover := openLibraryCoverURL(w.CoverID, w.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 w.Title,
			Authors:               w.AuthorName,
			FirstPublishYear:      w.FirstPublishYear,
			OpenLibraryWorkKey:    w.Key,
			OpenLibraryEditionKey: w.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
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
		cover := openLibraryCoverURL(w.CoverID, w.CoverEditionKey)
		items = append(items, BookItem{
			Title:                 w.Title,
			Authors:               authors,
			OpenLibraryWorkKey:    w.Key,
			OpenLibraryEditionKey: w.CoverEditionKey,
			CoverSmall:            cover,
			CoverMedium:           cover,
		})
	}
	return items, nil
}

func (ol *OpenLibrary) WorkDetails(ctx context.Context, workKey string) (*OpenLibraryWorkDetails, error) {
	workKey = strings.TrimSpace(workKey)
	if workKey == "" {
		return nil, nil
	}
	if !strings.HasPrefix(workKey, "/") {
		workKey = "/" + workKey
	}
	u := ol.apiURL(workKey + ".json")
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	resp, err := ol.cl.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	var out OLWorkDetailsResp
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return nil, err
	}
	if strings.TrimSpace(out.Key) == "" && strings.TrimSpace(out.Title) == "" && len(out.Subjects) == 0 && len(out.Covers) == 0 && len(out.Description) == 0 {
		return nil, nil
	}
	details := &OpenLibraryWorkDetails{
		Key:              out.Key,
		Title:            out.Title,
		Description:      parseOpenLibraryText(out.Description),
		Subjects:         out.Subjects,
		FirstPublishDate: out.FirstPublishDate,
	}
	if len(out.Covers) > 0 {
		details.CoverMedium = openLibraryCoverURL(out.Covers[0], "")
	}
	return details, nil
}

func openLibraryCoverURL(coverID int, coverEditionKey string) string {
	if coverID != 0 {
		return fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-M.jpg", coverID)
	}
	if strings.TrimSpace(coverEditionKey) != "" {
		return "https://covers.openlibrary.org/b/olid/" + url.PathEscape(strings.TrimSpace(coverEditionKey)) + "-M.jpg"
	}
	return ""
}

func parseOpenLibraryText(raw json.RawMessage) string {
	if len(raw) == 0 {
		return ""
	}
	var asString string
	if err := json.Unmarshal(raw, &asString); err == nil {
		return strings.TrimSpace(asString)
	}
	var wrapped openLibraryTextValue
	if err := json.Unmarshal(raw, &wrapped); err == nil {
		return strings.TrimSpace(wrapped.Value)
	}
	return ""
}

func (ol *OpenLibrary) apiURL(path string) string {
	base := strings.TrimRight(strings.TrimSpace(ol.baseURL), "/")
	if base == "" {
		base = "https://openlibrary.org"
	}
	return base + path
}

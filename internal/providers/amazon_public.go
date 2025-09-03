package providers

import (
	"context"
	"errors"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
)

type AmazonPublic struct {
	client *http.Client
	market string // e.g., www.amazon.com
}

func NewAmazonPublic(marketplace string) *AmazonPublic {
	if strings.TrimSpace(marketplace) == "" {
		marketplace = "www.amazon.com"
	}
	return &AmazonPublic{
		client: &http.Client{Timeout: 10 * time.Second},
		market: marketplace,
	}
}

type PublicBook struct {
	ASIN    string
	Title   string
	Authors []string
	ISBN10  string
	ISBN13  string
	Image   string
}

func (a *AmazonPublic) GetByASIN(ctx context.Context, asin string) (*PublicBook, error) {
	asin = ExtractASINFromInput(asin)
	if asin == "" {
		return nil, errors.New("asin required")
	}
	url := "https://" + a.market + "/dp/" + asin
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}

	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}

	b := &PublicBook{ASIN: asin}

	b.Title = strings.TrimSpace(doc.Find("#productTitle").First().Text())
	if b.Title == "" {
		b.Title = strings.TrimSpace(doc.Find("meta[property='og:title']").AttrOr("content", ""))
	}

	b.Image = doc.Find("meta[property='og:image']").AttrOr("content", "")
	if b.Image == "" {
		b.Image = doc.Find("#imgBlkFront, #ebooksImgBlkFront, #imgTagWrapperId img").AttrOr("src", "")
	}

	doc.Find("#bylineInfo span.author a, #bylineInfo a.contributorNameID, .author a.a-link-normal").Each(func(_ int, s *goquery.Selection) {
		name := strings.TrimSpace(s.Text())
		if name != "" && !contains(b.Authors, name) {
			b.Authors = append(b.Authors, name)
		}
	})

	doc.Find("#detailBullets_feature_div li").Each(func(_ int, s *goquery.Selection) {
		label := strings.TrimSpace(s.Find("span.a-text-bold").First().Text())
		val := strings.TrimSpace(strings.ReplaceAll(s.Text(), label, ""))
		label = strings.TrimSuffix(label, ":")
		if strings.EqualFold(label, "ISBN-10") && b.ISBN10 == "" {
			b.ISBN10 = onlyISBN(val)
		}
		if strings.EqualFold(label, "ISBN-13") && b.ISBN13 == "" {
			b.ISBN13 = onlyISBN(val)
		}
	})

	doc.Find("#productDetailsTable .content ul li").Each(func(_ int, s *goquery.Selection) {
		text := strings.TrimSpace(s.Text())
		if strings.Contains(text, "ISBN-10") && b.ISBN10 == "" {
			b.ISBN10 = onlyISBN(text)
		}
		if strings.Contains(text, "ISBN-13") && b.ISBN13 == "" {
			b.ISBN13 = onlyISBN(text)
		}
	})

	if b.Title == "" && b.ISBN10 == "" && b.ISBN13 == "" {
		return nil, errors.New("no book info detected")
	}
	return b, nil
}

// SearchBooks scrapes Amazon's public search results for books by keyword.
// page is 1-based; limit caps the number of parsed items to return from the page.
func (a *AmazonPublic) SearchBooks(ctx context.Context, q string, page int, limit int) ([]PublicBook, error) {
	if limit <= 0 {
		limit = 10
	}
	if page <= 0 {
		page = 1
	}
	url := "https://" + a.market + "/s?k=" + strings.ReplaceAll(q, " ", "+") + "&i=stripbooks-intl-ship"
	if page > 1 {
		url += "&page=" + strconv.Itoa(page)
	}
	req, _ := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/120.0 Safari/537.36")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")
	resp, err := a.client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 400 {
		return nil, errors.New(resp.Status)
	}
	doc, err := goquery.NewDocumentFromReader(resp.Body)
	if err != nil {
		return nil, err
	}
	var items []PublicBook
	doc.Find("div.s-result-item[data-asin]").Each(func(_ int, s *goquery.Selection) {
		if len(items) >= limit {
			return
		}
		asin, _ := s.Attr("data-asin")
		if strings.TrimSpace(asin) == "" {
			return
		}
		title := strings.TrimSpace(s.Find("h2 a span").First().Text())
		if title == "" {
			return
		}
		img := strings.TrimSpace(s.Find("img.s-image").AttrOr("src", ""))
		var authors []string
		s.Find(".a-row .a-size-base").Each(func(_ int, aSel *goquery.Selection) {
			name := strings.TrimSpace(aSel.Text())
			if name != "" && len(authors) < 3 {
				authors = append(authors, name)
			}
		})
		items = append(items, PublicBook{ASIN: asin, Title: title, Authors: authors, Image: img})
	})
	return items, nil
}

func onlyISBN(s string) string {
	s = strings.TrimSpace(s)
	s = strings.ReplaceAll(s, "ISBN-10:", "")
	s = strings.ReplaceAll(s, "ISBN-13:", "")
	s = strings.ReplaceAll(s, "-", "")
	s = strings.ToUpper(s)
	if len(s) >= 13 {
		return s[:13]
	}
	if len(s) >= 10 {
		return s[:10]
	}
	return s
}

func contains(a []string, v string) bool {
	for _, e := range a {
		if e == v {
			return true
		}
	}
	return false
}

var asinRe = strings.NewReplacer("/dp/", " ", "/gp/product/", " ", "?", " ", "#", " ", "/", " ", "-", " ")

func ExtractASINFromInput(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	ls := strings.ToLower(s)
	if strings.Contains(ls, "amazon.") {
		s = asinRe.Replace(s)
		parts := strings.Fields(s)
		for _, p := range parts {
			if len(p) == 10 {
				return p
			}
		}
	}
	if len(s) == 10 {
		return s
	}
	return ""
}

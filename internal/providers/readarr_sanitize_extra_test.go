package providers

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
)

func TestSanitizeAndEnrichPayloadAuthorDefaultsAndNestedValue(t *testing.T) {
	ra := NewReadarrWithDB(ReadarrInstance{
		BaseURL:                 "http://readarr",
		APIKey:                  "secret",
		DefaultQualityProfileID: 5,
		DefaultRootFolderPath:   "/rootA",
		DefaultTags:             []string{"10", "skip", "20"},
	}, nil)

	ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/qualityprofile":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[{"id":5,"name":"Any"}]`)),
				Header:     make(http.Header),
			}, nil
		case "/api/v1/rootfolder":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[{"path":"/rootA"}]`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})

	pmap := map[string]any{
		"author": map[string]any{
			"name":              "Known Author",
			"foreignAuthorId":   "fa-known",
			"qualityProfileId":  "0",
			"metadataProfileId": "0",
			"addOptions":        map[string]any{"monitor": "all"},
			"value": map[string]any{
				"qualityProfileId":  "0",
				"metadataProfileId": "0",
				"addOptions":        map[string]any{"monitor": "all"},
			},
		},
		"authorId":         "42",
		"tags":             []any{float64(1), "2", "bad"},
		"addOptions":       map[string]any{"monitor": "none"},
		"editions":         []any{},
		"foreignEditionId": "fe-1",
	}

	out := ra.sanitizeAndEnrichPayload(context.Background(), pmap, AddOpts{})

	if qid, _ := out["qualityProfileId"].(int); qid != 5 {
		t.Fatalf("expected top-level qualityProfileId=5, got %#v", out["qualityProfileId"])
	}
	if mpid, _ := out["metadataProfileId"].(int); mpid != 1 {
		t.Fatalf("expected metadataProfileId=1, got %#v", out["metadataProfileId"])
	}
	if aid, _ := out["authorId"].(int); aid != 42 {
		t.Fatalf("expected normalized authorId=42, got %#v", out["authorId"])
	}

	eds, _ := out["editions"].([]any)
	if len(eds) != 1 {
		t.Fatalf("expected one synthesized edition, got %#v", out["editions"])
	}

	author, ok := out["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected author map, got %#v", out["author"])
	}
	if qid, _ := author["qualityProfileId"].(int); qid != 5 {
		t.Fatalf("expected author qualityProfileId=5, got %#v", author["qualityProfileId"])
	}
	if mpid, _ := author["metadataProfileId"].(int); mpid != 1 {
		t.Fatalf("expected author metadataProfileId=1, got %#v", author["metadataProfileId"])
	}
	if mn, _ := author["monitorNewItems"].(string); mn != "none" {
		t.Fatalf("expected author monitorNewItems=none, got %#v", author["monitorNewItems"])
	}
	ao, _ := author["addOptions"].(map[string]any)
	if monitor, _ := ao["monitor"].(string); monitor != "none" {
		t.Fatalf("expected author addOptions.monitor=none, got %#v", ao["monitor"])
	}
	tags, _ := author["tags"].([]int)
	if len(tags) != 2 || tags[0] != 1 || tags[1] != 2 {
		t.Fatalf("expected author tags [1 2], got %#v", author["tags"])
	}

	v, _ := author["value"].(map[string]any)
	if qid, _ := v["qualityProfileId"].(int); qid != 5 {
		t.Fatalf("expected nested value qualityProfileId=5, got %#v", v["qualityProfileId"])
	}
	if mpid, _ := v["metadataProfileId"].(int); mpid != 1 {
		t.Fatalf("expected nested value metadataProfileId=1, got %#v", v["metadataProfileId"])
	}
	vAo, _ := v["addOptions"].(map[string]any)
	if monitor, _ := vAo["monitor"].(string); monitor != "none" {
		t.Fatalf("expected nested value addOptions.monitor=none, got %#v", vAo["monitor"])
	}
}

func TestSanitizeAndEnrichPayloadInjectsAuthorAndDropsInvalidAuthorID(t *testing.T) {
	ra := NewReadarrWithDB(ReadarrInstance{BaseURL: "http://readarr", APIKey: "secret", DefaultQualityProfileID: 3}, nil)
	ra.cl.Transport = rtFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/v1/qualityprofile":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[{"id":3,"name":"Any"}]`)),
				Header:     make(http.Header),
			}, nil
		case "/api/v1/rootfolder":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`[{"path":"/rf"}]`)),
				Header:     make(http.Header),
			}, nil
		case "/api/v1/author/9":
			return &http.Response{
				StatusCode: http.StatusOK,
				Body:       io.NopCloser(strings.NewReader(`{"name":"Injected","foreignAuthorId":"fa-9"}`)),
				Header:     make(http.Header),
			}, nil
		default:
			t.Fatalf("unexpected path: %s", req.URL.Path)
			return nil, nil
		}
	})

	in := map[string]any{"authorId": "9", "tags": []string{"7", "bad"}}
	out := ra.sanitizeAndEnrichPayload(context.Background(), in, AddOpts{SearchForMissing: true})

	author, ok := out["author"].(map[string]any)
	if !ok {
		t.Fatalf("expected injected author map, got %#v", out["author"])
	}
	if id, _ := author["id"].(int); id != 9 {
		t.Fatalf("expected injected author id=9, got %#v", author["id"])
	}
	if name, _ := author["name"].(string); name != "Injected" {
		t.Fatalf("expected injected author name, got %#v", author["name"])
	}
	if fid, _ := author["foreignAuthorId"].(string); fid != "fa-9" {
		t.Fatalf("expected injected foreignAuthorId, got %#v", author["foreignAuthorId"])
	}
	ao, _ := author["addOptions"].(map[string]any)
	if monitor, _ := ao["monitor"].(string); monitor != "none" {
		t.Fatalf("expected injected author addOptions.monitor=none, got %#v", ao["monitor"])
	}
	if sfm, _ := ao["searchForMissingBooks"].(bool); !sfm {
		t.Fatalf("expected searchForMissingBooks=true, got %#v", ao["searchForMissingBooks"])
	}

	bad := ra.sanitizeAndEnrichPayload(context.Background(), map[string]any{"authorId": "null"}, AddOpts{})
	if _, ok := bad["authorId"]; ok {
		t.Fatalf("expected invalid authorId to be removed, got %#v", bad["authorId"])
	}
}

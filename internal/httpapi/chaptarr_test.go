package httpapi

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
)

func TestReadarrInstanceForFormatPrefersSharedChaptarr(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.BookBackend = "chaptarr"
	cfg.Chaptarr.BaseURL = "https://chaptarr.example"
	cfg.Chaptarr.APIKey = "secret"
	cfg.Chaptarr.Ebooks.QualityProfileID = 11
	cfg.Chaptarr.Ebooks.MetadataProfileID = 21
	cfg.Chaptarr.Ebooks.RootFolderPath = "/ebooks"
	cfg.Chaptarr.Audiobooks.QualityProfileID = 12
	cfg.Chaptarr.Audiobooks.MetadataProfileID = 22
	cfg.Chaptarr.Audiobooks.RootFolderPath = "/audio"
	cfg.Readarr.Ebooks.BaseURL = "https://legacy-readarr.example"
	cfg.Readarr.Ebooks.APIKey = "legacy"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	ebook, ok := s.readarrInstanceForFormat("ebook")
	if !ok || ebook.Backend != "chaptarr" || ebook.MediaType != "ebook" || ebook.BaseURL != "https://chaptarr.example" || ebook.DefaultQualityProfileID != 11 || ebook.DefaultRootFolderPath != "/ebooks" {
		t.Fatalf("unexpected ebook instance: %+v", ebook)
	}
	audio, ok := s.readarrInstanceForFormat("audiobook")
	if !ok || audio.Backend != "chaptarr" || audio.MediaType != "audiobook" || audio.DefaultQualityProfileID != 12 || audio.DefaultRootFolderPath != "/audio" {
		t.Fatalf("unexpected audiobook instance: %+v", audio)
	}
}

// TestReadarrInstanceForFormatRespectsExplicitReadarrSelection ensures the
// admin's BookBackend choice is authoritative: even with Chaptarr fully
// configured, explicitly selecting Readarr must use the Readarr instance,
// not silently fall back to whichever backend happens to be configured.
func TestReadarrInstanceForFormatRespectsExplicitReadarrSelection(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.BookBackend = "readarr"
	cfg.Chaptarr.BaseURL = "https://chaptarr.example"
	cfg.Chaptarr.APIKey = "secret"
	cfg.Readarr.Ebooks.BaseURL = "https://readarr.example"
	cfg.Readarr.Ebooks.APIKey = "readarr-key"
	cfg.Readarr.Ebooks.DefaultQualityProfileID = 7
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	ebook, ok := s.readarrInstanceForFormat("ebook")
	if !ok || ebook.Backend == "chaptarr" || ebook.BaseURL != "https://readarr.example" || ebook.DefaultQualityProfileID != 7 {
		t.Fatalf("unexpected ebook instance: %+v", ebook)
	}
}

func TestSetupSavesChaptarrAndPreservesBlankAPIKey(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Chaptarr.APIKey = "saved-secret"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	values := url.Values{
		"chaptarr_base":           {"https://chaptarr.example"},
		"chaptarr_ebook_qp":       {"11"},
		"chaptarr_ebook_mp":       {"21"},
		"chaptarr_ebook_root":     {"/ebooks"},
		"chaptarr_audiobook_qp":   {"12"},
		"chaptarr_audiobook_mp":   {"22"},
		"chaptarr_audiobook_root": {"/audio"},
	}
	req := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	(&setupUI{}).handleSetupSave(s)(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("save code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := s.settings.Get().Chaptarr
	if got.APIKey != "saved-secret" || got.BaseURL != "https://chaptarr.example" || got.Ebooks.QualityProfileID != 11 || got.Ebooks.MetadataProfileID != 21 || got.Audiobooks.QualityProfileID != 12 || got.Audiobooks.MetadataProfileID != 22 {
		t.Fatalf("unexpected saved Chaptarr config: %+v", got)
	}
}

func TestSetupOldFormPreservesChaptarr(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Chaptarr.BaseURL = "https://chaptarr.example"
	cfg.Chaptarr.APIKey = "saved-secret"
	cfg.Chaptarr.Ebooks.RootFolderPath = "/ebooks"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	values := url.Values{"server_url": {"https://scriptorum.example"}}
	req := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(values.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	rec := httptest.NewRecorder()
	(&setupUI{}).handleSetupSave(s)(rec, req)
	if rec.Code != http.StatusNoContent {
		t.Fatalf("save code=%d body=%s", rec.Code, rec.Body.String())
	}
	got := s.settings.Get().Chaptarr
	if got.BaseURL != "https://chaptarr.example" || got.APIKey != "saved-secret" || got.Ebooks.RootFolderPath != "/ebooks" {
		t.Fatalf("old setup form changed Chaptarr config: %+v", got)
	}
}

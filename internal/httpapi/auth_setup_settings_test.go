package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"reflect"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"github.com/go-chi/chi/v5"
)

func withURLParam(r *http.Request, key, value string) *http.Request {
	routeCtx := chi.NewRouteContext()
	routeCtx.URLParams.Add(key, value)
	return r.WithContext(context.WithValue(r.Context(), chi.RouteCtxKey, routeCtx))
}

func TestAuthHelpers(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Admins.Usernames = []string{"Admin", "Root"}
	cfg.ServerURL = "https://scriptorum.example"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	if got := defaultIf(" ", "fallback"); got != "fallback" {
		t.Fatalf("defaultIf blank = %q", got)
	}
	if got := defaultIf("value", "fallback"); got != "value" {
		t.Fatalf("defaultIf value = %q", got)
	}
	if !s.isAdminUsername("admin") || s.isAdminUsername("guest") {
		t.Fatal("unexpected admin username lookup")
	}
	if !s.sessionCookieSecure() {
		t.Fatal("expected secure cookie when server URL is https")
	}
	if s.userName(httptest.NewRequest(http.MethodGet, "/", nil)) != "" {
		t.Fatal("expected empty user name without session context")
	}
	if s.userEmail(httptest.NewRequest(http.MethodGet, "/", nil)) != "" {
		t.Fatal("expected empty user email without session context")
	}
	if !containsInsensitive([]string{"Alpha", "Bravo"}, "bravo") {
		t.Fatal("expected containsInsensitive to match")
	}

	token, err := randomToken(8)
	if err != nil || token == "" {
		t.Fatalf("randomToken token=%q err=%v", token, err)
	}
	verifier, challenge, err := generatePKCE()
	if err != nil || verifier == "" || challenge == "" || verifier == challenge {
		t.Fatalf("generatePKCE verifier=%q challenge=%q err=%v", verifier, challenge, err)
	}
}

func TestOAuthHandlersHandleDisabledAndUnavailableState(t *testing.T) {
	s := newServerForTest(t)

	cfg := s.settings.Get()
	cfg.OAuth.Enabled = false
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	disabledReq := httptest.NewRequest(http.MethodGet, "/oauth/login", nil)
	disabledRec := httptest.NewRecorder()
	s.handleOAuthLogin(disabledRec, disabledReq)
	if disabledRec.Code != http.StatusFound || !strings.Contains(disabledRec.Header().Get("Location"), "OAuth+disabled+by+configuration") {
		t.Fatalf("unexpected disabled redirect: code=%d loc=%q", disabledRec.Code, disabledRec.Header().Get("Location"))
	}

	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = ""
	cfg.OAuth.ClientID = "client-id"
	cfg.OAuth.RedirectURL = "https://scriptorum.example/oauth/callback"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	unavailableReq := httptest.NewRequest(http.MethodGet, "/oauth/login", nil)
	unavailableRec := httptest.NewRecorder()
	s.handleOAuthLogin(unavailableRec, unavailableReq)
	if unavailableRec.Code != http.StatusFound || unavailableRec.Header().Get("Location") != "/login" {
		t.Fatalf("unexpected unavailable redirect: code=%d loc=%q", unavailableRec.Code, unavailableRec.Header().Get("Location"))
	}

	callbackReq := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=abc", nil)
	callbackRec := httptest.NewRecorder()
	s.handleCallback(callbackRec, callbackReq)
	if callbackRec.Code != http.StatusFound || !strings.Contains(callbackRec.Header().Get("Location"), "OAuth+temporarily+unavailable") {
		t.Fatalf("unexpected callback redirect: code=%d loc=%q", callbackRec.Code, callbackRec.Header().Get("Location"))
	}
}

func TestSetupHelpersAndFinish(t *testing.T) {
	s := newServerForTest(t)
	ui := &setupUI{}

	probeRec := httptest.NewRecorder()
	writeProbeJSON(probeRec, true, "")
	if ct := probeRec.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("unexpected probe JSON content type: %q", ct)
	}
	var probe map[string]any
	_ = json.Unmarshal(probeRec.Body.Bytes(), &probe)
	if probe["ok"] != true {
		t.Fatalf("unexpected probe JSON: %+v", probe)
	}

	htmlRec := httptest.NewRecorder()
	writeProbeHTML(htmlRec, false, "<bad>")
	if !strings.Contains(htmlRec.Body.String(), "&lt;bad&gt;") {
		t.Fatalf("expected escaped html, got %q", htmlRec.Body.String())
	}
	if errString(nil) != "" || errString(errors.New("boom")) != "boom" {
		t.Fatal("unexpected errString behavior")
	}

	stepFlags = map[string]bool{"admin": true, "oauth": false, "rebooks": false, "raudio": false}
	canAdvanceReq := withURLParam(httptest.NewRequest(http.MethodGet, "/setup/can-advance/2", nil), "n", "2")
	canAdvanceRec := httptest.NewRecorder()
	ui.handleCanAdvance(s)(canAdvanceRec, canAdvanceReq)
	_ = json.Unmarshal(canAdvanceRec.Body.Bytes(), &probe)
	if probe["ok"] != true {
		t.Fatalf("expected oauth step to be skippable when oauth disabled, got %+v", probe)
	}

	cfg := s.settings.Get()
	cfg.Admins.Usernames = nil
	cfg.Setup.Completed = false
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	finishReq := httptest.NewRequest(http.MethodPost, "/setup/finish", nil)
	finishRec := httptest.NewRecorder()
	ui.handleSetupFinish(s)(finishRec, finishReq)
	if finishRec.Code != http.StatusBadRequest {
		t.Fatalf("expected admin required error, got %d", finishRec.Code)
	}

	cfg.Admins.Usernames = []string{"admin"}
	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = ""
	cfg.OAuth.ClientID = ""
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	finishRec2 := httptest.NewRecorder()
	ui.handleSetupFinish(s)(finishRec2, finishReq)
	if finishRec2.Code != http.StatusBadRequest {
		t.Fatalf("expected oauth incomplete error, got %d", finishRec2.Code)
	}

	cfg.OAuth.Enabled = false
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	finishRec3 := httptest.NewRecorder()
	ui.handleSetupFinish(s)(finishRec3, finishReq)
	if finishRec3.Code != http.StatusFound || finishRec3.Header().Get("Location") != "/login" {
		t.Fatalf("unexpected finish redirect: code=%d loc=%q", finishRec3.Code, finishRec3.Header().Get("Location"))
	}
	if !s.settings.Get().Setup.Completed {
		t.Fatal("expected setup completion to persist")
	}
}

func TestReadarrSettingsEndpoints(t *testing.T) {
	readarr := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/qualityprofile/1":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":1,\"name\":\"Any\"}"))
		case "/api/v1/qualityprofile/2":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte("{\"id\":2,\"name\":\"Lossless\"}"))
		case "/api/v1/qualityprofile/3":
			http.NotFound(w, r)
		default:
			http.NotFound(w, r)
		}
	}))
	defer readarr.Close()

	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.Debug = true
	cfg.Readarr.Ebooks.BaseURL = readarr.URL
	cfg.Readarr.Ebooks.APIKey = "abcd1234"
	cfg.Readarr.Ebooks.InsecureSkipVerify = true
	cfg.Readarr.Audiobooks.BaseURL = "https://audio.example"
	cfg.Readarr.Audiobooks.APIKey = "zz99"
	if err := config.Save(s.cfgPath, cfg); err != nil {
		t.Fatalf("save cfg: %v", err)
	}
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	r := s.Router()
	admin := makeCookie(t, s, "admin", true)

	profilesReq := httptest.NewRequest(http.MethodGet, "/api/readarr/profiles?kind=ebooks", nil)
	profilesReq.AddCookie(admin)
	profilesRec := httptest.NewRecorder()
	r.ServeHTTP(profilesRec, profilesReq)
	if profilesRec.Code != http.StatusOK {
		t.Fatalf("profiles code=%d body=%s", profilesRec.Code, profilesRec.Body.String())
	}
	var profiles map[string]float64
	_ = json.Unmarshal(profilesRec.Body.Bytes(), &profiles)
	if len(profiles) != 2 {
		t.Fatalf("unexpected profiles: %+v", profiles)
	}

	missingReq := httptest.NewRequest(http.MethodGet, "/api/readarr/profiles?kind=unknown", nil)
	missingReq.AddCookie(admin)
	missingRec := httptest.NewRecorder()
	r.ServeHTTP(missingRec, missingReq)
	if missingRec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad request for missing kind, got %d", missingRec.Code)
	}

	debugReq := httptest.NewRequest(http.MethodGet, "/api/readarr/debug", nil)
	debugReq.AddCookie(admin)
	debugRec := httptest.NewRecorder()
	r.ServeHTTP(debugRec, debugReq)
	if debugRec.Code != http.StatusOK {
		t.Fatalf("debug code=%d body=%s", debugRec.Code, debugRec.Body.String())
	}
	var debugOut map[string]any
	_ = json.Unmarshal(debugRec.Body.Bytes(), &debugOut)
	ebooks := debugOut["ebooks"].(map[string]any)
	if ebooks["api_key_masked"] != "abcd****" {
		t.Fatalf("unexpected ebook api key mask: %+v", ebooks)
	}
	audiobooks := debugOut["audiobooks"].(map[string]any)
	if audiobooks["api_key_masked"] != "****" {
		t.Fatalf("unexpected audiobook api key mask: %+v", audiobooks)
	}
}

func TestSettingsSaveParsesFields(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	form := url.Values{
		"debug":                {"on"},
		"server_url":           {"https://scriptorum.example"},
		"discovery_languages":  {"eng", "spa"},
		"ra_ebooks_base":       {"https://ebooks.example"},
		"ra_ebooks_key":        {"ebooks-key"},
		"ra_ebooks_qp":         {"5"},
		"ra_audio_base":        {"https://audio.example"},
		"ra_audio_key":         {"audio-key"},
		"ra_audio_qp":          {"7"},
		"oauth_enabled":        {"true"},
		"oauth_issuer":         {"https://issuer.example"},
		"oauth_client_id":      {"client-id"},
		"oauth_client_secret":  {"secret"},
		"oauth_redirect":       {"https://scriptorum.example/oauth/callback"},
		"oauth_scopes":         {"openid, email, profile"},
		"oauth_username_claim": {"preferred_username"},
		"oauth_autocreate":     {"on"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("settings save code=%d body=%s", rec.Code, rec.Body.String())
	}

	cfg := s.settings.Get()
	if !cfg.Debug || cfg.Readarr.Ebooks.DefaultQualityProfileID != 5 || cfg.Readarr.Audiobooks.DefaultQualityProfileID != 7 {
		t.Fatalf("unexpected saved quality settings: %+v", cfg.Readarr)
	}
	if !reflect.DeepEqual(cfg.Discovery.Languages, []string{"eng", "spa"}) {
		t.Fatalf("unexpected discovery language settings: %+v", cfg.Discovery.Languages)
	}
	if !cfg.OAuth.Enabled || cfg.OAuth.ClientSecret != "secret" || len(cfg.OAuth.Scopes) != 3 || !cfg.OAuth.AutoCreateUsers {
		t.Fatalf("unexpected saved oauth settings: %+v", cfg.OAuth)
	}
}

func TestSettingsSavePreservesExistingReadarrAPIKeysWhenBlank(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://ebooks.example"
	cfg.Readarr.Ebooks.APIKey = "ebooks-existing"
	cfg.Readarr.Audiobooks.BaseURL = "https://audio.example"
	cfg.Readarr.Audiobooks.APIKey = "audio-existing"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	form := url.Values{
		"ra_ebooks_base": {"https://ebooks.example"},
		"ra_audio_base":  {"https://audio.example"},
	}
	req := httptest.NewRequest(http.MethodPost, "/settings/save", strings.NewReader(form.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusFound {
		t.Fatalf("settings save code=%d body=%s", rec.Code, rec.Body.String())
	}

	got := s.settings.Get()
	if got.Readarr.Ebooks.APIKey != "ebooks-existing" || got.Readarr.Audiobooks.APIKey != "audio-existing" {
		t.Fatalf("expected existing api keys to be preserved, got %+v", got.Readarr)
	}
}

func TestSettingsPageDoesNotRenderReadarrAPIKeys(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()

	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://ebooks.example"
	cfg.Readarr.Ebooks.APIKey = "ebooks-secret"
	cfg.Readarr.Audiobooks.BaseURL = "https://audio.example"
	cfg.Readarr.Audiobooks.APIKey = "audio-secret"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	req := httptest.NewRequest(http.MethodGet, "/settings", nil)
	req.AddCookie(makeCookie(t, s, "admin", true))
	rec := httptest.NewRecorder()
	r.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("settings page code=%d body=%s", rec.Code, rec.Body.String())
	}
	body := rec.Body.String()
	if strings.Contains(body, "ebooks-secret") || strings.Contains(body, "audio-secret") {
		t.Fatalf("expected settings page to keep api keys out of html, got %q", body)
	}
}

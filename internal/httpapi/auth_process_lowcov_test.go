package httpapi

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"golang.org/x/oauth2"
)

func TestProcessApprovalStoredPayloadErrors(t *testing.T) {
	s := makeTestServer(t)
	cfg := s.settings.Get()
	cfg.Readarr.Ebooks.BaseURL = "https://readarr.example"
	cfg.Readarr.Ebooks.APIKey = "api-key"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	resMissing := s.processApproval(context.Background(), &db.Request{
		ID:     1,
		Title:  "Missing Payload",
		Format: "ebook",
	}, "admin")
	if resMissing == nil || resMissing.Error == nil || !strings.Contains(resMissing.Error.Error(), "could not be matched to the backend system") {
		t.Fatalf("expected missing payload error, got %+v", resMissing)
	}

	resInvalid := s.processApproval(context.Background(), &db.Request{
		ID:         2,
		Title:      "Invalid Payload",
		Format:     "ebook",
		ReadarrReq: []byte("{"),
	}, "admin")
	if resInvalid == nil || resInvalid.Error == nil || !strings.Contains(resInvalid.Error.Error(), "invalid stored selection payload") {
		t.Fatalf("expected invalid payload error, got %+v", resInvalid)
	}
}

func TestHandleOAuthLoginOperationalPath(t *testing.T) {
	s := newServerForTest(t)
	cfg := s.settings.Get()
	cfg.OAuth.Enabled = true
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	s.oidc = &oidcMgr{
		enabled:     true,
		operational: true,
		issuer:      "https://issuer.example",
		config: oauth2.Config{
			ClientID: "client-id",
			Endpoint: oauth2.Endpoint{AuthURL: "https://issuer.example/auth"},
		},
	}

	req := httptest.NewRequest(http.MethodGet, "/oauth/login", nil)
	rec := httptest.NewRecorder()
	s.handleOAuthLogin(rec, req)

	if rec.Code != http.StatusFound {
		t.Fatalf("expected redirect status, got %d", rec.Code)
	}
	loc := rec.Header().Get("Location")
	if !strings.HasPrefix(loc, "https://issuer.example/auth") || !strings.Contains(loc, "state=") || !strings.Contains(loc, "code_challenge=") {
		t.Fatalf("unexpected oauth redirect location: %q", loc)
	}
}

func TestHandleCallbackStateAndIDTokenErrors(t *testing.T) {
	s := newServerForTest(t)

	stateMismatchReq := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state=expected", nil)
	stateMismatchReq.AddCookie(&http.Cookie{Name: "oauth_state", Value: "different"})
	stateMismatchRec := httptest.NewRecorder()
	s.oidc = &oidcMgr{operational: true}
	s.handleCallback(stateMismatchRec, stateMismatchReq)
	if stateMismatchRec.Code != http.StatusBadRequest {
		t.Fatalf("expected invalid state bad request, got %d", stateMismatchRec.Code)
	}

	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"access_token":"abc","token_type":"Bearer"}`))
	}))
	defer tokenServer.Close()

	noIDTokenReq := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state=ok-state", nil)
	noIDTokenReq.AddCookie(&http.Cookie{Name: "oauth_state", Value: "ok-state"})
	noIDTokenReq.AddCookie(&http.Cookie{Name: "oauth_pkce", Value: "verifier"})
	noIDTokenRec := httptest.NewRecorder()

	s.oidc = &oidcMgr{
		enabled:     true,
		operational: true,
		config: oauth2.Config{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			Endpoint: oauth2.Endpoint{
				TokenURL: tokenServer.URL,
			},
		},
	}

	s.handleCallback(noIDTokenRec, noIDTokenReq)
	if noIDTokenRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing id_token bad request, got %d", noIDTokenRec.Code)
	}
}

func TestInitOIDCErrors(t *testing.T) {
	s := newServerForTest(t)

	cfg := s.settings.Get()
	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = ""
	cfg.OAuth.ClientID = "client-id"
	cfg.OAuth.RedirectURL = "https://scriptorum.example/oauth/callback"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := s.initOIDC(); err == nil {
		t.Fatal("expected initOIDC to fail when issuer is missing")
	}

	cfg = s.settings.Get()
	cfg.OAuth.Issuer = "https://issuer.example"
	cfg.OAuth.ClientID = ""
	cfg.OAuth.RedirectURL = ""
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}
	if err := s.initOIDC(); err == nil {
		t.Fatal("expected initOIDC to fail when client_id/redirect are missing")
	}
}

func TestHandleCallbackUnavailableAndExchangeFailed(t *testing.T) {
	s := newServerForTest(t)

	// Unavailable path: initOIDC remains non-operational and callback redirects.
	cfg := s.settings.Get()
	cfg.OAuth.Enabled = true
	cfg.OAuth.Issuer = ""
	cfg.OAuth.ClientID = "client-id"
	cfg.OAuth.RedirectURL = "https://scriptorum.example/oauth/callback"
	if err := s.settings.Update(cfg); err != nil {
		t.Fatalf("update settings: %v", err)
	}

	unavailableReq := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test&state=s", nil)
	unavailableRec := httptest.NewRecorder()
	s.oidc = nil
	s.handleCallback(unavailableRec, unavailableReq)
	if unavailableRec.Code != http.StatusFound {
		t.Fatalf("expected unavailable redirect, got %d", unavailableRec.Code)
	}

	// Exchange failure path: token endpoint returns error.
	tokenServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "token error", http.StatusInternalServerError)
	}))
	defer tokenServer.Close()

	exchangeReq := httptest.NewRequest(http.MethodGet, "/oauth/callback?code=test-code&state=ok-state", nil)
	exchangeReq.AddCookie(&http.Cookie{Name: "oauth_state", Value: "ok-state"})
	exchangeReq.AddCookie(&http.Cookie{Name: "oauth_pkce", Value: "verifier"})
	exchangeRec := httptest.NewRecorder()

	s.oidc = &oidcMgr{
		enabled:     true,
		operational: true,
		config: oauth2.Config{
			ClientID:     "client-id",
			ClientSecret: "client-secret",
			Endpoint: oauth2.Endpoint{
				TokenURL: tokenServer.URL,
			},
		},
	}

	s.handleCallback(exchangeRec, exchangeReq)
	if exchangeRec.Code != http.StatusInternalServerError {
		t.Fatalf("expected exchange failure 500, got %d", exchangeRec.Code)
	}
}

func TestAuthorNameFromLookupBook(t *testing.T) {
	fromAuthor := authorNameFromLookupBook(providers.LookupBook{Author: map[string]any{"name": "Primary Author"}})
	if fromAuthor != "Primary Author" {
		t.Fatalf("expected author map name, got %q", fromAuthor)
	}

	fromAuthors := authorNameFromLookupBook(providers.LookupBook{Authors: []map[string]any{{"name": "Listed Author"}}})
	if fromAuthors != "Listed Author" {
		t.Fatalf("expected first authors[] name, got %q", fromAuthors)
	}

	fromTitle := authorNameFromLookupBook(providers.LookupBook{AuthorTitle: "andrews, ilona Burn for Me"})
	if fromTitle != "Ilona Andrews" {
		t.Fatalf("expected parsed title-based author, got %q", fromTitle)
	}
}

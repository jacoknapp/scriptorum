package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func TestSetupSaveAndStepRoutes(t *testing.T) {
	s := makeTestServer(t)
	s.disableCSRF = true
	r := s.Router()

	form := url.Values{}
	form.Set("server_url", "https://scriptorum.example")
	form.Set("admin_username", "wizardadmin")
	form.Set("admin_password", "secret-pass")
	form.Set("oauth_enabled", "on")
	form.Set("oauth_issuer", "https://issuer.example")
	form.Set("oauth_client_id", "client-id")
	form.Set("oauth_client_secret", "client-secret")
	form.Set("oauth_redirect", "https://scriptorum.example/oauth/callback")
	form.Set("oauth_scopes", "openid,email,profile")
	form.Set("ra_ebooks_base", "https://readarr-ebooks.example")
	form.Set("ra_ebooks_key", "ebooks-key")
	form.Set("ra_audio_base", "https://readarr-audio.example")
	form.Set("ra_audio_key", "audio-key")

	postReq := httptest.NewRequest(http.MethodPost, "/setup/save", strings.NewReader(form.Encode()))
	postReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	postRec := httptest.NewRecorder()
	r.ServeHTTP(postRec, postReq)

	if postRec.Code != http.StatusNoContent {
		t.Fatalf("expected 204 for setup save, got %d", postRec.Code)
	}
	if postRec.Header().Get("HX-Trigger") != "setup-saved" {
		t.Fatalf("expected setup-saved trigger, got %q", postRec.Header().Get("HX-Trigger"))
	}

	cur := s.settings.Get()
	if cur.ServerURL != "https://scriptorum.example" || cur.Readarr.Ebooks.APIKey != "ebooks-key" || cur.Readarr.Audiobooks.APIKey != "audio-key" {
		t.Fatalf("setup values not persisted: %+v", cur)
	}
	if len(cur.Admins.Usernames) == 0 || !strings.EqualFold(cur.Admins.Usernames[0], "wizardadmin") {
		t.Fatalf("expected wizard admin user in config, got %+v", cur.Admins.Usernames)
	}

	for _, n := range []string{"2", "3", "4"} {
		stepReq := httptest.NewRequest(http.MethodGet, "/setup/step/"+n, nil)
		stepRec := httptest.NewRecorder()
		r.ServeHTTP(stepRec, stepReq)
		if stepRec.Code != http.StatusOK {
			t.Fatalf("expected step %s to render, got %d", n, stepRec.Code)
		}
	}

	unknownReq := httptest.NewRequest(http.MethodGet, "/setup/step/99", nil)
	unknownRec := httptest.NewRecorder()
	r.ServeHTTP(unknownRec, unknownReq)
	if unknownRec.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown step, got %d", unknownRec.Code)
	}
}

func TestNotificationAPITestHandlers(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	adminCookie := makeCookie(t, s, "admin", true)

	badJSONReq := httptest.NewRequest(http.MethodPost, "/api/notifications/test-smtp", strings.NewReader("{"))
	badJSONReq.AddCookie(adminCookie)
	badJSONRec := httptest.NewRecorder()
	r.ServeHTTP(badJSONRec, badJSONReq)
	if badJSONRec.Code != http.StatusBadRequest {
		t.Fatalf("expected bad JSON to return 400, got %d", badJSONRec.Code)
	}

	missingFieldsBody, _ := json.Marshal(map[string]any{"host": "", "from_email": "", "to_email": ""})
	missingFieldsReq := httptest.NewRequest(http.MethodPost, "/api/notifications/test-smtp", bytes.NewReader(missingFieldsBody))
	missingFieldsReq.Header.Set("Content-Type", "application/json")
	missingFieldsReq.AddCookie(adminCookie)
	missingFieldsRec := httptest.NewRecorder()
	r.ServeHTTP(missingFieldsRec, missingFieldsReq)
	if missingFieldsRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing SMTP fields to return 400, got %d", missingFieldsRec.Code)
	}

	webhook := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
			return
		}
		w.WriteHeader(http.StatusNoContent)
	}))
	defer webhook.Close()

	validDiscordBody, _ := json.Marshal(map[string]any{"webhook_url": webhook.URL, "username": ""})
	validDiscordReq := httptest.NewRequest(http.MethodPost, "/api/notifications/test-discord", bytes.NewReader(validDiscordBody))
	validDiscordReq.Header.Set("Content-Type", "application/json")
	validDiscordReq.AddCookie(adminCookie)
	validDiscordRec := httptest.NewRecorder()
	r.ServeHTTP(validDiscordRec, validDiscordReq)
	if validDiscordRec.Code != http.StatusOK {
		t.Fatalf("expected discord test success, got %d body=%s", validDiscordRec.Code, validDiscordRec.Body.String())
	}

	missingWebhookBody, _ := json.Marshal(map[string]any{"webhook_url": ""})
	missingWebhookReq := httptest.NewRequest(http.MethodPost, "/api/notifications/test-discord", bytes.NewReader(missingWebhookBody))
	missingWebhookReq.Header.Set("Content-Type", "application/json")
	missingWebhookReq.AddCookie(adminCookie)
	missingWebhookRec := httptest.NewRecorder()
	r.ServeHTTP(missingWebhookRec, missingWebhookReq)
	if missingWebhookRec.Code != http.StatusBadRequest {
		t.Fatalf("expected missing webhook to return 400, got %d", missingWebhookRec.Code)
	}
}

func TestUsersAndRequestsTableHandlers(t *testing.T) {
	s := newServerForTest(t)
	r := s.Router()
	adminCookie := makeCookie(t, s, "admin", true)

	userForm := url.Values{}
	userForm.Set("username", "new-admin")
	userForm.Set("password", "new-admin-pass")
	userForm.Set("is_admin", "on")
	userForm.Set("is_auto_approve", "on")

	createReq := httptest.NewRequest(http.MethodPost, "/users", strings.NewReader(userForm.Encode()))
	createReq.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	createReq.AddCookie(adminCookie)
	createRec := httptest.NewRecorder()
	r.ServeHTTP(createRec, createReq)
	if createRec.Code != http.StatusFound {
		t.Fatalf("expected user create redirect, got %d", createRec.Code)
	}

	storedUser, err := s.db.GetUserByUsername(context.Background(), "new-admin")
	if err != nil {
		t.Fatalf("expected created user, got error: %v", err)
	}
	if !storedUser.IsAdmin || !storedUser.AutoApprove {
		t.Fatalf("expected admin+autoapprove user, got %+v", storedUser)
	}

	id, err := s.db.CreateRequest(context.Background(), &db.Request{
		RequesterEmail: "admin",
		Title:          "UI Table Request",
		Format:         "ebook",
		Status:         "pending",
		ReadarrReq:     []byte(`{"images":[{"coverType":"cover","url":"https://covers.example/1.jpg"}]}`),
	})
	if err != nil {
		t.Fatalf("create request: %v", err)
	}
	if id <= 0 {
		t.Fatalf("expected valid request id, got %d", id)
	}

	tableReq := httptest.NewRequest(http.MethodGet, "/ui/requests/table", nil)
	tableReq.AddCookie(adminCookie)
	tableRec := httptest.NewRecorder()
	r.ServeHTTP(tableRec, tableReq)
	if tableRec.Code != http.StatusOK {
		t.Fatalf("expected requests table render, got %d", tableRec.Code)
	}
}

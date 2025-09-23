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
	"time"
)

func TestNotificationsPage(t *testing.T) {
	server := newServerForTest(t)

	// Create admin session
	adminSession := &session{Username: "admin", Admin: true}

	req := httptest.NewRequest("GET", "/notifications", nil)
	ctx := context.WithValue(req.Context(), ctxUser, adminSession)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d", recorder.Code)
	}

	body := recorder.Body.String()
	if !strings.Contains(body, "Notifications") {
		t.Error("Page should contain 'Notifications' title")
	}

	if !strings.Contains(body, "ntfy_enabled") {
		t.Error("Page should contain ntfy enable checkbox")
	}

	if !strings.Contains(body, "smtp_enabled") {
		t.Error("Page should contain SMTP enable checkbox")
	}

	if !strings.Contains(body, "discord_enabled") {
		t.Error("Page should contain Discord enable checkbox")
	}
}

func TestNotificationsPageRequiresAdmin(t *testing.T) {
	server := newServerForTest(t)

	// Test with regular user
	userSession := &session{Username: "user", Admin: false}

	req := httptest.NewRequest("GET", "/notifications", nil)
	ctx := context.WithValue(req.Context(), ctxUser, userSession)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusForbidden {
		t.Errorf("Expected status 403 for non-admin user, got %d", recorder.Code)
	}
}

func TestNotificationsSave(t *testing.T) {
	server := newServerForTest(t)

	// Create admin session
	adminSession := &session{Username: "admin", Admin: true}

	// Prepare form data
	formData := url.Values{
		"_csrf_token":                        {server.getCSRFToken(httptest.NewRequest("GET", "/", nil))},
		"ntfy_enabled":                       {"on"},
		"ntfy_server":                        {"https://ntfy.example.com"},
		"ntfy_topic":                         {"scriptorum-test"},
		"ntfy_username":                      {"testuser"},
		"ntfy_password":                      {"testpass"},
		"ntfy_enable_request_notifications":  {"on"},
		"ntfy_enable_approval_notifications": {"on"},
		"ntfy_enable_system_notifications":   {"on"},
	}

	req := httptest.NewRequest("POST", "/notifications/save", strings.NewReader(formData.Encode()))
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
	ctx := context.WithValue(req.Context(), ctxUser, adminSession)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusFound {
		t.Errorf("Expected redirect (302), got %d", recorder.Code)
	}

	// Verify settings were saved
	cfg := server.settings.Get()
	if !cfg.Notifications.Ntfy.Enabled {
		t.Error("Expected ntfy to be enabled")
	}

	if cfg.Notifications.Ntfy.Server != "https://ntfy.example.com" {
		t.Errorf("Expected server 'https://ntfy.example.com', got '%s'", cfg.Notifications.Ntfy.Server)
	}

	if cfg.Notifications.Ntfy.Topic != "scriptorum-test" {
		t.Errorf("Expected topic 'scriptorum-test', got '%s'", cfg.Notifications.Ntfy.Topic)
	}

	if !cfg.Notifications.Ntfy.EnableRequestNotifications {
		t.Error("Expected request notifications to be enabled")
	}

	if !cfg.Notifications.Ntfy.EnableApprovalNotifications {
		t.Error("Expected approval notifications to be enabled")
	}

	if !cfg.Notifications.Ntfy.EnableSystemNotifications {
		t.Error("Expected system notifications to be enabled")
	}
}

func TestApiTestNtfy(t *testing.T) {
	server := newServerForTest(t)

	// Create admin session
	adminSession := &session{Username: "admin", Admin: true}

	// Create a test server to mock ntfy
	mockNtfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		if payload["topic"] != "test-topic" {
			http.Error(w, "Invalid topic", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mockNtfy.Close()

	// Prepare test data
	testData := map[string]string{
		"server":   mockNtfy.URL,
		"topic":    "test-topic",
		"username": "",
		"password": "",
	}

	jsonData, _ := json.Marshal(testData)

	req := httptest.NewRequest("POST", "/api/notifications/test-ntfy", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("X-Requested-With", "XMLHttpRequest")
	ctx := context.WithValue(req.Context(), ctxUser, adminSession)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusOK {
		t.Errorf("Expected status 200, got %d. Body: %s", recorder.Code, recorder.Body.String())
	}

	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if success, ok := response["success"].(bool); !ok || !success {
		t.Errorf("Expected success=true, got %v", response)
	}
}

func TestApiTestNtfyFailure(t *testing.T) {
	server := newServerForTest(t)

	// Create admin session
	adminSession := &session{Username: "admin", Admin: true}

	// Create a test server that returns error
	mockNtfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "Server error", http.StatusInternalServerError)
	}))
	defer mockNtfy.Close()

	// Prepare test data
	testData := map[string]string{
		"server": mockNtfy.URL,
		"topic":  "test-topic",
	}

	jsonData, _ := json.Marshal(testData)

	req := httptest.NewRequest("POST", "/api/notifications/test-ntfy", bytes.NewBuffer(jsonData))
	req.Header.Set("Content-Type", "application/json")
	ctx := context.WithValue(req.Context(), ctxUser, adminSession)
	req = req.WithContext(ctx)

	recorder := httptest.NewRecorder()
	server.Router().ServeHTTP(recorder, req)

	if recorder.Code != http.StatusInternalServerError {
		t.Errorf("Expected status 500, got %d", recorder.Code)
	}

	var response map[string]any
	if err := json.NewDecoder(recorder.Body).Decode(&response); err != nil {
		t.Fatalf("Failed to decode response: %v", err)
	}

	if success, ok := response["success"].(bool); !ok || success {
		t.Errorf("Expected success=false, got %v", response)
	}
}

func TestSendNtfyNotification(t *testing.T) {
	server := newServerForTest(t)

	// Create a test server to mock ntfy
	var receivedPayload map[string]any
	mockNtfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mockNtfy.Close()

	// Test sending notification
	err := server.sendNtfyNotification(
		mockNtfy.URL,
		"test-topic",
		"",
		"",
		"Test Title",
		"Test message",
		"default",
	)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	if receivedPayload["topic"] != "test-topic" {
		t.Errorf("Expected topic 'test-topic', got '%v'", receivedPayload["topic"])
	}

	if receivedPayload["title"] != "Test Title" {
		t.Errorf("Expected title 'Test Title', got '%v'", receivedPayload["title"])
	}

	if receivedPayload["message"] != "Test message" {
		t.Errorf("Expected message 'Test message', got '%v'", receivedPayload["message"])
	}

	// Test that priority is converted to numeric
	if priority, ok := receivedPayload["priority"].(float64); !ok || priority != 3 {
		t.Errorf("Expected priority 3, got '%v' (type %T)", receivedPayload["priority"], receivedPayload["priority"])
	}

	// Test that markdown is enabled
	if markdown, ok := receivedPayload["markdown"].(bool); !ok || !markdown {
		t.Errorf("Expected markdown true, got '%v'", receivedPayload["markdown"])
	}
}

func TestSendNtfyNotificationWithActions(t *testing.T) {
	server := newServerForTest(t)

	// Create a test server to mock ntfy
	var receivedPayload map[string]any
	mockNtfy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "POST" {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

		if err := json.NewDecoder(r.Body).Decode(&receivedPayload); err != nil {
			http.Error(w, "Bad request", http.StatusBadRequest)
			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	defer mockNtfy.Close()

	// Test sending notification with actions
	actions := []map[string]string{
		{
			"action": "view",
			"label":  "View Task",
			"url":    "https://example.com/task/123",
		},
		{
			"action": "reply",
			"label":  "Reply",
			"url":    "mailto:user@example.com",
		},
	}

	err := server.sendNtfyNotificationWithActions(
		mockNtfy.URL,
		"test-topic",
		"",
		"",
		"New Task Assigned",
		"A new task requires your attention.",
		"default",
		actions,
	)

	if err != nil {
		t.Errorf("Expected no error, got: %v", err)
	}

	// Test payload structure matches the example format
	if receivedPayload["topic"] != "test-topic" {
		t.Errorf("Expected topic 'test-topic', got '%v'", receivedPayload["topic"])
	}

	if receivedPayload["message"] != "A new task requires your attention." {
		t.Errorf("Expected message 'A new task requires your attention.', got '%v'", receivedPayload["message"])
	}

	if receivedPayload["title"] != "New Task Assigned" {
		t.Errorf("Expected title 'New Task Assigned', got '%v'", receivedPayload["title"])
	}

	// Test no tags field is present
	if _, ok := receivedPayload["tags"]; ok {
		t.Error("Expected no tags field in payload")
	}

	// Test actions array
	if actionsInterface, ok := receivedPayload["actions"]; ok {
		if actionsSlice, ok := actionsInterface.([]interface{}); ok {
			if len(actionsSlice) != 2 {
				t.Errorf("Expected 2 actions, got %d", len(actionsSlice))
			}
		} else {
			t.Errorf("Expected actions to be an array, got %T", actionsInterface)
		}
	} else {
		t.Error("Expected actions field in payload")
	}
}

func TestNotificationIntegrationWithRequests(t *testing.T) {
	server := newServerForTest(t)

	// Configure notifications
	cfg := server.settings.Get()
	cfg.Notifications.Ntfy.Enabled = true
	cfg.Notifications.Ntfy.Server = "https://ntfy.example.com"
	cfg.Notifications.Ntfy.Topic = "test-topic"
	cfg.Notifications.Ntfy.EnableRequestNotifications = true
	cfg.Notifications.Ntfy.EnableApprovalNotifications = true
	server.settings.Update(cfg)

	// Test that SendRequestNotification doesn't panic
	server.SendRequestNotification(1, "testuser", "Test Book", []string{"Test Author"})

	// Test that SendApprovalNotification doesn't panic
	server.SendApprovalNotification("testuser", "Test Book", []string{"Test Author"})

	// Test that SendSystemNotification doesn't panic
	server.SendSystemNotification("Test System Alert", "This is a test system notification")

	// These are async operations, so we just verify they don't panic
	// In a real test environment, you'd want to set up mock HTTP servers
}

func TestNotificationDisabled(t *testing.T) {
	server := newServerForTest(t)

	// Ensure notifications are disabled
	cfg := server.settings.Get()
	cfg.Notifications.Ntfy.Enabled = false
	cfg.Notifications.SMTP.Enabled = false
	cfg.Notifications.Discord.Enabled = false
	server.settings.Update(cfg)

	// These should not send anything and not panic
	server.SendRequestNotification(1, "testuser", "Test Book", []string{"Test Author"})
	server.SendApprovalNotification("testuser", "Test Book", []string{"Test Author"})
	server.SendSystemNotification("Test System Alert", "This is a test system notification")
}

func TestNotificationConfigDefaults(t *testing.T) {
	server := newServerForTest(t)

	cfg := server.settings.Get()

	// Test default notification settings
	if cfg.Notifications.Ntfy.Enabled {
		t.Error("Expected default ntfy to be disabled")
	}

	if cfg.Notifications.SMTP.Enabled {
		t.Error("Expected default SMTP to be disabled")
	}

	if cfg.Notifications.Discord.Enabled {
		t.Error("Expected default Discord to be disabled")
	}

	if cfg.Notifications.Ntfy.Server != "https://ntfy.sh" {
		t.Errorf("Expected default ntfy server to be 'https://ntfy.sh', got %s", cfg.Notifications.Ntfy.Server)
	}

	if cfg.Notifications.Ntfy.EnableRequestNotifications {
		t.Error("Expected default request notifications to be disabled")
	}

	if cfg.Notifications.Ntfy.EnableApprovalNotifications {
		t.Error("Expected default approval notifications to be disabled")
	}

	if cfg.Notifications.Ntfy.EnableSystemNotifications {
		t.Error("Expected default system notifications to be disabled")
	}
}

func TestApprovalToken(t *testing.T) {
	server := newServerForTest(t)

	// Test approval token generation
	approveToken := server.generateApprovalToken(123)
	if approveToken == "" {
		t.Error("Expected non-empty approval token")
	}

	// Test decline token generation
	declineToken := server.generateDeclineToken(123)
	if declineToken == "" {
		t.Error("Expected non-empty decline token")
	}

	// Verify tokens are different
	if approveToken == declineToken {
		t.Error("Expected different tokens for approve and decline")
	}

	// Verify approve token exists with correct action
	server.tokenMutex.RLock()
	approveData, approveExists := server.approvalTokens[approveToken]
	declineData, declineExists := server.approvalTokens[declineToken]
	server.tokenMutex.RUnlock()

	if !approveExists {
		t.Error("Expected approve token to exist in storage")
	}

	if !declineExists {
		t.Error("Expected decline token to exist in storage")
	}

	if approveData.RequestID != 123 {
		t.Errorf("Expected approve token request ID 123, got %d", approveData.RequestID)
	}

	if declineData.RequestID != 123 {
		t.Errorf("Expected decline token request ID 123, got %d", declineData.RequestID)
	}

	if approveData.Action != "approve" {
		t.Errorf("Expected approve token action 'approve', got '%s'", approveData.Action)
	}

	if declineData.Action != "decline" {
		t.Errorf("Expected decline token action 'decline', got '%s'", declineData.Action)
	}

	// Test token generation creates unique tokens
	token2 := server.generateApprovalToken(456)
	if approveToken == token2 {
		t.Error("Expected unique tokens to be generated")
	}
}

func TestNotificationProviderSelection(t *testing.T) {
	server := newServerForTest(t)

	// Set up test servers for ntfy and Discord
	ntfyServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just respond OK for ntfy
		w.WriteHeader(http.StatusOK)
	}))
	defer ntfyServer.Close()

	discordServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		// Just respond OK for Discord
		w.WriteHeader(http.StatusOK)
	}))
	defer discordServer.Close()

	// Configure different notification settings for each provider
	cfg := server.settings.Get()

	// Ntfy: enabled, only request notifications
	cfg.Notifications.Ntfy.Enabled = true
	cfg.Notifications.Ntfy.Server = ntfyServer.URL
	cfg.Notifications.Ntfy.Topic = "test-topic"
	cfg.Notifications.Ntfy.EnableRequestNotifications = true
	cfg.Notifications.Ntfy.EnableApprovalNotifications = false
	cfg.Notifications.Ntfy.EnableSystemNotifications = false

	// SMTP: enabled, only approval notifications (we'll skip actual SMTP testing)
	cfg.Notifications.SMTP.Enabled = true
	cfg.Notifications.SMTP.Host = "smtp.example.com"
	cfg.Notifications.SMTP.Port = 587
	cfg.Notifications.SMTP.Username = "test@example.com"
	cfg.Notifications.SMTP.Password = "password"
	cfg.Notifications.SMTP.FromEmail = "from@example.com"
	cfg.Notifications.SMTP.ToEmail = "to@example.com"
	cfg.Notifications.SMTP.EnableRequestNotifications = false
	cfg.Notifications.SMTP.EnableApprovalNotifications = true
	cfg.Notifications.SMTP.EnableSystemNotifications = false

	// Discord: enabled, only system notifications
	cfg.Notifications.Discord.Enabled = true
	cfg.Notifications.Discord.WebhookURL = discordServer.URL + "/webhook"
	cfg.Notifications.Discord.Username = "TestBot"
	cfg.Notifications.Discord.EnableRequestNotifications = false
	cfg.Notifications.Discord.EnableApprovalNotifications = false
	cfg.Notifications.Discord.EnableSystemNotifications = true

	server.settings.Update(cfg)

	// Track HTTP requests made to our test servers
	var ntfyRequests, discordRequests int
	originalNtfyHandler := ntfyServer.Config.Handler
	ntfyServer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		ntfyRequests++
		if originalNtfyHandler != nil {
			originalNtfyHandler.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	originalDiscordHandler := discordServer.Config.Handler
	discordServer.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		discordRequests++
		if originalDiscordHandler != nil {
			originalDiscordHandler.ServeHTTP(w, r)
		} else {
			w.WriteHeader(http.StatusOK)
		}
	})

	// Test request notifications - should only call ntfy
	ntfyRequests = 0
	discordRequests = 0
	server.SendRequestNotification(1, "testuser", "Test Book", []string{"Test Author"})

	// Wait a bit for async calls to complete
	time.Sleep(100 * time.Millisecond)

	if ntfyRequests != 1 {
		t.Errorf("Expected 1 ntfy request for request notification, got %d", ntfyRequests)
	}
	if discordRequests != 0 {
		t.Errorf("Expected 0 discord requests for request notification, got %d", discordRequests)
	}

	// Test approval notifications - should only call smtp (no HTTP request)
	ntfyRequests = 0
	discordRequests = 0
	server.SendApprovalNotification("testuser", "Test Book", []string{"Test Author"})

	time.Sleep(100 * time.Millisecond)

	if ntfyRequests != 0 {
		t.Errorf("Expected 0 ntfy requests for approval notification, got %d", ntfyRequests)
	}
	if discordRequests != 0 {
		t.Errorf("Expected 0 discord requests for approval notification, got %d", discordRequests)
	}

	// Test system notifications - should only call discord
	ntfyRequests = 0
	discordRequests = 0
	server.SendSystemNotification("Test Alert", "Test message")

	time.Sleep(100 * time.Millisecond)

	if ntfyRequests != 0 {
		t.Errorf("Expected 0 ntfy requests for system notification, got %d", ntfyRequests)
	}
	if discordRequests != 1 {
		t.Errorf("Expected 1 discord request for system notification, got %d", discordRequests)
	}
}

func TestApprovalHandler(t *testing.T) {
	server := newServerForTest(t)

	// Test that tokens are generated with correct actions
	approveToken := server.generateApprovalToken(123)
	declineToken := server.generateDeclineToken(456)

	// Verify tokens exist and have correct actions
	server.tokenMutex.RLock()
	approveData, approveExists := server.approvalTokens[approveToken]
	declineData, declineExists := server.approvalTokens[declineToken]
	server.tokenMutex.RUnlock()

	if !approveExists {
		t.Error("Expected approve token to exist")
	}

	if !declineExists {
		t.Error("Expected decline token to exist")
	}

	if approveData.Action != "approve" {
		t.Errorf("Expected approve action, got %s", approveData.Action)
	}

	if declineData.Action != "decline" {
		t.Errorf("Expected decline action, got %s", declineData.Action)
	}

	// Test that tokens expire after 1 hour as expected
	if time.Until(approveData.ExpiresAt) > time.Hour+time.Minute {
		t.Error("Approve token should expire within 1 hour")
	}

	if time.Until(declineData.ExpiresAt) > time.Hour+time.Minute {
		t.Error("Decline token should expire within 1 hour")
	}
}

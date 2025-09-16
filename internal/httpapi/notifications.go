package httpapi

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"html/template"
	"net/http"
	"strconv"
	"strings"
	"time"

	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
)

func (s *Server) mountNotifications(r chi.Router) {
	funcMap := template.FuncMap{"toJSON": func(v any) string { b, _ := json.Marshal(v); return string(b) }}
	u := &notificationsUI{tpl: template.Must(template.New("tpl").Funcs(funcMap).ParseFS(tplFS, "web/templates/*.html"))}

	// Public approval endpoint for one-click approvals from notifications
	r.Get("/approve/{token}", s.handleApprovalToken)

	r.Group(func(rt chi.Router) {
		rt.Use(func(next http.Handler) http.Handler { return s.requireAdmin(next.ServeHTTP) })
		rt.Get("/notifications", u.handleNotifications(s))
		rt.Post("/notifications/save", u.handleNotificationsSave(s))
		rt.Post("/api/notifications/test-ntfy", s.apiTestNtfy())
	})
}

// generateApprovalToken generates a secure token for one-click approvals
func (s *Server) generateApprovalToken(requestID int64) string {
	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	// Store token with expiration (1 hour)
	s.tokenMutex.Lock()
	s.approvalTokens[token] = approvalTokenData{
		RequestID: requestID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Action:    "approve",
	}
	s.tokenMutex.Unlock()

	return token
}

// generateDeclineToken generates a secure token for one-click declines
func (s *Server) generateDeclineToken(requestID int64) string {
	tokenBytes := make([]byte, 16)
	rand.Read(tokenBytes)
	token := hex.EncodeToString(tokenBytes)

	// Store token with expiration (1 hour)
	s.tokenMutex.Lock()
	s.approvalTokens[token] = approvalTokenData{
		RequestID: requestID,
		ExpiresAt: time.Now().Add(1 * time.Hour),
		Action:    "decline",
	}
	s.tokenMutex.Unlock()

	return token
}

// approvalTokenData holds token information
type approvalTokenData struct {
	RequestID int64
	ExpiresAt time.Time
	Action    string // "approve" or "decline"
}

// handleApprovalToken handles one-click approvals/declines via secure tokens
func (s *Server) handleApprovalToken(w http.ResponseWriter, r *http.Request) {
	token := chi.URLParam(r, "token")

	s.tokenMutex.RLock()
	tokenData, exists := s.approvalTokens[token]
	s.tokenMutex.RUnlock()

	if !exists {
		http.Error(w, "Invalid approval token", 404)
		return
	}

	if time.Now().After(tokenData.ExpiresAt) {
		s.tokenMutex.Lock()
		delete(s.approvalTokens, token)
		s.tokenMutex.Unlock()
		http.Error(w, "Approval token has expired", http.StatusGone)
		return
	}

	// Process the action based on token type
	var color, emoji, actionText string
	var statusMessage string

	if tokenData.Action == "decline" {
		// For decline, just update the status
		err := s.db.UpdateRequestStatus(r.Context(), tokenData.RequestID, "declined", "declined via notification", "system", nil, nil)
		if err != nil {
			http.Error(w, "Failed to update request status", 500)
			return
		}
		color = "#ef4444"
		emoji = "❌"
		actionText = "Declined"
		statusMessage = "declined"
	} else {
		// For approve, run the full approval logic
		req, err := s.db.GetRequest(r.Context(), tokenData.RequestID)
		if err != nil {
			http.Error(w, "Request not found", 404)
			return
		}

		// Call the same approval logic as the API
		approvalResult := s.processApproval(r.Context(), req, "system")
		if approvalResult.Error != nil {
			http.Error(w, "Failed to approve request: "+approvalResult.Error.Error(), 500)
			return
		}

		color = "#10b981"
		emoji = "✅"
		actionText = "Approved"
		statusMessage = approvalResult.Status + " via notification" // Will be "approved via notification" or "queued via notification"
	}

	// Clean up the token
	s.tokenMutex.Lock()
	delete(s.approvalTokens, token)
	s.tokenMutex.Unlock()

	// Send success response
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(200)
	w.Write([]byte(`<!DOCTYPE html>
<html>
<head>
	<title>Request ` + actionText + ` - Scriptorum</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; background: #1e293b; color: #e2e8f0; margin: 0; padding: 2rem; text-align: center; }
		.container { max-width: 500px; margin: 0 auto; background: #334155; padding: 2rem; border-radius: 12px; }
		.status { color: ` + color + `; font-size: 1.25rem; margin-bottom: 1rem; }
		.button { display: inline-block; background: #3b82f6; color: white; text-decoration: none; padding: 0.75rem 1.5rem; border-radius: 6px; margin-top: 1rem; }
		.button:hover { background: #2563eb; }
	</style>
</head>
<body>
	<div class="container">
		<div class="status">` + emoji + ` Request #` + strconv.FormatInt(tokenData.RequestID, 10) + ` ` + actionText + `!</div>
		<p>The request has been successfully ` + statusMessage + ` and will be processed accordingly.</p>
		<a href="http://localhost:8080/requests" class="button">View All Requests</a>
	</div>
</body>
</html>`))
}

// ApprovalResult represents the result of processing an approval
type ApprovalResult struct {
	Status string
	Error  error
}

// processApproval handles the approval logic shared between API and notification approval
func (s *Server) processApproval(ctx context.Context, req *db.Request, username string) *ApprovalResult {
	var inst providers.ReadarrInstance
	if req.Format == "audiobook" {
		c := s.settings.Get().Readarr.Audiobooks
		inst = providers.ReadarrInstance{
			BaseURL:                 c.BaseURL,
			APIKey:                  c.APIKey,
			DefaultQualityProfileID: c.DefaultQualityProfileID,
			DefaultRootFolderPath:   c.DefaultRootFolderPath,
			DefaultTags:             c.DefaultTags,
			InsecureSkipVerify:      c.InsecureSkipVerify,
		}
	} else {
		c := s.settings.Get().Readarr.Ebooks
		inst = providers.ReadarrInstance{
			BaseURL:                 c.BaseURL,
			APIKey:                  c.APIKey,
			DefaultQualityProfileID: c.DefaultQualityProfileID,
			DefaultRootFolderPath:   c.DefaultRootFolderPath,
			DefaultTags:             c.DefaultTags,
			InsecureSkipVerify:      c.InsecureSkipVerify,
		}
	}

	// If Readarr not configured, approve without sending
	if strings.TrimSpace(inst.BaseURL) == "" || strings.TrimSpace(inst.APIKey) == "" {
		_ = s.db.ApproveRequest(ctx, req.ID, username)
		_ = s.db.UpdateRequestStatus(ctx, req.ID, "approved", "approved via notification (no Readarr configured)", username, nil, nil)

		// Send notification for approved request
		s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

		return &ApprovalResult{Status: "approved", Error: nil}
	}

	ra := providers.NewReadarrWithDB(inst, s.db.SQL())

	reqCtx, cancel := context.WithTimeout(ctx, 12*time.Second)
	defer cancel()

	// Require an exact selection payload saved at request-time
	if len(req.ReadarrReq) == 0 {
		return &ApprovalResult{Status: "", Error: fmt.Errorf("request has no stored selection payload")}
	}

	var cand map[string]any
	if err := json.Unmarshal(req.ReadarrReq, &cand); err != nil || cand == nil {
		return &ApprovalResult{Status: "", Error: fmt.Errorf("invalid stored selection payload")}
	}

	// Handle author resolution (simplified from API version)
	if a, ok := cand["author"].(map[string]any); ok {
		if _, hasID := a["id"]; !hasID {
			var name string
			if n, _ := a["name"].(string); n != "" {
				name = n
			} else if n, _ := cand["title"].(string); n != "" {
				name = n
			}
			if name != "" {
				if aid, err := ra.FindAuthorIDByName(reqCtx, name); err == nil && aid != 0 {
					a["id"] = aid
				}
			}
			cand["author"] = a
		}
	}

	// Try to add the book to Readarr
	var payload []byte
	var respBody []byte
	var err error

	if len(req.ReadarrReq) > 0 {
		var raw map[string]any
		if json.Unmarshal(req.ReadarrReq, &raw) == nil {
			if _, ok := raw["authorTitle"]; ok || raw["author"] != nil || raw["editions"] != nil || raw["addOptions"] != nil {
				payload, respBody, err = ra.AddBookRaw(reqCtx, req.ReadarrReq)
			}
		}
	}

	// Fallback to templated add
	if payload == nil && err == nil {
		payload, respBody, err = ra.AddBook(reqCtx, cand, providers.AddOpts{
			QualityProfileID: inst.DefaultQualityProfileID,
			RootFolderPath:   inst.DefaultRootFolderPath,
			SearchForMissing: true,
			Tags:             inst.DefaultTags,
		})
	}

	if err != nil {
		// Handle duplicate book error
		emsg := strings.ToLower(err.Error())
		if strings.Contains(emsg, "ix_editions_foreigneditionid") ||
			strings.Contains(emsg, "duplicate key value") ||
			strings.Contains(emsg, "already exists") {

			// For duplicates, try to enable monitoring with a single command (no background loop needed)
			if payload != nil {
				if bid, gotBody, gerr := ra.GetBookByAddPayload(reqCtx, payload); gerr == nil && bid > 0 {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Duplicate detected; GET existing book with same payload returned (id=%d):\n%s\n", bid, string(gotBody))
					}
					if monBody, merr := ra.MonitorBooks(reqCtx, []int{bid}, true); merr == nil {
						if s.settings.Get().Debug {
							mb, _ := json.Marshal(map[string]any{"bookIds": []int{bid}, "monitored": true})
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor sent payload:\n%s\n", string(mb))
							fmt.Printf("DEBUG: PUT /api/v1/book/monitor returned body:\n%s\n", string(monBody))
						}
						_ = s.db.ApproveRequest(ctx, req.ID, username)
						_ = s.db.UpdateRequestStatus(ctx, req.ID, "queued", fmt.Sprintf("already in Readarr; monitoring enabled for id %d via notification", bid), username, payload, respBody)

						// Send notification for approved request
						s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

						return &ApprovalResult{Status: "queued", Error: nil}
					}
				}
			}

			// Fallback: treat as already present without monitor update
			_ = s.db.ApproveRequest(ctx, req.ID, username)
			_ = s.db.UpdateRequestStatus(ctx, req.ID, "queued", "already in Readarr (duplicate edition) via notification", username, payload, respBody)

			// Send notification for approved request
			s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

			return &ApprovalResult{Status: "queued", Error: nil}
		}

		_ = s.db.UpdateRequestStatus(ctx, req.ID, "error", err.Error(), "system", payload, respBody)
		return &ApprovalResult{Status: "", Error: err}
	}

	// Success - book added to Readarr
	_ = s.db.ApproveRequest(ctx, req.ID, username)
	_ = s.db.UpdateRequestStatus(ctx, req.ID, "queued", "sent to Readarr via notification", username, payload, respBody)

	// Start background monitoring task for successful additions
	if respBody != nil {
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Readarr add response body for monitoring (notification approval):\n%s\n", string(respBody))
		}
		var rb map[string]any
		if json.Unmarshal(respBody, &rb) == nil {
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Parsed response structure (notification): %+v\n", rb)
			}
			if v, ok := rb["id"]; ok {
				var bid int
				switch t := v.(type) {
				case float64:
					bid = int(t)
				case int:
					bid = t
				case int64:
					bid = int(t)
				}
				if bid > 0 {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Starting background monitor for book ID: %d (notification approval)\n", bid)
					}
					ra := providers.NewReadarrWithDB(inst, s.db.SQL())
					go s.backgroundMonitorBook(ra, bid)
				} else {
					if s.settings.Get().Debug {
						fmt.Printf("DEBUG: Book ID is 0 or invalid (notification): %v (type: %T)\n", v, v)
					}
				}
			} else {
				if s.settings.Get().Debug {
					keys := make([]string, 0, len(rb))
					for k := range rb {
						keys = append(keys, k)
					}
					fmt.Printf("DEBUG: No 'id' field found in response (notification). Available fields: %v\n", keys)
				}
			}
		} else {
			if s.settings.Get().Debug {
				fmt.Printf("DEBUG: Failed to parse response body as JSON (notification)\n")
			}
		}
	} else {
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: No response body from Readarr add operation (notification)\n")
		}
	}

	// Send notification for approved request
	s.SendApprovalNotification(req.RequesterEmail, req.Title, req.Authors)

	return &ApprovalResult{Status: "queued", Error: nil}
}

type notificationsUI struct{ tpl *template.Template }

func (u *notificationsUI) handleNotifications(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		data := map[string]any{
			"Notifications": s.settings.Get().Notifications,
			"UserName":      s.userName(r),
			"IsAdmin":       true,
			"CSRFToken":     s.getCSRFToken(r),
		}
		_ = u.tpl.ExecuteTemplate(w, "notifications.html", data)
	}
}

func (u *notificationsUI) handleNotificationsSave(s *Server) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		_ = r.ParseForm()
		cur := *s.settings.Get()

		// Update notification settings
		cur.Notifications.Provider = strings.TrimSpace(r.FormValue("notification_provider"))

		// Update ntfy settings
		cur.Notifications.Ntfy.Server = strings.TrimSpace(r.FormValue("ntfy_server"))
		if cur.Notifications.Ntfy.Server == "" {
			cur.Notifications.Ntfy.Server = "https://ntfy.sh"
		}
		cur.Notifications.Ntfy.Topic = strings.TrimSpace(r.FormValue("ntfy_topic"))
		cur.Notifications.Ntfy.Username = strings.TrimSpace(r.FormValue("ntfy_username"))
		if v := strings.TrimSpace(r.FormValue("ntfy_password")); v != "" {
			cur.Notifications.Ntfy.Password = v
		}
		cur.Notifications.Ntfy.EnableRequestNotifications = r.FormValue("ntfy_enable_request_notifications") == "on"
		cur.Notifications.Ntfy.EnableApprovalNotifications = r.FormValue("ntfy_enable_approval_notifications") == "on"
		cur.Notifications.Ntfy.EnableSystemNotifications = r.FormValue("ntfy_enable_system_notifications") == "on"

		_ = s.settings.Update(&cur)
		http.Redirect(w, r, "/notifications", http.StatusFound)
	}
}

// apiTestNtfy tests the ntfy configuration by sending a test notification
func (s *Server) apiTestNtfy() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Server   string `json:"server"`
			Topic    string `json:"topic"`
			Username string `json:"username"`
			Password string `json:"password"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]any{"success": false, "error": "Invalid request"}, 400)
			return
		}

		if req.Server == "" {
			req.Server = "https://ntfy.sh"
		}
		if req.Topic == "" {
			writeJSON(w, map[string]any{"success": false, "error": "Topic is required"}, 400)
			return
		}

		// Send test notification
		testMessage := "🧪 **Test Notification**\n\n✅ *Configuration is working correctly!*\n\n🔔 You will receive notifications for:\n• New book requests\n• Request approvals\n• System alerts\n\n💡 *Click the button below to visit Scriptorum*"

		// Create test action button
		testActions := []map[string]string{
			{
				"action": "view",
				"label":  "🌐 Open Scriptorum",
				"url":    "http://localhost:8080",
			},
		}

		err := s.sendNtfyNotificationWithActions(req.Server, req.Topic, req.Username, req.Password, "🎯 Scriptorum Test", testMessage, "default", testActions)
		if err != nil {
			writeJSON(w, map[string]any{"success": false, "error": err.Error()}, 500)
			return
		}

		writeJSON(w, map[string]any{"success": true}, 200)
	}
}

// sendNtfyNotification sends a notification via ntfy.sh
func (s *Server) sendNtfyNotification(server, topic, username, password, title, message, priority string) error {
	return s.sendNtfyNotificationWithActions(server, topic, username, password, title, message, priority, nil)
}

// sendNtfyNotificationWithActions sends a notification via ntfy.sh with optional action buttons
func (s *Server) sendNtfyNotificationWithActions(server, topic, username, password, title, message, priority string, actions []map[string]string) error {
	// For JSON publishing, POST to the root URL, not the topic URL
	url := strings.TrimRight(server, "/")

	// Convert priority string to proper format
	var priorityValue interface{}
	switch priority {
	case "1", "min":
		priorityValue = 1
	case "2", "low":
		priorityValue = 2
	case "3", "default":
		priorityValue = 3
	case "4", "high":
		priorityValue = 4
	case "5", "max", "urgent":
		priorityValue = 5
	default:
		priorityValue = 3 // Default to 3
	}

	payload := map[string]any{
		"topic":    topic,
		"message":  message,
		"title":    title,
		"priority": priorityValue,
		"markdown": true, // Enable markdown formatting for bold/italic text
	}

	// Add actions if provided
	if len(actions) > 0 {
		payload["actions"] = actions
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal notification: %w", err)
	}

	req, err := http.NewRequest("POST", url, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	// Add authentication if provided
	if username != "" && password != "" {
		req.SetBasicAuth(username, password)
	}

	client := &http.Client{
		Timeout: 10 * time.Second, // 10 second timeout
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("ntfy server returned error: %d", resp.StatusCode)
	}

	return nil
}

// SendRequestNotification sends a notification when a new request is created
func (s *Server) SendRequestNotification(requestID int64, username, title string, authors []string) {
	cfg := s.settings.Get()
	if cfg.Notifications.Provider != "ntfy" || !cfg.Notifications.Ntfy.EnableRequestNotifications {
		return
	}

	authorsStr := strings.Join(authors, ", ")
	message := fmt.Sprintf("📖 **%s**", title)
	if authorsStr != "" {
		message += fmt.Sprintf("\n👤 *by %s*", authorsStr)
	}
	message += fmt.Sprintf("\n🙋 Requested by: **%s**", username)
	message += fmt.Sprintf("\n🆔 Request ID: **#%d**", requestID)
	message += "\n\n💡 *Click 'Approve' or 'Decline' to act on this specific request, or 'View' to see all requests.*"

	// Create action buttons with individual request approval and decline
	approvalToken := s.generateApprovalToken(requestID)
	declineToken := s.generateDeclineToken(requestID)
	actions := []map[string]string{
		{
			"action": "view",
			"label":  "📋 View All Requests",
			"url":    "http://localhost:8080/requests",
		},
		{
			"action": "view",
			"label":  fmt.Sprintf("✅ Approve Request #%d", requestID),
			"url":    fmt.Sprintf("http://localhost:8080/approve/%s", approvalToken),
		},
		{
			"action": "view",
			"label":  fmt.Sprintf("❌ Decline Request #%d", requestID),
			"url":    fmt.Sprintf("http://localhost:8080/approve/%s", declineToken),
		},
	}

	go func() {
		_ = s.sendNtfyNotificationWithActions(
			cfg.Notifications.Ntfy.Server,
			cfg.Notifications.Ntfy.Topic,
			cfg.Notifications.Ntfy.Username,
			cfg.Notifications.Ntfy.Password,
			"📚 New Book Request",
			message,
			"default",
			actions,
		)
	}()
}

// SendApprovalNotification sends a notification when a request is approved
func (s *Server) SendApprovalNotification(username, title string, authors []string) {
	cfg := s.settings.Get()
	if cfg.Notifications.Provider != "ntfy" || !cfg.Notifications.Ntfy.EnableApprovalNotifications {
		return
	}

	authorsStr := strings.Join(authors, ", ")
	message := fmt.Sprintf("🎉 **%s**", title)
	if authorsStr != "" {
		message += fmt.Sprintf("\n👤 *by %s*", authorsStr)
	}
	message += fmt.Sprintf("\n✅ **Approved** for: **%s**", username)
	message += "\n\n📚 *Your request has been processed and should be available soon!*"

	// Create action button to view requests
	actions := []map[string]string{
		{
			"action": "view",
			"label":  "📋 View All Requests",
			"url":    "http://localhost:8080/requests",
		},
	}

	go func() {
		_ = s.sendNtfyNotificationWithActions(
			cfg.Notifications.Ntfy.Server,
			cfg.Notifications.Ntfy.Topic,
			cfg.Notifications.Ntfy.Username,
			cfg.Notifications.Ntfy.Password,
			"✅ Request Approved",
			message,
			"default",
			actions,
		)
	}()
}

// SendSystemNotification sends a system notification
func (s *Server) SendSystemNotification(title, message string) {
	cfg := s.settings.Get()
	if cfg.Notifications.Provider != "ntfy" || !cfg.Notifications.Ntfy.EnableSystemNotifications {
		return
	}

	go func() {
		_ = s.sendNtfyNotification(
			cfg.Notifications.Ntfy.Server,
			cfg.Notifications.Ntfy.Topic,
			cfg.Notifications.Ntfy.Username,
			cfg.Notifications.Ntfy.Password,
			title,
			message,
			"high",
		)
	}()
}

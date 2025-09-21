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

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
	"gitea.knapp/jacoknapp/scriptorum/internal/providers"
	"github.com/go-chi/chi/v5"
	"gopkg.in/gomail.v2"
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
		rt.Post("/api/notifications/test-smtp", s.apiTestSMTP())
		rt.Post("/api/notifications/test-discord", s.apiTestDiscord())
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

	if s.settings.Get().Debug {
		fmt.Printf("DEBUG: Generated approval token %s for request %d\n", token, requestID)
	}
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
		// Log for debugging
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Token not found: %s. Available tokens: %d\n", token, len(s.approvalTokens))
		}
		http.Error(w, "Invalid approval token", 404)
		return
	}

	if time.Now().After(tokenData.ExpiresAt) {
		s.tokenMutex.Lock()
		delete(s.approvalTokens, token)
		s.tokenMutex.Unlock()
		if s.settings.Get().Debug {
			fmt.Printf("DEBUG: Token expired: %s\n", token)
		}
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
		<a href="` + s.cfg.ServerURL + `/requests" class="button">View All Requests</a>
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

		// Update SMTP settings
		cur.Notifications.SMTP.Host = strings.TrimSpace(r.FormValue("smtp_host"))
		if portStr := strings.TrimSpace(r.FormValue("smtp_port")); portStr != "" {
			if port, err := strconv.Atoi(portStr); err == nil && port > 0 {
				cur.Notifications.SMTP.Port = port
			}
		}
		cur.Notifications.SMTP.Username = strings.TrimSpace(r.FormValue("smtp_username"))
		if v := strings.TrimSpace(r.FormValue("smtp_password")); v != "" {
			cur.Notifications.SMTP.Password = v
		}
		cur.Notifications.SMTP.FromEmail = strings.TrimSpace(r.FormValue("smtp_from_email"))
		cur.Notifications.SMTP.FromName = strings.TrimSpace(r.FormValue("smtp_from_name"))
		if cur.Notifications.SMTP.FromName == "" {
			cur.Notifications.SMTP.FromName = "Scriptorum"
		}
		cur.Notifications.SMTP.ToEmail = strings.TrimSpace(r.FormValue("smtp_to_email"))
		cur.Notifications.SMTP.EnableTLS = r.FormValue("smtp_enable_tls") == "on"
		cur.Notifications.SMTP.EnableRequestNotifications = r.FormValue("smtp_enable_request_notifications") == "on"
		cur.Notifications.SMTP.EnableApprovalNotifications = r.FormValue("smtp_enable_approval_notifications") == "on"
		cur.Notifications.SMTP.EnableSystemNotifications = r.FormValue("smtp_enable_system_notifications") == "on"

		// Update Discord settings
		cur.Notifications.Discord.WebhookURL = strings.TrimSpace(r.FormValue("discord_webhook_url"))
		cur.Notifications.Discord.Username = strings.TrimSpace(r.FormValue("discord_username"))
		if cur.Notifications.Discord.Username == "" {
			cur.Notifications.Discord.Username = "Scriptorum"
		}
		cur.Notifications.Discord.EnableRequestNotifications = r.FormValue("discord_enable_request_notifications") == "on"
		cur.Notifications.Discord.EnableApprovalNotifications = r.FormValue("discord_enable_approval_notifications") == "on"
		cur.Notifications.Discord.EnableSystemNotifications = r.FormValue("discord_enable_system_notifications") == "on"

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
		currentCfg := s.settings.Get()
		testActions := []map[string]string{
			{
				"action": "view",
				"label":  "🌐 Open Scriptorum",
				"url":    currentCfg.ServerURL,
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

// sendSMTPNotification sends a notification via SMTP email
func (s *Server) sendSMTPNotification(smtpConfig config.SMTPConfig, subject, htmlBody, textBody string) error {
	if smtpConfig.Host == "" || smtpConfig.FromEmail == "" || smtpConfig.ToEmail == "" {
		return fmt.Errorf("SMTP configuration incomplete: missing host, from_email, or to_email")
	}

	m := gomail.NewMessage()
	m.SetHeader("From", fmt.Sprintf("%s <%s>", smtpConfig.FromName, smtpConfig.FromEmail))
	m.SetHeader("To", smtpConfig.ToEmail)
	m.SetHeader("Subject", subject)

	if htmlBody != "" {
		m.SetBody("text/html", htmlBody)
		if textBody != "" {
			m.AddAlternative("text/plain", textBody)
		}
	} else {
		m.SetBody("text/plain", textBody)
	}

	d := gomail.NewDialer(smtpConfig.Host, smtpConfig.Port, smtpConfig.Username, smtpConfig.Password)
	if !smtpConfig.EnableTLS {
		d.TLSConfig = nil
	}

	return d.DialAndSend(m)
}

// apiTestSMTP tests the SMTP configuration by sending a test email
func (s *Server) apiTestSMTP() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Host      string `json:"host"`
			Port      int    `json:"port"`
			Username  string `json:"username"`
			Password  string `json:"password"`
			FromEmail string `json:"from_email"`
			FromName  string `json:"from_name"`
			ToEmail   string `json:"to_email"`
			EnableTLS bool   `json:"enable_tls"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]any{"success": false, "error": "Invalid request"}, 400)
			return
		}

		if req.Host == "" || req.FromEmail == "" || req.ToEmail == "" {
			writeJSON(w, map[string]any{"success": false, "error": "Host, from_email, and to_email are required"}, 400)
			return
		}

		if req.Port == 0 {
			req.Port = 587 // Default to 587
		}
		if req.FromName == "" {
			req.FromName = "Scriptorum Test"
		}

		// Create test email content
		subject := "🧪 Scriptorum SMTP Test"
		htmlBody := `<!DOCTYPE html>
<html>
<head>
	<title>Scriptorum Test Email</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #333; }
		.container { max-width: 600px; margin: 0 auto; padding: 20px; }
		.header { background: #3b82f6; color: white; padding: 20px; border-radius: 8px 8px 0 0; }
		.content { background: #f8fafc; padding: 20px; border-radius: 0 0 8px 8px; }
		.success { color: #10b981; font-weight: bold; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>🎯 Scriptorum SMTP Test</h1>
		</div>
		<div class="content">
			<p><span class="success">✅ Configuration is working correctly!</span></p>
			<p>🔔 You will receive email notifications for:</p>
			<ul>
				<li>New book requests</li>
				<li>Request approvals</li>
				<li>System alerts</li>
			</ul>
			<p>💡 <em>This is a test email to verify your SMTP configuration.</em></p>
		</div>
	</div>
</body>
</html>`

		textBody := `🧪 Scriptorum SMTP Test

✅ Configuration is working correctly!

🔔 You will receive email notifications for:
• New book requests
• Request approvals  
• System alerts

💡 This is a test email to verify your SMTP configuration.`

		smtpConfig := config.SMTPConfig{
			Host:      req.Host,
			Port:      req.Port,
			Username:  req.Username,
			Password:  req.Password,
			FromEmail: req.FromEmail,
			FromName:  req.FromName,
			ToEmail:   req.ToEmail,
			EnableTLS: req.EnableTLS,
		}

		err := s.sendSMTPNotification(smtpConfig, subject, htmlBody, textBody)
		if err != nil {
			writeJSON(w, map[string]any{"success": false, "error": err.Error()}, 500)
			return
		}

		writeJSON(w, map[string]any{"success": true}, 200)
	}
}

// sendDiscordNotification sends a notification via Discord webhook
func (s *Server) sendDiscordNotification(webhookURL, username, title, message string, color int) error {
	if webhookURL == "" {
		return fmt.Errorf("discord webhook URL is required")
	}

	embed := map[string]any{
		"title":       title,
		"description": message,
		"color":       color,
		"timestamp":   time.Now().Format(time.RFC3339),
		"footer": map[string]string{
			"text": "Scriptorum",
		},
	}

	payload := map[string]any{
		"username": username,
		"embeds":   []map[string]any{embed},
	}

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal Discord payload: %w", err)
	}

	req, err := http.NewRequest("POST", webhookURL, bytes.NewBuffer(jsonData))
	if err != nil {
		return fmt.Errorf("failed to create Discord request: %w", err)
	}

	req.Header.Set("Content-Type", "application/json")

	client := &http.Client{
		Timeout: 10 * time.Second,
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to send Discord notification: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode >= 400 {
		return fmt.Errorf("discord webhook returned error: %d", resp.StatusCode)
	}

	return nil
}

// apiTestDiscord tests the Discord configuration by sending a test message
func (s *Server) apiTestDiscord() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			WebhookURL string `json:"webhook_url"`
			Username   string `json:"username"`
		}

		if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
			writeJSON(w, map[string]any{"success": false, "error": "Invalid request"}, 400)
			return
		}

		if req.WebhookURL == "" {
			writeJSON(w, map[string]any{"success": false, "error": "webhook URL is required"}, 400)
			return
		}

		if req.Username == "" {
			req.Username = "Scriptorum Test"
		}

		// Send test notification
		title := "🧪 Scriptorum Discord Test"
		message := "✅ **Configuration is working correctly!**\n\n🔔 You will receive Discord notifications for:\n• New book requests\n• Request approvals\n• System alerts\n\n💡 *This is a test message to verify your Discord webhook configuration.*"
		color := 0x3b82f6 // Blue color

		err := s.sendDiscordNotification(req.WebhookURL, req.Username, title, message, color)
		if err != nil {
			writeJSON(w, map[string]any{"success": false, "error": err.Error()}, 500)
			return
		}

		writeJSON(w, map[string]any{"success": true}, 200)
	}
}

// SendRequestNotification sends a notification when a new request is created
func (s *Server) SendRequestNotification(requestID int64, username, title string, authors []string) {
	cfg := s.settings.Get()

	// Check if any notifications are enabled
	if cfg.Notifications.Provider == "" {
		return
	}

	authorsStr := strings.Join(authors, ", ")

	switch cfg.Notifications.Provider {
	case "ntfy":
		if !cfg.Notifications.Ntfy.EnableRequestNotifications {
			return
		}
		s.sendRequestNotificationNtfy(cfg, requestID, username, title, authorsStr)

	case "smtp":
		if !cfg.Notifications.SMTP.EnableRequestNotifications {
			return
		}
		s.sendRequestNotificationSMTP(cfg, requestID, username, title, authorsStr)

	case "discord":
		if !cfg.Notifications.Discord.EnableRequestNotifications {
			return
		}
		s.sendRequestNotificationDiscord(cfg, requestID, username, title, authorsStr)
	}
}

// sendRequestNotificationNtfy sends ntfy notification for new requests
func (s *Server) sendRequestNotificationNtfy(cfg *config.Config, requestID int64, username, title, authorsStr string) {
	// Always get fresh configuration to ensure server_url is current
	currentCfg := s.settings.Get()

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
			"url":    currentCfg.ServerURL + "/requests",
		},
		{
			"action": "view",
			"label":  fmt.Sprintf("✅ Approve Request #%d", requestID),
			"url":    fmt.Sprintf("%s/approve/%s", currentCfg.ServerURL, approvalToken),
		},
		{
			"action": "view",
			"label":  fmt.Sprintf("❌ Decline Request #%d", requestID),
			"url":    fmt.Sprintf("%s/approve/%s", currentCfg.ServerURL, declineToken),
		},
	}

	go func() {
		_ = s.sendNtfyNotificationWithActions(
			currentCfg.Notifications.Ntfy.Server,
			currentCfg.Notifications.Ntfy.Topic,
			currentCfg.Notifications.Ntfy.Username,
			currentCfg.Notifications.Ntfy.Password,
			"📚 New Book Request",
			message,
			"default",
			actions,
		)
	}()
}

// sendRequestNotificationSMTP sends email notification for new requests
func (s *Server) sendRequestNotificationSMTP(cfg *config.Config, requestID int64, username, title, authorsStr string) {
	currentCfg := s.settings.Get()
	approvalToken := s.generateApprovalToken(requestID)
	declineToken := s.generateDeclineToken(requestID)
	subject := "📚 New Book Request - Scriptorum"

	// HTML email content
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>New Book Request</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #333; margin: 0; padding: 20px; }
		.container { max-width: 600px; margin: 0 auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
		.header { background: #3b82f6; color: white; padding: 20px; text-align: center; }
		.content { padding: 20px; }
		.book-info { background: #f8fafc; padding: 15px; border-radius: 6px; margin: 15px 0; }
		.actions { text-align: center; margin: 20px 0; }
		.button { display: inline-block; padding: 10px 20px; margin: 0 5px; text-decoration: none; border-radius: 5px; font-weight: bold; }
		.approve { background: #10b981; color: white; }
		.decline { background: #ef4444; color: white; }
		.view { background: #6b7280; color: white; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>📚 New Book Request</h1>
		</div>
		<div class="content">
			<div class="book-info">
				<h2>📖 %s</h2>
				%s
				<p><strong>🙋 Requested by:</strong> %s</p>
				<p><strong>🆔 Request ID:</strong> #%d</p>
			</div>
			<div class="actions">
				<a href="%s/approve/%s" class="button approve">✅ Approve Request</a>
				<a href="%s/approve/%s" class="button decline">❌ Decline Request</a>
				<a href="%s/requests" class="button view">📋 View All Requests</a>
			</div>
		</div>
	</div>
</body>
</html>`, title,
		func() string {
			if authorsStr != "" {
				return fmt.Sprintf("<p><strong>👤 Author(s):</strong> %s</p>", authorsStr)
			}
			return ""
		}(),
		username, requestID, currentCfg.ServerURL, approvalToken, currentCfg.ServerURL, declineToken, currentCfg.ServerURL)

	// Plain text content
	textBody := fmt.Sprintf(`📚 New Book Request - Scriptorum

📖 %s
%s🙋 Requested by: %s
🆔 Request ID: #%d

Actions:
• Approve: %s/approve/%s
• Decline: %s/approve/%s  
• View All Requests: %s/requests`,
		title,
		func() string {
			if authorsStr != "" {
				return fmt.Sprintf("👤 Author(s): %s\n", authorsStr)
			}
			return ""
		}(),
		username, requestID, currentCfg.ServerURL, approvalToken, currentCfg.ServerURL, declineToken, currentCfg.ServerURL)

	go func() {
		_ = s.sendSMTPNotification(cfg.Notifications.SMTP, subject, htmlBody, textBody)
	}()
}

// sendRequestNotificationDiscord sends Discord notification for new requests
func (s *Server) sendRequestNotificationDiscord(cfg *config.Config, requestID int64, username, title, authorsStr string) {
	currentCfg := s.settings.Get()
	approvalToken := s.generateApprovalToken(requestID)
	declineToken := s.generateDeclineToken(requestID)
	embedTitle := "📚 New Book Request"
	message := fmt.Sprintf("📖 **%s**", title)
	if authorsStr != "" {
		message += fmt.Sprintf("\n👤 **Author(s):** %s", authorsStr)
	}
	message += fmt.Sprintf("\n🙋 **Requested by:** %s", username)
	message += fmt.Sprintf("\n🆔 **Request ID:** #%d", requestID)
	message += fmt.Sprintf("\n\n[✅ Approve Request](%s/approve/%s) | [❌ Decline Request](%s/approve/%s) | [📋 View All Requests](%s/requests)",
		currentCfg.ServerURL, approvalToken, currentCfg.ServerURL, declineToken, currentCfg.ServerURL)

	color := 0x3b82f6 // Blue color for new requests

	go func() {
		_ = s.sendDiscordNotification(cfg.Notifications.Discord.WebhookURL, cfg.Notifications.Discord.Username, embedTitle, message, color)
	}()
}

// SendApprovalNotification sends a notification when a request is approved
func (s *Server) SendApprovalNotification(username, title string, authors []string) {
	cfg := s.settings.Get()

	// Check if any notifications are enabled
	if cfg.Notifications.Provider == "" {
		return
	}

	authorsStr := strings.Join(authors, ", ")

	switch cfg.Notifications.Provider {
	case "ntfy":
		if !cfg.Notifications.Ntfy.EnableApprovalNotifications {
			return
		}
		s.sendApprovalNotificationNtfy(cfg, username, title, authorsStr)

	case "smtp":
		if !cfg.Notifications.SMTP.EnableApprovalNotifications {
			return
		}
		s.sendApprovalNotificationSMTP(cfg, username, title, authorsStr)

	case "discord":
		if !cfg.Notifications.Discord.EnableApprovalNotifications {
			return
		}
		s.sendApprovalNotificationDiscord(cfg, username, title, authorsStr)
	}
}

// sendApprovalNotificationNtfy sends ntfy notification for approved requests
func (s *Server) sendApprovalNotificationNtfy(cfg *config.Config, username, title, authorsStr string) {
	message := fmt.Sprintf("🎉 **%s**", title)
	if authorsStr != "" {
		message += fmt.Sprintf("\n👤 *by %s*", authorsStr)
	}
	message += fmt.Sprintf("\n✅ **Approved** for: **%s**", username)
	message += "\n\n📚 *Your request has been processed and should be available soon!*"

	// Create action button to view requests
	currentCfg := s.settings.Get()
	actions := []map[string]string{
		{
			"action": "view",
			"label":  "📋 View All Requests",
			"url":    currentCfg.ServerURL + "/requests",
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

// sendApprovalNotificationSMTP sends email notification for approved requests
func (s *Server) sendApprovalNotificationSMTP(cfg *config.Config, username, title, authorsStr string) {
	subject := "✅ Request Approved - Scriptorum"

	// HTML email content
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>Request Approved</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #333; margin: 0; padding: 20px; }
		.container { max-width: 600px; margin: 0 auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
		.header { background: #10b981; color: white; padding: 20px; text-align: center; }
		.content { padding: 20px; }
		.book-info { background: #f0fdf4; padding: 15px; border-radius: 6px; margin: 15px 0; border-left: 4px solid #10b981; }
		.actions { text-align: center; margin: 20px 0; }
		.button { display: inline-block; padding: 10px 20px; margin: 0 5px; text-decoration: none; border-radius: 5px; font-weight: bold; background: #6b7280; color: white; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>✅ Request Approved</h1>
		</div>
		<div class="content">
			<div class="book-info">
				<h2>🎉 %s</h2>
				%s
				<p><strong>✅ Approved for:</strong> %s</p>
				<p>📚 <em>Your request has been processed and should be available soon!</em></p>
			</div>
			<div class="actions">
				<a href="%s/requests" class="button">📋 View All Requests</a>
			</div>
		</div>
	</div>
</body>
</html>`, title,
		func() string {
			if authorsStr != "" {
				return fmt.Sprintf("<p><strong>👤 Author(s):</strong> %s</p>", authorsStr)
			}
			return ""
		}(),
		username, s.cfg.ServerURL)

	// Plain text content
	textBody := fmt.Sprintf(`✅ Request Approved - Scriptorum

🎉 %s
%s✅ Approved for: %s

📚 Your request has been processed and should be available soon!

View All Requests: %s/requests`,
		title,
		func() string {
			if authorsStr != "" {
				return fmt.Sprintf("👤 Author(s): %s\n", authorsStr)
			}
			return ""
		}(),
		username, s.cfg.ServerURL)

	go func() {
		_ = s.sendSMTPNotification(cfg.Notifications.SMTP, subject, htmlBody, textBody)
	}()
}

// sendApprovalNotificationDiscord sends Discord notification for approved requests
func (s *Server) sendApprovalNotificationDiscord(cfg *config.Config, username, title, authorsStr string) {
	embedTitle := "✅ Request Approved"
	message := fmt.Sprintf("🎉 **%s**", title)
	if authorsStr != "" {
		message += fmt.Sprintf("\n👤 **Author(s):** %s", authorsStr)
	}
	message += fmt.Sprintf("\n✅ **Approved for:** %s", username)
	message += "\n\n📚 *Your request has been processed and should be available soon!*"
	message += "\n\n[📋 View All Requests](" + s.cfg.ServerURL + "/requests)"

	color := 0x10b981 // Green color for approved requests

	go func() {
		_ = s.sendDiscordNotification(cfg.Notifications.Discord.WebhookURL, cfg.Notifications.Discord.Username, embedTitle, message, color)
	}()
}

// SendSystemNotification sends a system notification
func (s *Server) SendSystemNotification(title, message string) {
	cfg := s.settings.Get()

	// Check if any notifications are enabled
	if cfg.Notifications.Provider == "" {
		return
	}

	switch cfg.Notifications.Provider {
	case "ntfy":
		if !cfg.Notifications.Ntfy.EnableSystemNotifications {
			return
		}
		s.sendSystemNotificationNtfy(cfg, title, message)

	case "smtp":
		if !cfg.Notifications.SMTP.EnableSystemNotifications {
			return
		}
		s.sendSystemNotificationSMTP(cfg, title, message)

	case "discord":
		if !cfg.Notifications.Discord.EnableSystemNotifications {
			return
		}
		s.sendSystemNotificationDiscord(cfg, title, message)
	}
}

// sendSystemNotificationNtfy sends ntfy notification for system alerts
func (s *Server) sendSystemNotificationNtfy(cfg *config.Config, title, message string) {
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

// sendSystemNotificationSMTP sends email notification for system alerts
func (s *Server) sendSystemNotificationSMTP(cfg *config.Config, title, message string) {
	subject := fmt.Sprintf("🚨 %s - Scriptorum", title)

	// HTML email content
	htmlBody := fmt.Sprintf(`<!DOCTYPE html>
<html>
<head>
	<title>System Notification</title>
	<style>
		body { font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif; color: #333; margin: 0; padding: 20px; }
		.container { max-width: 600px; margin: 0 auto; background: #ffffff; border-radius: 8px; overflow: hidden; box-shadow: 0 4px 6px rgba(0, 0, 0, 0.1); }
		.header { background: #ef4444; color: white; padding: 20px; text-align: center; }
		.content { padding: 20px; }
		.alert-info { background: #fef2f2; padding: 15px; border-radius: 6px; margin: 15px 0; border-left: 4px solid #ef4444; }
		.actions { text-align: center; margin: 20px 0; }
		.button { display: inline-block; padding: 10px 20px; text-decoration: none; border-radius: 5px; font-weight: bold; background: #6b7280; color: white; }
	</style>
</head>
<body>
	<div class="container">
		<div class="header">
			<h1>🚨 %s</h1>
		</div>
		<div class="content">
			<div class="alert-info">
				<p>%s</p>
			</div>
			<div class="actions">
				<a href="%s" class="button">🌐 Open Scriptorum</a>
			</div>
		</div>
	</div>
</body>
</html>`, title, message, s.cfg.ServerURL)

	// Plain text content
	textBody := fmt.Sprintf(`🚨 %s - Scriptorum

%s

Open Scriptorum: %s`, title, message, s.cfg.ServerURL)

	go func() {
		_ = s.sendSMTPNotification(cfg.Notifications.SMTP, subject, htmlBody, textBody)
	}()
}

// sendSystemNotificationDiscord sends Discord notification for system alerts
func (s *Server) sendSystemNotificationDiscord(cfg *config.Config, title, message string) {
	embedTitle := fmt.Sprintf("🚨 %s", title)
	color := 0xef4444 // Red color for system alerts

	go func() {
		_ = s.sendDiscordNotification(cfg.Notifications.Discord.WebhookURL, cfg.Notifications.Discord.Username, embedTitle, message, color)
	}()
}

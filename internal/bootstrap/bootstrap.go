package bootstrap

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"os"
	"path/filepath"

	"gitea.knapp/jacoknapp/scriptorum/internal/config"
	"gitea.knapp/jacoknapp/scriptorum/internal/db"
)

func EnsureFirstRun(ctx context.Context, cfgPath, dbPath string) (*config.Config, *db.DB, error) {
	if err := os.MkdirAll(filepath.Dir(cfgPath), 0o755); err != nil {
		return nil, nil, err
	}
	if err := os.MkdirAll(filepath.Dir(dbPath), 0o755); err != nil {
		return nil, nil, err
	}

	if _, err := os.Stat(cfgPath); errors.Is(err, os.ErrNotExist) {
		cfg := defaultConfig(dbPath)
		if err := config.Save(cfgPath, cfg); err != nil {
			return nil, nil, err
		}
	}

	cfg, err := config.Load(cfgPath)
	if err != nil {
		return nil, nil, err
	}

	if dbPath != "" && dbPath != cfg.DB.Path {
		cfg.DB.Path = dbPath
		_ = config.Save(cfgPath, cfg)
	}

	database, err := db.Open(cfg.DB.Path)
	if err != nil {
		return nil, nil, err
	}
	if err := database.Migrate(ctx); err != nil {
		database.Close()
		return nil, nil, err
	}

	return cfg, database, nil
}

func defaultConfig(dbPath string) *config.Config {
	c := &config.Config{}

	// Mirror data/scriptorum.yaml defaults
	c.Debug = false
	c.HTTP.Listen = ":8491"
	c.DB.Path = dbPath
	// Mark setup as not completed so the initial-run setup wizard is shown
	// when the application creates the default config for the first time.
	c.Setup.Completed = false

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err == nil {
		c.Auth.Salt = fmt.Sprintf("%x", saltBytes)
	} else {
		c.Auth.Salt = "default_salt"
	}
	c.Admins.Usernames = []string{"admin"}

	c.OAuth.Enabled = false
	c.OAuth.Issuer = ""
	c.OAuth.ClientID = ""
	c.OAuth.RedirectURL = ""
	c.OAuth.Scopes = []string{"openid", "profile"}
	c.OAuth.UsernameClaim = "preferred_username"
	c.OAuth.AllowDomains = []string{}
	c.OAuth.AllowEmails = []string{}
	c.OAuth.CookieName = "scriptorum_session"
	c.OAuth.CookieDomain = ""
	c.OAuth.CookieSecure = false
	c.OAuth.CookieSecret = ""
	c.OAuth.AutoCreateUsers = false
	// Cookie handling (name/secret/etc.) is managed by the server and not stored in user config.

	c.AmazonPublic.Enabled = true

	// Readarr instances (values taken from your scriptorum.yaml)
	c.Readarr.Ebooks.BaseURL = ""
	c.Readarr.Ebooks.APIKey = ""
	c.Readarr.Ebooks.DefaultQualityProfileID = 1
	c.Readarr.Ebooks.DefaultRootFolderPath = "/books/ebooks"
	c.Readarr.Ebooks.DefaultTags = []string{}

	c.Readarr.Audiobooks.BaseURL = ""
	c.Readarr.Audiobooks.APIKey = ""
	c.Readarr.Audiobooks.DefaultQualityProfileID = 2
	c.Readarr.Audiobooks.DefaultRootFolderPath = "/books/audiobooks"
	c.Readarr.Audiobooks.DefaultTags = []string{}

	// Notifications defaults
	c.Notifications.Provider = ""
	c.Notifications.Ntfy.Server = "https://ntfy.sh"
	c.Notifications.Ntfy.Topic = ""
	c.Notifications.Ntfy.Username = ""
	c.Notifications.Ntfy.Password = ""
	c.Notifications.Ntfy.EnableRequestNotifications = false
	c.Notifications.Ntfy.EnableApprovalNotifications = false
	c.Notifications.Ntfy.EnableSystemNotifications = false

	// SMTP defaults
	c.Notifications.SMTP.Host = ""
	c.Notifications.SMTP.Port = 587
	c.Notifications.SMTP.Username = ""
	c.Notifications.SMTP.Password = ""
	c.Notifications.SMTP.FromEmail = ""
	c.Notifications.SMTP.FromName = "Scriptorum"
	c.Notifications.SMTP.ToEmail = ""
	c.Notifications.SMTP.EnableTLS = true
	c.Notifications.SMTP.EnableRequestNotifications = false
	c.Notifications.SMTP.EnableApprovalNotifications = false
	c.Notifications.SMTP.EnableSystemNotifications = false

	// Discord defaults
	c.Notifications.Discord.WebhookURL = ""
	c.Notifications.Discord.Username = "Scriptorum"
	c.Notifications.Discord.EnableRequestNotifications = false
	c.Notifications.Discord.EnableApprovalNotifications = false
	c.Notifications.Discord.EnableSystemNotifications = false

	return c
}

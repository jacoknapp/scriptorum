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
	c.HTTP.Listen = ":8080"
	c.DB.Path = dbPath
	c.Setup.Completed = true

	saltBytes := make([]byte, 16)
	if _, err := rand.Read(saltBytes); err == nil {
		c.Auth.Salt = fmt.Sprintf("%x", saltBytes)
	} else {
		c.Auth.Salt = "default_salt"
	}
	c.Admins.Emails = []string{"jacoknapp@gmail.com"}

	c.OAuth.Enabled = false
	c.OAuth.Issuer = ""
	c.OAuth.ClientID = ""
	c.OAuth.ClientSecret = ""
	c.OAuth.RedirectURL = ""
	c.OAuth.Scopes = []string{"openid", "profile", "email"}
	c.OAuth.AllowDomains = []string{}
	c.OAuth.AllowEmails = []string{}
	c.OAuth.CookieName = "scriptorum_session"
	c.OAuth.CookieDomain = ""
	c.OAuth.CookieSecure = false
	c.OAuth.CookieSecret = ""

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

	// Automations (match your YAML)
	c.Automations.PreferISBN13 = false
	c.Automations.AutoSearchForMissing = false
	c.Automations.TagRequester = false
	c.Automations.CreateAuthorIfMissing = false
	c.Automations.SeriesLinking = false
	c.Automations.RequireApproval = false
	c.Automations.AutoCompleteOnImport = false

	return c
}

func defaultAddTemplate() string {
	return `{
  "monitored": true,
  "qualityProfileId": {{ .Opts.QualityProfileID }},
  "rootFolderPath": "{{ .Opts.RootFolderPath }}",
  "addOptions": { "searchForMissingBooks": {{ if .Opts.SearchForMissing }}true{{ else }}false{{ end }} },
  "tags": {{ toJSON .Opts.Tags }},
  "title": {{ toJSON (index .Candidate "title") }},
  "titleSlug": {{ toJSON (index .Candidate "titleSlug") }},
	"foreignBookId": {{ toJSON (index .Candidate "foreignBookId") }},
	"foreignEditionId": {{ toJSON (index .Candidate "foreignEditionId") }},
  "author": {{ toJSON (index .Candidate "author") }},
  "editions": {{ toJSON (index .Candidate "editions") }}
}`
}

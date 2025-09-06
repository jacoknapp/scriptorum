package bootstrap

import (
	"context"
	"errors"
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
	c.HTTP.Listen = ":8080"
	c.DB.Path = dbPath
	c.Setup.Completed = false
	c.OAuth.Enabled = false
	c.OAuth.RedirectURL = "http://localhost:8080/oauth/callback"
	c.OAuth.Scopes = []string{"openid", "profile", "email"}
	c.OAuth.CookieName = "scriptorum_session"
	c.AmazonPublic.Enabled = true

	c.Audiobookshelf.Enabled = true
	c.Audiobookshelf.BaseURL = "http://audiobookshelf:13378"
	c.Audiobookshelf.SearchEndpoint = "/api/search?query={{urlquery .Term}}"

	c.Readarr.Ebooks.BaseURL = "http://readarr-ebooks:8787"
	c.Readarr.Ebooks.LookupEndpoint = "/api/v1/book/lookup"
	c.Readarr.Ebooks.AddEndpoint = "/api/v1/book"
	c.Readarr.Ebooks.AddMethod = "POST"
	c.Readarr.Ebooks.DefaultQualityProfileID = 1
	c.Readarr.Ebooks.DefaultRootFolderPath = "/books/ebooks"
	c.Readarr.Ebooks.DefaultTags = []string{"requested-by-abs"}
	c.Readarr.Ebooks.AddPayloadTemplate = defaultAddTemplate()

	c.Readarr.Audiobooks.BaseURL = "http://readarr-audio:8787"
	c.Readarr.Audiobooks.LookupEndpoint = "/api/v1/book/lookup"
	c.Readarr.Audiobooks.AddEndpoint = "/api/v1/book"
	c.Readarr.Audiobooks.AddMethod = "POST"
	c.Readarr.Audiobooks.DefaultQualityProfileID = 2
	c.Readarr.Audiobooks.DefaultRootFolderPath = "/books/audiobooks"
	c.Readarr.Audiobooks.DefaultTags = []string{"requested-by-abs", "audiobook"}
	c.Readarr.Audiobooks.AddPayloadTemplate = defaultAddTemplate()
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

package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

const (
	defaultReadarrAddEndpoint        = "/api/v1/book"
	defaultReadarrAddMethod          = "POST"
	defaultReadarrAddPayloadTemplate = `{
				"id": {{ if (index .Candidate "id") }}{{ toJSON (index .Candidate "id") }}{{ else }}0{{ end }},
				"title": {{ toJSON (index .Candidate "title") }},
				"authorTitle": {{ toJSON (index .Candidate "authorTitle") }},
				"seriesTitle": {{ toJSON (index .Candidate "seriesTitle") }},
				"disambiguation": {{ toJSON (index .Candidate "disambiguation") }},
				"overview": {{ toJSON (index .Candidate "overview") }},
				"authorId": {{ toJSON (index .Candidate "authorId") }},
				"foreignBookId": {{ toJSON (index .Candidate "foreignBookId") }},
				"foreignEditionId": {{ toJSON (index .Candidate "foreignEditionId") }},
				"titleSlug": {{ toJSON (index .Candidate "titleSlug") }},
				"monitored": {{ if (index .Candidate "monitored") }}{{ toJSON (index .Candidate "monitored") }}{{ else }}true{{ end }},
				"anyEditionOk": {{ if (index .Candidate "anyEditionOk") }}{{ toJSON (index .Candidate "anyEditionOk") }}{{ else }}true{{ end }},
				"ratings": {{ if (index .Candidate "ratings") }}{{ toJSON (index .Candidate "ratings") }}{{ else }}{"votes":0,"value":0}{{ end }},
				"releaseDate": {{ toJSON (index .Candidate "releaseDate") }},
				"pageCount": {{ if (index .Candidate "pageCount") }}{{ toJSON (index .Candidate "pageCount") }}{{ else }}0{{ end }},
				"genres": {{ if (index .Candidate "genres") }}{{ toJSON (index .Candidate "genres") }}{{ else }}[]{{ end }},
				"author": {{ toJSON (index .Candidate "author") }},
				"images": {{ if (index .Candidate "images") }}{{ toJSON (index .Candidate "images") }}{{ else }}[]{{ end }},
				"links": {{ if (index .Candidate "links") }}{{ toJSON (index .Candidate "links") }}{{ else }}[]{{ end }},
				"statistics": {{ if (index .Candidate "statistics") }}{{ toJSON (index .Candidate "statistics") }}{{ else }}{"bookFileCount":0,"bookCount":0,"totalBookCount":0,"sizeOnDisk":0}{{ end }},
				"added": {{ toJSON (index .Candidate "added") }},
				"addOptions": {
					"addType": {{ if (index (index .Candidate "addOptions") "addType") }}{{ toJSON (index (index .Candidate "addOptions") "addType") }}{{ else }}"automatic"{{ end }},
					"searchForNewBook": {{ if (index (index .Candidate "addOptions") "searchForNewBook") }}{{ toJSON (index (index .Candidate "addOptions") "searchForNewBook") }}{{ else }}true{{ end }},
					"monitor": "all",
					"monitored": true,
					"booksToMonitor": [],
					"searchForMissingBooks": {{ if .Opts.SearchForMissing }}true{{ else }}false{{ end }}
				},
				"remoteCover": {{ toJSON (index .Candidate "remoteCover") }},
				"lastSearchTime": {{ toJSON (index .Candidate "lastSearchTime") }},
				"editions": {{ if (index .Candidate "editions") }}{{ toJSON (index .Candidate "editions") }}{{ else }}[]{{ end }},
				"qualityProfileId": {{ if .Opts.QualityProfileID }}{{ .Opts.QualityProfileID }}{{ else }}{{ .Inst.DefaultQualityProfileID }}{{ end }},
				"rootFolderPath": "{{ .Opts.RootFolderPath }}",
				"tags": {{ toJSON .Opts.Tags }}
			}`
)

type Config struct {
	Debug bool `yaml:"debug"`
	HTTP  struct {
		Listen string `yaml:"listen"`
	} `yaml:"http"`
	DB struct {
		Path string `yaml:"path"`
	} `yaml:"db"`
	Setup struct {
		Completed bool `yaml:"completed"`
	} `yaml:"setup"`

	Auth struct {
		Salt string `yaml:"salt"`
	} `yaml:"auth"`

	Admins struct {
		Emails []string `yaml:"emails"`
	} `yaml:"admins"`

	OAuth struct {
		Enabled      bool     `yaml:"enabled"`
		Issuer       string   `yaml:"issuer"`
		ClientID     string   `yaml:"client_id"`
		ClientSecret string   `yaml:"client_secret"`
		RedirectURL  string   `yaml:"redirect_url"`
		Scopes       []string `yaml:"scopes"`
		AllowDomains []string `yaml:"allow_email_domains"`
		AllowEmails  []string `yaml:"allow_emails"`
		// Cookie-related settings are managed by the server and not exposed to user config
		// AutoCreateUsers will create a local user record on first OAuth login
		// using the OIDC email as the username. Password is random/unusable.
		AutoCreateUsers bool `yaml:"auto_create_users"`
	} `yaml:"oauth"`

	AmazonPublic struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"amazon_public"`

	// Audiobookshelf integration removed

	Readarr struct {
		Ebooks     ReadarrInstance `yaml:"ebooks"`
		Audiobooks ReadarrInstance `yaml:"audiobooks"`
	} `yaml:"readarr"`

	Automations struct {
		PreferISBN13          bool `yaml:"prefer_isbn13"`
		AutoSearchForMissing  bool `yaml:"auto_search_for_missing"`
		TagRequester          bool `yaml:"tag_requester"`
		CreateAuthorIfMissing bool `yaml:"create_author_if_missing"`
		SeriesLinking         bool `yaml:"series_linking"`
		RequireApproval       bool `yaml:"require_approval"`
		AutoCompleteOnImport  bool `yaml:"auto_complete_on_import"`
	} `yaml:"automations"`
}

type ReadarrInstance struct {
	BaseURL                 string   `yaml:"base_url"`
	APIKey                  string   `yaml:"api_key"`
	LookupEndpoint          string   `yaml:"lookup_endpoint"`
	AddEndpoint             string   `yaml:"add_endpoint"`
	AddMethod               string   `yaml:"add_method"`
	AddPayloadTemplate      string   `yaml:"add_payload_template"`
	DefaultQualityProfileID int      `yaml:"default_quality_profile_id"`
	DefaultRootFolderPath   string   `yaml:"default_root_folder_path"`
	DefaultTags             []string `yaml:"default_tags"`
}

func Load(path string) (*Config, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var cfg Config
	if err := yaml.Unmarshal(b, &cfg); err != nil {
		return nil, err
	}
	// Ensure Readarr instances have sensible defaults if not provided in YAML
	if cfg.Readarr.Ebooks.AddEndpoint == "" {
		cfg.Readarr.Ebooks.AddEndpoint = defaultReadarrAddEndpoint
	}
	if cfg.Readarr.Ebooks.AddMethod == "" {
		cfg.Readarr.Ebooks.AddMethod = defaultReadarrAddMethod
	}
	if cfg.Readarr.Ebooks.AddPayloadTemplate == "" {
		cfg.Readarr.Ebooks.AddPayloadTemplate = defaultReadarrAddPayloadTemplate
	}
	// Ensure lookup endpoints have sensible defaults
	if cfg.Readarr.Ebooks.LookupEndpoint == "" {
		cfg.Readarr.Ebooks.LookupEndpoint = "/api/v1/book/lookup"
	}
	if cfg.Readarr.Audiobooks.AddEndpoint == "" {
		cfg.Readarr.Audiobooks.AddEndpoint = defaultReadarrAddEndpoint
	}
	if cfg.Readarr.Audiobooks.AddMethod == "" {
		cfg.Readarr.Audiobooks.AddMethod = defaultReadarrAddMethod
	}
	if cfg.Readarr.Audiobooks.AddPayloadTemplate == "" {
		cfg.Readarr.Audiobooks.AddPayloadTemplate = defaultReadarrAddPayloadTemplate
	}
	if cfg.Readarr.Audiobooks.LookupEndpoint == "" {
		cfg.Readarr.Audiobooks.LookupEndpoint = "/api/v1/book/lookup"
	}
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

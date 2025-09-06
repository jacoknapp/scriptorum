package config

import (
	"os"

	"gopkg.in/yaml.v3"
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
		CookieName   string   `yaml:"cookie_name"`
		CookieDomain string   `yaml:"cookie_domain"`
		CookieSecure bool     `yaml:"cookie_secure"`
		CookieSecret string   `yaml:"cookie_secret"`
	} `yaml:"oauth"`

	AmazonPublic struct {
		Enabled bool `yaml:"enabled"`
	} `yaml:"amazon_public"`

	Audiobookshelf struct {
		Enabled        bool   `yaml:"enabled"`
		BaseURL        string `yaml:"base_url"`
		Token          string `yaml:"token"`
		SearchEndpoint string `yaml:"search_endpoint"`
	} `yaml:"audiobookshelf"`

	Readarr struct {
		Ebooks     ReadarrInstance `yaml:"ebooks"`
		Audiobooks ReadarrInstance `yaml:"audiobooks"`
	} `yaml:"readarr"`

	Automations struct {
		PresenceCheckABS      bool `yaml:"presence_check_abs"`
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
	return &cfg, nil
}

func Save(path string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

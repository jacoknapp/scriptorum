package config

import (
	"os"

	"gopkg.in/yaml.v3"
)

type Config struct {
	Debug     bool   `yaml:"debug"`
	ServerURL string `yaml:"server_url"`
	HTTP      struct {
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
		Usernames []string `yaml:"usernames"`
		// Back-compat: allow reading legacy admins.emails and map into usernames on load
		Emails []string `yaml:"emails,omitempty"`
	} `yaml:"admins"`

	OAuth struct {
		Enabled      bool   `yaml:"enabled"`
		Issuer       string `yaml:"issuer"`
		ClientID     string `yaml:"client_id"`
		ClientSecret string `yaml:"client_secret"`
		RedirectURL  string `yaml:"redirect_url"`
		// Optional overrides for provider-discovered endpoints; normally not needed.
		AuthURL  string   `yaml:"auth_url,omitempty"`
		TokenURL string   `yaml:"token_url,omitempty"`
		Scopes   []string `yaml:"scopes"`
		// UsernameClaim selects the OIDC claim used as the username (e.g. "preferred_username").
		UsernameClaim string `yaml:"username_claim"`
		// Legacy allowlists retained for backward-compatibility; not used anymore.
		AllowDomains []string `yaml:"allow_email_domains,omitempty"`
		AllowEmails  []string `yaml:"allow_emails,omitempty"`
		// Cookie configuration for production environments
		CookieName   string `yaml:"cookie_name,omitempty"`
		CookieDomain string `yaml:"cookie_domain,omitempty"`
		CookieSecure bool   `yaml:"cookie_secure,omitempty"`
		CookieSecret string `yaml:"cookie_secret,omitempty"`
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

	Notifications struct {
		Ntfy    NtfyConfig    `yaml:"ntfy"`
		SMTP    SMTPConfig    `yaml:"smtp"`
		Discord DiscordConfig `yaml:"discord"`
	} `yaml:"notifications"`
}

type NtfyConfig struct {
	Enabled                     bool   `yaml:"enabled"`
	Server                      string `yaml:"server"`
	Topic                       string `yaml:"topic"`
	Username                    string `yaml:"username"`
	Password                    string `yaml:"password"`
	EnableRequestNotifications  bool   `yaml:"enable_request_notifications"`
	EnableApprovalNotifications bool   `yaml:"enable_approval_notifications"`
	EnableSystemNotifications   bool   `yaml:"enable_system_notifications"`
}

type SMTPConfig struct {
	Enabled                     bool   `yaml:"enabled"`
	Host                        string `yaml:"host"`
	Port                        int    `yaml:"port"`
	Username                    string `yaml:"username"`
	Password                    string `yaml:"password"`
	FromEmail                   string `yaml:"from_email"`
	FromName                    string `yaml:"from_name"`
	ToEmail                     string `yaml:"to_email"`
	EnableTLS                   bool   `yaml:"enable_tls"`
	EnableRequestNotifications  bool   `yaml:"enable_request_notifications"`
	EnableApprovalNotifications bool   `yaml:"enable_approval_notifications"`
	EnableSystemNotifications   bool   `yaml:"enable_system_notifications"`
}

type DiscordConfig struct {
	Enabled                     bool   `yaml:"enabled"`
	WebhookURL                  string `yaml:"webhook_url"`
	Username                    string `yaml:"username"`
	EnableRequestNotifications  bool   `yaml:"enable_request_notifications"`
	EnableApprovalNotifications bool   `yaml:"enable_approval_notifications"`
	EnableSystemNotifications   bool   `yaml:"enable_system_notifications"`
}

type ReadarrInstance struct {
	BaseURL                 string   `yaml:"base_url"`
	APIKey                  string   `yaml:"api_key"`
	DefaultQualityProfileID int      `yaml:"default_quality_profile_id"`
	DefaultRootFolderPath   string   `yaml:"default_root_folder_path"`
	DefaultTags             []string `yaml:"default_tags"`
	// If true, the Readarr HTTP client will skip TLS certificate verification.
	InsecureSkipVerify bool `yaml:"insecure_skip_verify"`
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
	// Migrate legacy admin emails to usernames if needed
	if len(cfg.Admins.Usernames) == 0 && len(cfg.Admins.Emails) > 0 {
		cfg.Admins.Usernames = append(cfg.Admins.Usernames, cfg.Admins.Emails...)
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

package config

import (
	"os"
	"strings"

	"gopkg.in/yaml.v3"
)

var discoveryLanguageAliases = map[string]string{
	"en":  "eng",
	"eng": "eng",
	"es":  "spa",
	"spa": "spa",
	"fr":  "fre",
	"fre": "fre",
	"de":  "ger",
	"ger": "ger",
	"it":  "ita",
	"ita": "ita",
	"pt":  "por",
	"por": "por",
	"nl":  "dut",
	"dut": "dut",
	"sv":  "swe",
	"swe": "swe",
	"no":  "nor",
	"nor": "nor",
	"da":  "dan",
	"dan": "dan",
	"fi":  "fin",
	"fin": "fin",
	"pl":  "pol",
	"pol": "pol",
	"cs":  "cze",
	"cze": "cze",
	"hu":  "hun",
	"hun": "hun",
	"ro":  "rum",
	"rum": "rum",
	"bg":  "bul",
	"bul": "bul",
	"el":  "gre",
	"gre": "gre",
	"ru":  "rus",
	"rus": "rus",
	"uk":  "ukr",
	"ukr": "ukr",
	"ar":  "ara",
	"ara": "ara",
	"he":  "heb",
	"heb": "heb",
	"hi":  "hin",
	"hin": "hin",
	"bn":  "ben",
	"ben": "ben",
	"ta":  "tam",
	"tam": "tam",
	"te":  "tel",
	"tel": "tel",
	"ml":  "mal",
	"mal": "mal",
	"mr":  "mar",
	"mar": "mar",
	"gu":  "guj",
	"guj": "guj",
	"pa":  "pan",
	"pan": "pan",
	"ur":  "urd",
	"urd": "urd",
	"tr":  "tur",
	"tur": "tur",
	"fa":  "per",
	"per": "per",
	"zh":  "chi",
	"chi": "chi",
	"ja":  "jpn",
	"jpn": "jpn",
	"ko":  "kor",
	"kor": "kor",
	"th":  "tha",
	"tha": "tha",
	"vi":  "vie",
	"vie": "vie",
	"id":  "ind",
	"ind": "ind",
}

type Config struct {
	Debug     bool   `yaml:"debug"`
	ServerURL string `yaml:"server_url"`
	Discovery struct {
		Languages []string `yaml:"languages"`
	} `yaml:"discovery"`
	HTTP struct {
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
	cfg.Discovery.Languages = NormalizeDiscoveryLanguages(cfg.Discovery.Languages)
	return &cfg, nil
}

func DefaultDiscoveryLanguages() []string {
	return []string{"eng"}
}

func NormalizeDiscoveryLanguages(input []string) []string {
	if len(input) == 0 {
		return DefaultDiscoveryLanguages()
	}
	seen := make(map[string]struct{}, len(input))
	out := make([]string, 0, len(input))
	for _, raw := range input {
		code := strings.ToLower(strings.TrimSpace(raw))
		if code == "" {
			continue
		}
		canonical, ok := discoveryLanguageAliases[code]
		if !ok {
			continue
		}
		if _, ok := seen[canonical]; ok {
			continue
		}
		seen[canonical] = struct{}{}
		out = append(out, canonical)
	}
	if len(out) == 0 {
		return DefaultDiscoveryLanguages()
	}
	return out
}

func Save(path string, cfg *Config) error {
	b, err := yaml.Marshal(cfg)
	if err != nil {
		return err
	}
	return os.WriteFile(path, b, 0o644)
}

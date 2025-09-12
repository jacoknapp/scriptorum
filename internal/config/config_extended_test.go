package config

import (
	"os"
	"path/filepath"
	"testing"
)

// Test config loading from file
func TestLoadConfig(t *testing.T) {
	// Create a temporary config file
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "test_config.yaml")

	configContent := `debug: true
http:
  listen: :8080
db:
  path: /test/db.sqlite
auth:
  salt: test-salt
admins:
  usernames:
    - testuser
oauth:
  enabled: false
amazon_public:
  enabled: true
readarr:
  ebooks:
    base_url: http://readarr:8787
    api_key: testkey
    default_quality_profile_id: 1
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write test config: %v", err)
	}

	// Load the config
	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify loaded values
	if !cfg.Debug {
		t.Error("Expected debug to be true")
	}

	if cfg.HTTP.Listen != ":8080" {
		t.Errorf("Expected listen port :8080, got %s", cfg.HTTP.Listen)
	}

	if cfg.DB.Path != "/test/db.sqlite" {
		t.Errorf("Expected db path /test/db.sqlite, got %s", cfg.DB.Path)
	}

	if cfg.Auth.Salt != "test-salt" {
		t.Errorf("Expected auth salt test-salt, got %s", cfg.Auth.Salt)
	}

	if len(cfg.Admins.Usernames) != 1 || cfg.Admins.Usernames[0] != "testuser" {
		t.Errorf("Expected admin username testuser, got %v", cfg.Admins.Usernames)
	}
}

// Test config saving to file
func TestSaveConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "save_test.yaml")

	// Create a test config
	cfg := &Config{
		Debug: true,
		HTTP: struct {
			Listen string `yaml:"listen"`
		}{
			Listen: ":9090",
		},
		DB: struct {
			Path string `yaml:"path"`
		}{
			Path: "/test/save.sqlite",
		},
		Auth: struct {
			Salt string `yaml:"salt"`
		}{
			Salt: "save-test-salt",
		},
		Admins: struct {
			Usernames []string `yaml:"usernames"`
			Emails    []string `yaml:"emails,omitempty"`
		}{
			Usernames: []string{"saveuser"},
		},
		OAuth: struct {
			Enabled         bool     `yaml:"enabled"`
			Issuer          string   `yaml:"issuer"`
			ClientID        string   `yaml:"client_id"`
			ClientSecret    string   `yaml:"client_secret"`
			RedirectURL     string   `yaml:"redirect_url"`
			AuthURL         string   `yaml:"auth_url,omitempty"`
			TokenURL        string   `yaml:"token_url,omitempty"`
			Scopes          []string `yaml:"scopes"`
			UsernameClaim   string   `yaml:"username_claim"`
			AllowDomains    []string `yaml:"allow_email_domains,omitempty"`
			AllowEmails     []string `yaml:"allow_emails,omitempty"`
			CookieName      string   `yaml:"cookie_name,omitempty"`
			CookieDomain    string   `yaml:"cookie_domain,omitempty"`
			CookieSecure    bool     `yaml:"cookie_secure,omitempty"`
			CookieSecret    string   `yaml:"cookie_secret,omitempty"`
			AutoCreateUsers bool     `yaml:"auto_create_users"`
		}{
			Enabled: false,
		},
		AmazonPublic: struct {
			Enabled bool `yaml:"enabled"`
		}{
			Enabled: true,
		},
	}

	// Save the config
	err := Save(configPath, cfg)
	if err != nil {
		t.Fatalf("Failed to save config: %v", err)
	}

	// Verify file was created
	if _, err := os.Stat(configPath); os.IsNotExist(err) {
		t.Fatal("Config file was not created")
	}

	// Load it back and verify
	loadedCfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load saved config: %v", err)
	}

	if loadedCfg.Debug != cfg.Debug {
		t.Error("Debug value not preserved")
	}

	if loadedCfg.HTTP.Listen != cfg.HTTP.Listen {
		t.Error("HTTP listen value not preserved")
	}

	if loadedCfg.DB.Path != cfg.DB.Path {
		t.Error("DB path value not preserved")
	}
}

// Test config validation
func TestConfigValidation(t *testing.T) {
	// Test with invalid YAML
	tempDir := t.TempDir()
	invalidPath := filepath.Join(tempDir, "invalid.yaml")

	invalidContent := `debug: true
http:
  listen: :8080
invalid_yaml: [unclosed bracket
`

	err := os.WriteFile(invalidPath, []byte(invalidContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write invalid config: %v", err)
	}

	_, err = Load(invalidPath)
	if err == nil {
		t.Error("Expected error when loading invalid YAML")
	}
}

// Test loading non-existent config file
func TestLoadNonExistentConfig(t *testing.T) {
	_, err := Load("/non/existent/path.yaml")
	if err == nil {
		t.Error("Expected error when loading non-existent config")
	}
}

// Test config with empty values
func TestConfigWithEmptyValues(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "empty_test.yaml")

	// Create minimal config
	configContent := `debug: false
http:
  listen: ""
db:
  path: ""
auth:
  salt: ""
admins:
  usernames: []
oauth:
  enabled: false
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write empty config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load empty config: %v", err)
	}

	if cfg.Debug != false {
		t.Error("Expected debug to be false")
	}

	if cfg.HTTP.Listen != "" {
		t.Error("Expected empty listen string")
	}

	if len(cfg.Admins.Usernames) != 0 {
		t.Error("Expected empty usernames array")
	}
}

// Test Readarr instance configuration
func TestReadarrInstanceConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "readarr_test.yaml")

	configContent := `readarr:
  ebooks:
    base_url: https://ebooks.example.com
    api_key: ebooks-key
    default_quality_profile_id: 1
    default_root_folder_path: /books/ebooks
    default_tags: [1, 2]
    insecure_skip_verify: true
  audiobooks:
    base_url: https://audiobooks.example.com
    api_key: audiobooks-key
    default_quality_profile_id: 2
    default_root_folder_path: /books/audiobooks
    default_tags: [3, 4]
    insecure_skip_verify: false
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write readarr config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load readarr config: %v", err)
	}

	// Test ebooks config
	if cfg.Readarr.Ebooks.BaseURL != "https://ebooks.example.com" {
		t.Errorf("Expected ebooks base URL, got %s", cfg.Readarr.Ebooks.BaseURL)
	}

	if cfg.Readarr.Ebooks.APIKey != "ebooks-key" {
		t.Errorf("Expected ebooks API key, got %s", cfg.Readarr.Ebooks.APIKey)
	}

	if cfg.Readarr.Ebooks.DefaultQualityProfileID != 1 {
		t.Errorf("Expected quality profile ID 1, got %d", cfg.Readarr.Ebooks.DefaultQualityProfileID)
	}

	if !cfg.Readarr.Ebooks.InsecureSkipVerify {
		t.Error("Expected insecure skip verify to be true for ebooks")
	}

	// Test audiobooks config
	if cfg.Readarr.Audiobooks.BaseURL != "https://audiobooks.example.com" {
		t.Errorf("Expected audiobooks base URL, got %s", cfg.Readarr.Audiobooks.BaseURL)
	}

	if cfg.Readarr.Audiobooks.InsecureSkipVerify {
		t.Error("Expected insecure skip verify to be false for audiobooks")
	}
}

// Test OAuth configuration
func TestOAuthConfig(t *testing.T) {
	tempDir := t.TempDir()
	configPath := filepath.Join(tempDir, "oauth_test.yaml")

	configContent := `oauth:
  enabled: true
  issuer: https://auth.example.com
  client_id: test-client
  client_secret: test-secret
  redirect_url: https://app.example.com/callback
  scopes:
    - openid
    - profile
    - email
  username_claim: preferred_username
  cookie_name: auth_session
  cookie_domain: .example.com
  cookie_secure: true
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	if err != nil {
		t.Fatalf("Failed to write oauth config: %v", err)
	}

	cfg, err := Load(configPath)
	if err != nil {
		t.Fatalf("Failed to load oauth config: %v", err)
	}

	if !cfg.OAuth.Enabled {
		t.Error("Expected OAuth to be enabled")
	}

	if cfg.OAuth.Issuer != "https://auth.example.com" {
		t.Errorf("Expected issuer URL, got %s", cfg.OAuth.Issuer)
	}

	if cfg.OAuth.ClientID != "test-client" {
		t.Errorf("Expected client ID, got %s", cfg.OAuth.ClientID)
	}

	if len(cfg.OAuth.Scopes) != 3 {
		t.Errorf("Expected 3 scopes, got %d", len(cfg.OAuth.Scopes))
	}

	if cfg.OAuth.UsernameClaim != "preferred_username" {
		t.Errorf("Expected username claim, got %s", cfg.OAuth.UsernameClaim)
	}

	if cfg.OAuth.CookieName != "auth_session" {
		t.Errorf("Expected cookie name, got %s", cfg.OAuth.CookieName)
	}

	if !cfg.OAuth.CookieSecure {
		t.Error("Expected cookie secure to be true")
	}
}

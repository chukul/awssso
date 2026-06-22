package main

import (
	"crypto/sha1"
	"encoding/hex"
	"os"
	"path/filepath"
	"testing"
)

func TestLoadAWSConfigFromPath(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "aws-config-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	configContent := `
[profile test-profile]
sso_session = test-session
sso_account_id = 123456789012
sso_role_name = TestRole
region = us-west-2

[sso-session test-session]
sso_start_url = https://test.awsapps.com/start/
sso_region = us-west-2

[profile dev-sandbox]
sso_start_url = https://dev.awsapps.com/start/
sso_region = eu-west-1
sso_account_id = 999888777666
sso_role_name = DevAdmin
region = eu-west-1

[default]
region = us-east-1
`
	configPath := filepath.Join(tempDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := loadAWSConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Verify profiles parsed correctly
	if len(config.Profiles) != 3 {
		t.Errorf("Expected 3 profiles, got %d", len(config.Profiles))
	}

	p, ok := config.Profiles["test-profile"]
	if !ok {
		t.Fatal("Expected test-profile to exist")
	}
	if p.SSOSession != "test-session" {
		t.Errorf("Expected sso_session=test-session, got %q", p.SSOSession)
	}
	if p.SSOAccountID != "123456789012" {
		t.Errorf("Expected account 123456789012, got %q", p.SSOAccountID)
	}

	// Verify sessions parsed correctly
	if len(config.Sessions) != 1 {
		t.Errorf("Expected 1 session, got %d", len(config.Sessions))
	}

	s, ok := config.Sessions["test-session"]
	if !ok {
		t.Fatal("Expected test-session to exist")
	}
	if s.SSOStartURL != "https://test.awsapps.com/start/" {
		t.Errorf("Expected start URL, got %q", s.SSOStartURL)
	}

	// Verify default profile
	def, ok := config.Profiles["default"]
	if !ok {
		t.Fatal("Expected default profile")
	}
	if def.Region != "us-east-1" {
		t.Errorf("Expected region us-east-1, got %q", def.Region)
	}

	// Verify inline SSO profile
	devP, ok := config.Profiles["dev-sandbox"]
	if !ok {
		t.Fatal("Expected dev-sandbox profile")
	}
	if devP.SSOStartURL != "https://dev.awsapps.com/start/" {
		t.Errorf("Expected start URL, got %q", devP.SSOStartURL)
	}
}

func TestLoadAWSConfigFromPath_NotExist(t *testing.T) {
	config, err := loadAWSConfigFromPath("/nonexistent/path/config")
	if err != nil {
		t.Fatalf("Expected no error for missing file, got: %v", err)
	}
	if len(config.Profiles) != 0 {
		t.Errorf("Expected 0 profiles, got %d", len(config.Profiles))
	}
}

func TestGetSSOTokenPath(t *testing.T) {
	config := &AWSConfig{
		Profiles: map[string]*AWSProfile{},
		Sessions: map[string]*SSOSession{
			"test-session": {
				Name:        "test-session",
				SSOStartURL: "https://test.awsapps.com/start/",
				SSORegion:   "us-west-2",
			},
		},
	}

	// Test with sso_session — hash source is "test-session"
	profile := &AWSProfile{
		Name:       "test-profile",
		SSOSession: "test-session",
	}

	path, err := getSSOTokenPath(profile, config)
	if err != nil {
		t.Fatalf("Failed to get token path: %v", err)
	}
	if path == "" {
		t.Fatal("Expected non-empty path")
	}

	// SHA1("test-session") = 70780fa17c57df49aa7c7f603fde6ea7a14148f2
	h := sha1.New()
	h.Write([]byte("test-session"))
	expectedHash := hex.EncodeToString(h.Sum(nil))

	baseName := filepath.Base(path)
	expectedBase := expectedHash + ".json"
	if baseName != expectedBase {
		t.Errorf("Expected base name %q, got %q", expectedBase, baseName)
	}
}

func TestGetSSOTokenPath_InlineSSO(t *testing.T) {
	config := &AWSConfig{
		Profiles: map[string]*AWSProfile{},
		Sessions: map[string]*SSOSession{},
	}

	profile := &AWSProfile{
		Name:        "inline-profile",
		SSOStartURL: "https://my-org.awsapps.com/start/",
	}

	path, err := getSSOTokenPath(profile, config)
	if err != nil {
		t.Fatalf("Failed to get token path: %v", err)
	}

	h := sha1.New()
	h.Write([]byte("https://my-org.awsapps.com/start/"))
	expectedHash := hex.EncodeToString(h.Sum(nil))
	expectedBase := expectedHash + ".json"

	if filepath.Base(path) != expectedBase {
		t.Errorf("Expected base %q, got %q", expectedBase, filepath.Base(path))
	}
}

func TestGetSSOTokenPath_NoSSO(t *testing.T) {
	config := &AWSConfig{
		Profiles: map[string]*AWSProfile{},
		Sessions: map[string]*SSOSession{},
	}

	profile := &AWSProfile{
		Name: "bare-profile",
	}

	_, err := getSSOTokenPath(profile, config)
	if err == nil {
		t.Fatal("Expected error for profile with no SSO config")
	}
}

func TestSSOTokenExpiry(t *testing.T) {
	expired := &SSOToken{ExpiresAt: "2020-01-01T00:00:00Z"}
	if !expired.IsExpired() {
		t.Error("Expected expired token to report true")
	}

	valid := &SSOToken{ExpiresAt: "2099-01-01T00:00:00Z"}
	if valid.IsExpired() {
		t.Error("Expected non-expired token to report false")
	}

	empty := &SSOToken{ExpiresAt: ""}
	if !empty.IsExpired() {
		t.Error("Expected empty expiry to report expired")
	}

	malformed := &SSOToken{ExpiresAt: "not-a-date"}
	if !malformed.IsExpired() {
		t.Error("Expected malformed expiry to report expired")
	}
}

func TestSessionNameFromURL(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://d-123456789.awsapps.com/start/", "d-123456789"},
		{"https://my-org.awsapps.com/start/", "my-org"},
		{"http://localhost.test.com/start", "localhost"},
	}

	for _, tt := range tests {
		result := sessionNameFromURL(tt.input)
		if result != tt.expected {
			t.Errorf("sessionNameFromURL(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestResolveSSORegion(t *testing.T) {
	config := &AWSConfig{
		Sessions: map[string]*SSOSession{
			"my-session": {SSORegion: "us-west-2"},
		},
		Profiles: map[string]*AWSProfile{},
	}

	// From session
	p := &AWSProfile{SSOSession: "my-session"}
	if got := resolveSSORegion(p, config, nil); got != "us-west-2" {
		t.Errorf("Expected us-west-2, got %q", got)
	}

	// From profile inline
	p2 := &AWSProfile{SSORegion: "eu-west-1"}
	if got := resolveSSORegion(p2, config, nil); got != "eu-west-1" {
		t.Errorf("Expected eu-west-1, got %q", got)
	}

	// From token fallback
	p3 := &AWSProfile{}
	token := &SSOToken{Region: "ap-southeast-1"}
	if got := resolveSSORegion(p3, config, token); got != "ap-southeast-1" {
		t.Errorf("Expected ap-southeast-1, got %q", got)
	}
}

func TestLoadAWSConfigFromPath_MultiSessionSameURL(t *testing.T) {
	tempDir, err := os.MkdirTemp("", "aws-multi-session-test")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(tempDir)

	// Two sessions pointing to the same start URL but different identities
	configContent := `
[sso-session team-alpha]
sso_start_url = https://d-shared.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = alice@company.com

[sso-session team-beta]
sso_start_url = https://d-shared.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = bob@company.com

[profile alpha-dev]
sso_session = team-alpha
sso_account_id = 111111111111
sso_role_name = DeveloperAccess
region = eu-west-1

[profile beta-prod]
sso_session = team-beta
sso_account_id = 222222222222
sso_role_name = AdminAccess
region = us-east-1
`
	configPath := filepath.Join(tempDir, "config")
	if err := os.WriteFile(configPath, []byte(configContent), 0600); err != nil {
		t.Fatal(err)
	}

	config, err := loadAWSConfigFromPath(configPath)
	if err != nil {
		t.Fatalf("Failed to load config: %v", err)
	}

	// Both sessions should exist
	if len(config.Sessions) != 2 {
		t.Fatalf("Expected 2 sessions, got %d", len(config.Sessions))
	}

	alpha := config.Sessions["team-alpha"]
	beta := config.Sessions["team-beta"]

	// Same start URL
	if alpha.SSOStartURL != beta.SSOStartURL {
		t.Error("Expected same start URL for both sessions")
	}

	// Different emails
	if alpha.SSOAccountEmail != "alice@company.com" {
		t.Errorf("Expected alice@company.com, got %q", alpha.SSOAccountEmail)
	}
	if beta.SSOAccountEmail != "bob@company.com" {
		t.Errorf("Expected bob@company.com, got %q", beta.SSOAccountEmail)
	}

	// Token paths should be different (hashed by session name, not URL)
	alphaProfile := config.Profiles["alpha-dev"]
	betaProfile := config.Profiles["beta-prod"]

	alphaPath, _ := getSSOTokenPath(alphaProfile, config)
	betaPath, _ := getSSOTokenPath(betaProfile, config)

	if alphaPath == betaPath {
		t.Error("Expected different token paths for sessions with same URL but different names")
	}
}

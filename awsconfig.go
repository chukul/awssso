package main

import (
	"bufio"
	"crypto/sha1"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// AWSProfile represents a single [profile ...] section in ~/.aws/config.
type AWSProfile struct {
	Name         string
	SSOSession   string
	SSOStartURL  string
	SSORegion    string
	SSOAccountID string
	SSORoleName  string
	Region       string
}

// SSOSession represents a [sso-session ...] section in ~/.aws/config.
type SSOSession struct {
	Name            string
	SSOStartURL     string
	SSORegion       string
	SSOAccountEmail string // Optional: tracks which identity/email is used for this session
}

// SSOToken represents cached SSO token data stored in ~/.aws/sso/cache/.
type SSOToken struct {
	StartURL              string `json:"startUrl"`
	Region                string `json:"region"`
	AccessToken           string `json:"accessToken"`
	ExpiresAt             string `json:"expiresAt"`
	ClientID              string `json:"clientId,omitempty"`
	ClientSecret          string `json:"clientSecret,omitempty"`
	RegistrationExpiresAt string `json:"registrationExpiresAt,omitempty"`
	RefreshToken          string `json:"refreshToken,omitempty"`
}

// AWSConfig holds all parsed profiles and SSO sessions from ~/.aws/config.
type AWSConfig struct {
	Profiles map[string]*AWSProfile
	Sessions map[string]*SSOSession
}

// IsExpired returns true if the token has expired or will expire within 1 minute.
func (t *SSOToken) IsExpired() bool {
	if t.ExpiresAt == "" {
		return true
	}
	parsed, err := time.Parse(time.RFC3339, t.ExpiresAt)
	if err != nil {
		return true
	}
	return time.Now().Add(1 * time.Minute).After(parsed)
}

func resolveSSORegion(profile *AWSProfile, config *AWSConfig, token *SSOToken) string {
	if profile.SSOSession != "" {
		if session, found := config.Sessions[profile.SSOSession]; found {
			return session.SSORegion
		}
	}
	if profile.SSORegion != "" {
		return profile.SSORegion
	}
	if token != nil && token.Region != "" {
		return token.Region
	}
	return ""
}

func resolveStartURL(profile *AWSProfile, config *AWSConfig) string {
	if profile.SSOSession != "" {
		if session, found := config.Sessions[profile.SSOSession]; found {
			return session.SSOStartURL
		}
	}
	return profile.SSOStartURL
}

// cachedHomeDir stores the user home directory to avoid repeated syscalls.
var cachedHomeDir string

// homeDir returns the user's home directory, caching the result after the first call.
func homeDir() (string, error) {
	if cachedHomeDir != "" {
		return cachedHomeDir, nil
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("failed to get user home directory: %w", err)
	}
	cachedHomeDir = home
	return home, nil
}

// getAWSConfigPath returns the path to ~/.aws/config.
func getAWSConfigPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "config"), nil
}

// loadAWSConfig loads and parses the AWS config from the default location.
func loadAWSConfig() (*AWSConfig, error) {
	configPath, err := getAWSConfigPath()
	if err != nil {
		return nil, err
	}
	return loadAWSConfigFromPath(configPath)
}

// loadAWSConfigFromPath loads and parses an AWS config from a specific file path.
// This is separated from loadAWSConfig to enable testing without touching the real config.
func loadAWSConfigFromPath(configPath string) (*AWSConfig, error) {
	file, err := os.Open(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return &AWSConfig{
				Profiles: map[string]*AWSProfile{},
				Sessions: map[string]*SSOSession{},
			}, nil
		}
		return nil, err
	}
	defer file.Close()

	config := &AWSConfig{
		Profiles: map[string]*AWSProfile{},
		Sessions: map[string]*SSOSession{},
	}

	scanner := bufio.NewScanner(file)
	var currentProfile *AWSProfile
	var currentSession *SSOSession

	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, ";") || strings.HasPrefix(line, "#") {
			continue
		}

		if strings.HasPrefix(line, "[") && strings.HasSuffix(line, "]") {
			currentProfile = nil
			currentSession = nil

			section := strings.TrimSpace(line[1 : len(line)-1])
			switch {
			case strings.HasPrefix(section, "profile "):
				profileName := strings.TrimPrefix(section, "profile ")
				currentProfile = &AWSProfile{Name: profileName}
				config.Profiles[profileName] = currentProfile
			case section == "default":
				currentProfile = &AWSProfile{Name: "default"}
				config.Profiles["default"] = currentProfile
			case strings.HasPrefix(section, "sso-session "):
				sessionName := strings.TrimPrefix(section, "sso-session ")
				currentSession = &SSOSession{Name: sessionName}
				config.Sessions[sessionName] = currentSession
			}
			continue
		}

		parts := strings.SplitN(line, "=", 2)
		if len(parts) != 2 {
			continue
		}

		key := strings.TrimSpace(parts[0])
		val := strings.TrimSpace(parts[1])

		if currentProfile != nil {
			switch key {
			case "sso_session":
				currentProfile.SSOSession = val
			case "sso_start_url":
				currentProfile.SSOStartURL = val
			case "sso_region":
				currentProfile.SSORegion = val
			case "sso_account_id":
				currentProfile.SSOAccountID = val
			case "sso_role_name":
				currentProfile.SSORoleName = val
			case "region":
				currentProfile.Region = val
			}
		} else if currentSession != nil {
			switch key {
			case "sso_start_url":
				currentSession.SSOStartURL = val
			case "sso_region":
				currentSession.SSORegion = val
			case "sso_account_email":
				currentSession.SSOAccountEmail = val
			}
		}
	}

	return config, scanner.Err()
}

func getSSOTokenPath(profile *AWSProfile, config *AWSConfig) (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}

	var hashSource string
	if profile.SSOSession != "" {
		hashSource = profile.SSOSession
	} else if profile.SSOStartURL != "" {
		hashSource = profile.SSOStartURL
	} else {
		return "", fmt.Errorf("profile %q has no SSO session or start URL configured", profile.Name)
	}

	h := sha1.New()
	h.Write([]byte(hashSource))
	hashStr := hex.EncodeToString(h.Sum(nil))

	return filepath.Join(home, ".aws", "sso", "cache", hashStr+".json"), nil
}

func readSSOToken(path string) (*SSOToken, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var token SSOToken
	if err := json.NewDecoder(file).Decode(&token); err != nil {
		return nil, err
	}
	return &token, nil
}

func writeSSOToken(path string, token *SSOToken) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer file.Close()

	encoder := json.NewEncoder(file)
	encoder.SetIndent("", "  ")
	return encoder.Encode(token)
}

func writeAWSProfile(profileName string, newProfile *AWSProfile) error {
	configPath, err := getAWSConfigPath()
	if err != nil {
		return err
	}

	var lines []string
	file, err := os.Open(configPath)
	if err == nil {
		scanner := bufio.NewScanner(file)
		for scanner.Scan() {
			lines = append(lines, scanner.Text())
		}
		file.Close()
	}

	targetHeader := fmt.Sprintf("[profile %s]", profileName)
	if profileName == "default" {
		targetHeader = "[default]"
	}

	profileIndex := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == targetHeader {
			profileIndex = i
			break
		}
	}

	var profileLines []string
	profileLines = append(profileLines, targetHeader)
	if newProfile.SSOSession != "" {
		profileLines = append(profileLines, fmt.Sprintf("sso_session = %s", newProfile.SSOSession))
	}
	if newProfile.SSOStartURL != "" {
		profileLines = append(profileLines, fmt.Sprintf("sso_start_url = %s", newProfile.SSOStartURL))
	}
	if newProfile.SSORegion != "" {
		profileLines = append(profileLines, fmt.Sprintf("sso_region = %s", newProfile.SSORegion))
	}
	if newProfile.SSOAccountID != "" {
		profileLines = append(profileLines, fmt.Sprintf("sso_account_id = %s", newProfile.SSOAccountID))
	}
	if newProfile.SSORoleName != "" {
		profileLines = append(profileLines, fmt.Sprintf("sso_role_name = %s", newProfile.SSORoleName))
	}
	if newProfile.Region != "" {
		profileLines = append(profileLines, fmt.Sprintf("region = %s", newProfile.Region))
	}

	// Fix #8: Quote paths and profile names to handle spaces correctly
	binaryPath, err := os.Executable()
	if err != nil {
		binaryPath = "awssso"
	}
	escapedPath := strings.ReplaceAll(binaryPath, "\\", "\\\\")
	profileLines = append(profileLines, fmt.Sprintf(`credential_process = "%s" credential --profile "%s"`, escapedPath, profileName))

	if profileIndex != -1 {
		nextSectionIndex := len(lines)
		for i := profileIndex + 1; i < len(lines); i++ {
			trimmed := strings.TrimSpace(lines[i])
			if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
				nextSectionIndex = i
				break
			}
		}

		newLines := append([]string{}, lines[:profileIndex]...)
		newLines = append(newLines, profileLines...)
		newLines = append(newLines, lines[nextSectionIndex:]...)
		lines = newLines
	} else {
		if len(lines) > 0 && lines[len(lines)-1] != "" {
			lines = append(lines, "")
		}
		lines = append(lines, profileLines...)
	}

	dir := filepath.Dir(configPath)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

func sessionNameFromURL(startURL string) string {
	name := strings.TrimPrefix(startURL, "https://")
	name = strings.TrimPrefix(name, "http://")
	if idx := strings.Index(name, "."); idx > 0 {
		name = name[:idx]
	}
	return name
}

func writeSSOSession(sessionName string, startURL string, ssoRegion string) error {
	return writeSSOSessionWithEmail(sessionName, startURL, ssoRegion, "")
}

func writeSSOSessionWithEmail(sessionName string, startURL string, ssoRegion string, email string) error {
	configPath, err := getAWSConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			data = []byte{}
		} else {
			return err
		}
	}

	lines := strings.Split(string(data), "\n")

	targetHeader := fmt.Sprintf("[sso-session %s]", sessionName)
	for _, line := range lines {
		if strings.TrimSpace(line) == targetHeader {
			return nil
		}
	}

	sessionLines := []string{
		"",
		targetHeader,
		fmt.Sprintf("sso_start_url = %s", startURL),
		fmt.Sprintf("sso_region = %s", ssoRegion),
	}
	if email != "" {
		sessionLines = append(sessionLines, fmt.Sprintf("sso_account_email = %s", email))
	}

	lines = append(lines, sessionLines...)

	return os.WriteFile(configPath, []byte(strings.Join(lines, "\n")+"\n"), 0600)
}

func migrateSSOTokenCache(oldPath, newPath string) {
	if oldPath == "" || newPath == "" || oldPath == newPath {
		return
	}
	if _, err := os.Stat(oldPath); os.IsNotExist(err) {
		return
	}
	if _, err := os.Stat(newPath); err == nil {
		return
	}
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(newPath), 0700)
	_ = os.WriteFile(newPath, data, 0600)
}

func removeAWSProfile(profileName string) error {
	configPath, err := getAWSConfigPath()
	if err != nil {
		return err
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		return err
	}

	lines := strings.Split(string(data), "\n")

	targetHeader := fmt.Sprintf("[profile %s]", profileName)
	if profileName == "default" {
		targetHeader = "[default]"
	}

	profileStart := -1
	for i, line := range lines {
		if strings.TrimSpace(line) == targetHeader {
			profileStart = i
			break
		}
	}

	if profileStart == -1 {
		return fmt.Errorf("profile %q not found in config", profileName)
	}

	profileEnd := len(lines)
	for i := profileStart + 1; i < len(lines); i++ {
		trimmed := strings.TrimSpace(lines[i])
		if trimmed == "" {
			continue
		}
		if strings.HasPrefix(trimmed, "[") && strings.HasSuffix(trimmed, "]") {
			profileEnd = i
			break
		}
	}

	newLines := []string{}
	for i, line := range lines {
		if i >= profileStart && i < profileEnd {
			continue
		}
		newLines = append(newLines, line)
	}

	content := strings.Join(newLines, "\n") + "\n"
	content = strings.ReplaceAll(content, "\n\n\n", "\n\n")

	return os.WriteFile(configPath, []byte(content), 0600)
}

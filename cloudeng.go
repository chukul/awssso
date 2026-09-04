package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

func detectEnvironment(profile *AWSProfile) string {
	name := strings.ToLower(profile.Name)
	role := strings.ToLower(profile.SSORoleName)

	prodKeywords := []string{"prod", "production", "prd", "pro", "live", "master"}
	for _, keyword := range prodKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "production"
		}
	}

	stagingKeywords := []string{"staging", "stage", "stg", "uat", "preprod", "pre-prod"}
	for _, keyword := range stagingKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "staging"
		}
	}

	oatKeywords := []string{"oat", "e2e", "qa"}
	for _, keyword := range oatKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "oat"
		}
	}

	intKeywords := []string{"int", "integration"}
	for _, keyword := range intKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "integration"
		}
	}

	sandboxKeywords := []string{"sandbox", "sbx"}
	for _, keyword := range sandboxKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "sandbox"
		}
	}

	devKeywords := []string{"dev", "development", "test"}
	for _, keyword := range devKeywords {
		if strings.Contains(name, keyword) || strings.Contains(role, keyword) {
			return "development"
		}
	}

	return "unknown"
}

func getEnvironmentColor(env string) string {
	switch env {
	case "production":
		return Red
	case "staging":
		return Yellow
	case "oat":
		return Yellow
	case "integration":
		return Magenta
	case "sandbox":
		return White
	case "development":
		return Green
	default:
		return Cyan
	}
}

func getEnvironmentSymbol(env string) string {
	switch env {
	case "production":
		return "🔴"
	case "staging":
		return "🟡"
	case "oat":
		return "🟡"
	case "integration":
		return "🟣"
	case "sandbox":
		return "⚪"
	case "development":
		return "🟢"
	default:
		return "⚪"
	}
}

func showProductionWarning(profile *AWSProfile) bool {
	env := detectEnvironment(profile)
	if env != "production" {
		return true
	}

	fmt.Println()
	fmt.Printf("  %s🔴 Production%s  %s%s%s  (%s / %s)\n",
		Red, Reset,
		Bold, profile.Name, Reset,
		profile.SSOAccountID, profile.SSORoleName)
	fmt.Println()
	printPrompt("Continue? (y/N): ")

	var response string
	fmt.Scanln(&response)

	response = strings.ToLower(strings.TrimSpace(response))
	if response != "yes" && response != "y" {
		printInfo("Canceled")
		return false
	}
	return true
}

// RecentProfile tracks a recently used AWS profile for quick switching.
type RecentProfile struct {
	Name      string    `json:"name"`
	AccountID string    `json:"account_id"`
	RoleName  string    `json:"role_name"`
	Timestamp time.Time `json:"timestamp"`
}

type RecentProfiles struct {
	Profiles []RecentProfile `json:"profiles"`
}

func getActiveProfilePath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "sso", "active_profile"), nil
}


// writeActiveProfile persists the selected profile name so the REPL shell
// can read it back and update its own environment after a subprocess exits.
func writeActiveProfile(name string) {
	path, err := getActiveProfilePath()
	if err != nil {
		return
	}
	_ = os.MkdirAll(filepath.Dir(path), 0700)
	_ = os.WriteFile(path, []byte(name), 0600)
}

// readActiveProfile returns the last profile written by writeActiveProfile,
// or empty string if none.
func readActiveProfile() string {
	path, err := getActiveProfilePath()
	if err != nil {
		return ""
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(data))
}

func getRecentProfilesPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "sso", "recent_profiles.json"), nil
}

func recordRecentProfile(profile *AWSProfile) error {
	path, err := getRecentProfilesPath()
	if err != nil {
		return err
	}

	var recent RecentProfiles
	data, err := os.ReadFile(path)
	if err == nil {
		_ = json.Unmarshal(data, &recent)
	}

	filtered := []RecentProfile{}
	for _, p := range recent.Profiles {
		if p.Name != profile.Name {
			filtered = append(filtered, p)
		}
	}

	newEntry := RecentProfile{
		Name:      profile.Name,
		AccountID: profile.SSOAccountID,
		RoleName:  profile.SSORoleName,
		Timestamp: time.Now(),
	}
	recent.Profiles = append([]RecentProfile{newEntry}, filtered...)

	if len(recent.Profiles) > 10 {
		recent.Profiles = recent.Profiles[:10]
	}

	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0700); err != nil {
		return err
	}

	data, err = json.MarshalIndent(recent, "", "  ")
	if err != nil {
		return err
	}

	return os.WriteFile(path, data, 0600)
}

func getRecentProfiles() ([]RecentProfile, error) {
	path, err := getRecentProfilesPath()
	if err != nil {
		return nil, err
	}

	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return []RecentProfile{}, nil
		}
		return nil, err
	}

	var recent RecentProfiles
	if err := json.Unmarshal(data, &recent); err != nil {
		return nil, err
	}

	return recent.Profiles, nil
}

func formatTimeSince(t time.Time) string {
	duration := time.Since(t)

	switch {
	case duration < time.Minute:
		return "just now"
	case duration < time.Hour:
		mins := int(duration.Minutes())
		return fmt.Sprintf("%d min ago", mins)
	case duration < 24*time.Hour:
		hours := int(duration.Hours())
		return fmt.Sprintf("%d hr ago", hours)
	default:
		days := int(duration.Hours() / 24)
		if days == 1 {
			return "yesterday"
		}
		return fmt.Sprintf("%d days ago", days)
	}
}

// ExportFormat defines supported credential export formats.
type ExportFormat string

const (
	FormatEnv               ExportFormat = "env"
	FormatTerraform         ExportFormat = "terraform"
	FormatDocker            ExportFormat = "docker"
	FormatJSON              ExportFormat = "json"
	FormatYAML              ExportFormat = "yaml"
	FormatKYAML             ExportFormat = "kyaml"
	FormatCredentialProcess ExportFormat = "credential_process"
	FormatProfile           ExportFormat = "profile"
)

func exportCredentials(creds *CredentialResponse, format ExportFormat) string {
	switch format {
	case FormatEnv:
		if runtime.GOOS == "windows" {
			return fmt.Sprintf(`$env:AWS_ACCESS_KEY_ID = "%s"
$env:AWS_SECRET_ACCESS_KEY = "%s"
$env:AWS_SESSION_TOKEN = "%s"
# Expires: %s`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)
		}
		return fmt.Sprintf(`export AWS_ACCESS_KEY_ID="%s"
export AWS_SECRET_ACCESS_KEY="%s"
export AWS_SESSION_TOKEN="%s"
# Expires: %s`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)

	case FormatTerraform:
		return fmt.Sprintf(`# Add to terraform.tfvars or use -var flag
aws_access_key    = "%s"
aws_secret_key    = "%s"
aws_session_token = "%s"
# Expires: %s`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)

	case FormatDocker:
		if runtime.GOOS == "windows" {
			// PowerShell uses backtick ` for line continuation
			return fmt.Sprintf("docker run `\n  -e AWS_ACCESS_KEY_ID=\"%s\" `\n  -e AWS_SECRET_ACCESS_KEY=\"%s\" `\n  -e AWS_SESSION_TOKEN=\"%s\" `\n  your-image:tag\n# Expires: %s",
				creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)
		}
		return fmt.Sprintf(`docker run \
  -e AWS_ACCESS_KEY_ID="%s" \
  -e AWS_SECRET_ACCESS_KEY="%s" \
  -e AWS_SESSION_TOKEN="%s" \
  your-image:tag
# Expires: %s`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)

	case FormatJSON:
		output, _ := json.MarshalIndent(creds, "", "  ")
		return string(output)

	case FormatCredentialProcess:
		output, _ := json.Marshal(creds)
		return string(output)

	case FormatYAML:
		return fmt.Sprintf(`aws_access_key_id: "%s"
aws_secret_access_key: "%s"
aws_session_token: "%s"
expiration: "%s"`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)

	case FormatKYAML:
		return fmt.Sprintf(`apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: "%s"
  AWS_SECRET_ACCESS_KEY: "%s"
  AWS_SESSION_TOKEN: "%s"
  # Expires: %s`, creds.AccessKeyId, creds.SecretAccessKey, creds.SessionToken, creds.Expiration)

	default:
		return ""
	}
}

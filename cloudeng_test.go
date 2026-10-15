package main

import (
	"strings"
	"testing"
)

func TestDetectEnvironment(t *testing.T) {
	tests := []struct {
		name     string
		role     string
		expected string
	}{
		{"my-prod-account", "AdminRole", "production"},
		{"live-service", "ReadOnly", "production"},
		{"staging-api", "DevRole", "staging"},
		{"uat-testing", "Tester", "staging"},
		{"oat-verification", "QARole", "oat"},
		{"e2e-tests", "TestRunner", "oat"},
		{"int-account", "DevRole", "integration"},
		{"integration-env", "Tester", "integration"},
		{"dev-sandbox", "AdminRole", "sandbox"},
		{"my-sandbox", "AdminRole", "sandbox"},
		{"sbx-account", "DevRole", "sandbox"},
		{"my-test-env", "Developer", "development"},
		{"dev-account", "AdminRole", "development"},
		{"random-account", "SomeRole", "unknown"},

		// real profile naming: accor-<service>-<env>
		{"accor-acp-container-orch-dev", "", "development"},
		{"accor-acp-container-orch-oat", "", "oat"},
		{"accor-acp-container-orch-pro", "", "production"},
		{"accor-acp-coreapihub-nopro", "", "nonprod"},
		{"accor-acp-coreapihub-pro", "", "production"},
		{"accor-softfactory-int", "", "integration"},
		{"accor-master-pro", "", "production"},
		{"accor-soa-oat", "", "oat"},

		// keywords inside longer words must not match ("delivery" contains "live")
		{"accor-sftpdelivery-oat", "", "oat"},
		{"accor-sftpdelivery-dev", "", "development"},
		{"accor-sftpdelivery1", "", "unknown"},
		{"printer-float-internal", "", "unknown"},
		{"innopro-svc", "", "unknown"},

		// non-production variants
		{"x-non-prod", "", "nonprod"},
		{"x_nonprod_y", "", "nonprod"},
		{"x-nopro2", "", "nonprod"},
		{"x-NoPro", "", "nonprod"},

		// pre-prod is staging, not production
		{"x-pre-prod", "", "staging"},
		{"x-preprod", "", "staging"},
		{"x-pre_production-1", "", "staging"},

		// trailing digits, role names, precedence
		{"x-dev1", "", "development"},
		{"x-oat2", "", "oat"},
		{"x", "DevRole", "development"},
		{"x-dev", "ProvisioningRole", "development"},
		{"x-dev", "AWSAdministratorAccess", "development"},
		{"dev-int", "", "integration"},
	}

	for _, tt := range tests {
		profile := &AWSProfile{
			Name:        tt.name,
			SSORoleName: tt.role,
		}
		result := detectEnvironment(profile)
		if result != tt.expected {
			t.Errorf("detectEnvironment(name=%q, role=%q) = %q, want %q",
				tt.name, tt.role, result, tt.expected)
		}
	}
}

func TestDetectEnvironmentOverride(t *testing.T) {
	tests := []struct {
		name     string
		override string
		expected string
	}{
		{"accor-sftpdelivery1", "production", "production"},
		{"accor-sftpdelivery-dev", "production", "production"},
		{"accor-foo-pro", "development", "development"},
		{"accor-foo-pro", "nonprod", "nonprod"},
		{"accor-foo-pro", "bogus", "production"},
		{"accor-foo-dev", "", "development"},
	}

	for _, tt := range tests {
		profile := &AWSProfile{Name: tt.name, Environment: tt.override}
		if got := detectEnvironment(profile); got != tt.expected {
			t.Errorf("detectEnvironment(name=%q, override=%q) = %q, want %q",
				tt.name, tt.override, got, tt.expected)
		}
	}
}

func TestProfileOrMock(t *testing.T) {
	config := &AWSConfig{Profiles: map[string]*AWSProfile{
		"accor-sftpdelivery1": {Name: "accor-sftpdelivery1", Environment: "production"},
	}}

	if got := detectEnvironment(profileOrMock("accor-sftpdelivery1", config)); got != "production" {
		t.Errorf("configured profile with override = %q, want production", got)
	}
	if got := detectEnvironment(profileOrMock("accor-foo-oat", config)); got != "oat" {
		t.Errorf("unconfigured profile falls back to name detection, got %q, want oat", got)
	}
	if got := detectEnvironment(profileOrMock("accor-foo-oat", nil)); got != "oat" {
		t.Errorf("nil config falls back to name detection, got %q, want oat", got)
	}
}

func TestGetEnvironmentColor(t *testing.T) {
	if getEnvironmentColor("production") != Red {
		t.Error("production should be Red")
	}
	if getEnvironmentColor("nonprod") != Blue {
		t.Error("nonprod should be Blue")
	}
	if getEnvironmentSymbol("nonprod") != "🔵" {
		t.Error("nonprod should be 🔵")
	}
	if getEnvironmentColor("staging") != Yellow {
		t.Error("staging should be Yellow")
	}
	if getEnvironmentColor("oat") != Yellow {
		t.Error("oat should be Yellow")
	}
	if getEnvironmentColor("integration") != Magenta {
		t.Error("integration should be Magenta")
	}
	if getEnvironmentColor("sandbox") != White {
		t.Error("sandbox should be White")
	}
	if getEnvironmentColor("development") != Green {
		t.Error("development should be Green")
	}
	if getEnvironmentColor("unknown") != Cyan {
		t.Error("unknown should be Cyan")
	}
}

func TestExportCredentials(t *testing.T) {
	creds := &CredentialResponse{
		Version:         1,
		AccessKeyId:     "AKIAIOSFODNN7EXAMPLE",
		SecretAccessKey: "wJalrXUtnFEMI/K7MDENG/bPxRfiCYEXAMPLEKEY",
		SessionToken:    "FwoGZXIvYXdzEBAaDH...",
		Expiration:      "2026-06-18T18:32:35Z",
	}

	// env format
	envOutput := exportCredentials(creds, FormatEnv)
	if !strings.Contains(envOutput, "export AWS_ACCESS_KEY_ID=") {
		t.Error("env format should contain export statement")
	}
	if !strings.Contains(envOutput, creds.AccessKeyId) {
		t.Error("env format should contain access key")
	}

	// json format
	jsonOutput := exportCredentials(creds, FormatJSON)
	if !strings.Contains(jsonOutput, `"AccessKeyId"`) {
		t.Error("json format should contain AccessKeyId field")
	}

	// yaml format
	yamlOutput := exportCredentials(creds, FormatYAML)
	if !strings.Contains(yamlOutput, "aws_access_key_id:") {
		t.Error("yaml format should contain aws_access_key_id key")
	}

	// credential_process format (compact JSON)
	cpOutput := exportCredentials(creds, FormatCredentialProcess)
	if strings.Contains(cpOutput, "\n") {
		t.Error("credential_process format should be single line")
	}

	// unknown format
	unknownOutput := exportCredentials(creds, "bad")
	if unknownOutput != "" {
		t.Error("unknown format should return empty string")
	}
}

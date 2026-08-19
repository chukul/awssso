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

func TestGetEnvironmentColor(t *testing.T) {
	if getEnvironmentColor("production") != Red {
		t.Error("production should be Red")
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

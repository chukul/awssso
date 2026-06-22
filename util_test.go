package main

import (
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso/types"
)

func TestFormatDuration(t *testing.T) {
	tests := []struct {
		input    time.Duration
		expected string
	}{
		{5 * time.Minute, "5m"},
		{90 * time.Minute, "1h 30m"},
		{25 * time.Hour, "1d 1h 0m"},
		{0, "0m"},
	}

	for _, tt := range tests {
		result := formatDuration(tt.input)
		if result != tt.expected {
			t.Errorf("formatDuration(%v) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestLevenshteinDistance(t *testing.T) {
	tests := []struct {
		s1, s2   string
		expected int
	}{
		{"", "", 0},
		{"abc", "abc", 0},
		{"abc", "ABC", 0}, // case-insensitive
		{"kitten", "sitting", 3},
		{"", "hello", 5},
		{"hello", "", 5},
	}

	for _, tt := range tests {
		result := levenshteinDistance(tt.s1, tt.s2)
		if result != tt.expected {
			t.Errorf("levenshteinDistance(%q, %q) = %d, want %d", tt.s1, tt.s2, result, tt.expected)
		}
	}
}

func strPtr(s string) *string { return &s }

func TestFilterAccounts(t *testing.T) {
	accounts := []types.AccountInfo{
		{AccountName: strPtr("Production"), AccountId: strPtr("111111111111")},
		{AccountName: strPtr("Development"), AccountId: strPtr("222222222222")},
		{AccountName: strPtr("Staging"), AccountId: strPtr("333333333333")},
	}

	// Empty search returns all
	result := filterAccounts(accounts, "")
	if len(result) != 3 {
		t.Errorf("Expected 3, got %d", len(result))
	}

	// Search by name (case-insensitive)
	result = filterAccounts(accounts, "prod")
	if len(result) != 1 || *result[0].AccountName != "Production" {
		t.Errorf("Expected Production, got %v", result)
	}

	// Search by ID
	result = filterAccounts(accounts, "222222222222")
	if len(result) != 1 || *result[0].AccountName != "Development" {
		t.Errorf("Expected Development, got %v", result)
	}

	// No match
	result = filterAccounts(accounts, "nonexistent")
	if len(result) != 0 {
		t.Errorf("Expected 0, got %d", len(result))
	}
}

func TestSuggestProfiles(t *testing.T) {
	config := &AWSConfig{
		Profiles: map[string]*AWSProfile{
			"my-dev-profile":  {},
			"my-prod-profile": {},
			"staging":         {},
			"unrelated-name":  {},
		},
		Sessions: map[string]*SSOSession{},
	}

	suggestions := suggestProfiles("my-dev-proile", config, 3)
	if len(suggestions) == 0 {
		t.Fatal("Expected at least one suggestion")
	}
	// "my-dev-profile" should be the closest match (1 edit away)
	if suggestions[0] != "my-dev-profile" {
		t.Errorf("Expected my-dev-profile as top suggestion, got %q", suggestions[0])
	}
}

func TestShortName(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"https://d-123456789.awsapps.com/start/", "d-123456789"},
		{"my-session", "my-session"},
		{"https://my-org.awsapps.com/start", "my-org"},
	}

	for _, tt := range tests {
		result := shortName(tt.input)
		if result != tt.expected {
			t.Errorf("shortName(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestParseSelection(t *testing.T) {
	tests := []struct {
		input    string
		max      int
		expected []int
	}{
		{"1", 5, []int{0}},
		{"1,3,5", 5, []int{0, 2, 4}},
		{"1-3", 5, []int{0, 1, 2}},
		{"1-3,5", 5, []int{0, 1, 2, 4}},
		{"3-1", 5, []int{0, 1, 2}}, // reversed range
		{"2,2,2", 5, []int{1}},     // duplicates removed
		{"0", 5, nil},              // out of range
		{"6", 5, nil},              // out of range
		{"abc", 5, nil},            // invalid
		{"", 5, []int{}},           // empty returns empty
	}

	for _, tt := range tests {
		result := parseSelection(tt.input, tt.max)
		if tt.expected == nil {
			if result != nil {
				t.Errorf("parseSelection(%q, %d) = %v, want nil", tt.input, tt.max, result)
			}
			continue
		}
		if len(result) != len(tt.expected) {
			t.Errorf("parseSelection(%q, %d) = %v, want %v", tt.input, tt.max, result, tt.expected)
			continue
		}
		for i := range result {
			if result[i] != tt.expected[i] {
				t.Errorf("parseSelection(%q, %d) = %v, want %v", tt.input, tt.max, result, tt.expected)
				break
			}
		}
	}
}

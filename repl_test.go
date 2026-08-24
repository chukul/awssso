package main

import "testing"

func TestParseArgs(t *testing.T) {
	tests := []struct {
		input string
		want  []string
	}{
		{"login", []string{"login"}},
		{"login --profile my-profile", []string{"login", "--profile", "my-profile"}},
		{`login --profile "my profile"`, []string{"login", "--profile", "my profile"}},
		{`login --profile 'my profile'`, []string{"login", "--profile", "my profile"}},
		{"export --format env", []string{"export", "--format", "env"}},
		{"", nil},
		{"  ", nil},
	}

	for _, tt := range tests {
		got := parseArgs(tt.input)
		if len(got) != len(tt.want) {
			t.Errorf("parseArgs(%q): got %v, want %v", tt.input, got, tt.want)
			continue
		}
		for i, w := range tt.want {
			if got[i] != w {
				t.Errorf("parseArgs(%q)[%d]: got %q, want %q", tt.input, i, got[i], w)
			}
		}
	}
}

func TestIsKnownCommand(t *testing.T) {
	known := []string{"login", "switch", "export", "profiles", "doctor", "group", "pin", "unpin"}
	for _, cmd := range known {
		if !isKnownCommand(cmd) {
			t.Errorf("expected %q to be a known command", cmd)
		}
	}
	unknown := []string{"1", "2", "foo", "aws", "terraform"}
	for _, cmd := range unknown {
		if isKnownCommand(cmd) {
			t.Errorf("expected %q to NOT be a known command", cmd)
		}
	}
}

func TestCompleterFirstWord(t *testing.T) {
	c := &replCompleter{}

	// Empty → all commands offered
	results, length := c.Do([]rune(""), 0)
	if len(results) == 0 {
		t.Error("expected completions for empty input")
	}
	if length != 0 {
		t.Errorf("expected length 0 for empty input, got %d", length)
	}

	// Partial command
	results, length = c.Do([]rune("lo"), 2)
	if len(results) == 0 {
		t.Error("expected completions for 'lo'")
	}
	if length != 2 {
		t.Errorf("expected length 2, got %d", length)
	}
}

func TestCompleterFormatValues(t *testing.T) {
	c := &replCompleter{}

	// After "--format " the format values should be offered
	line := []rune("export --format ")
	results, _ := c.Do(line, len(line))
	if len(results) == 0 {
		t.Error("expected format completions after '--format '")
	}
}

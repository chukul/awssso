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
	known := []string{"login", "create", "export", "profiles", "doctor", "group", "shell"}
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

func TestCompleterShellProfile(t *testing.T) {
	c := &replCompleter{}

	// After "shell --profile " the profile names should be offered
	line := []rune("shell --profile ")
	results, _ := c.Do(line, len(line))
	if len(results) == 0 {
		t.Error("expected profile completions after 'shell --profile '")
	}
}

// TestTabCompleteMenuTrigger verifies the classification the interactive Tab
// menu relies on: multi-match prefixes yield >=2 full-word completions (menu
// path) while a unique prefix yields exactly one (auto-complete path). It also
// checks that selecting an option reconstructs the line as base + choice.
func TestTabCompleteMenuTrigger(t *testing.T) {
	// Multiple matches → menu path. "c" matches console + create.
	m := tabComplete("c")
	if len(m.completions) < 2 {
		t.Fatalf("expected >=2 completions for 'c' (menu path), got %v", m.completions)
	}
	if m.base != "" || m.word != "c" {
		t.Errorf("base/word wrong: base=%q word=%q", m.base, m.word)
	}
	// Selection reconstruction: choosing "create" rebuilds to base+choice.
	if got := m.base + "create"; got != "create" {
		t.Errorf("selection reconstruction wrong: got %q, want %q", got, "create")
	}

	// Mid-line value completion preserves the base and reconstructs correctly.
	f := tabComplete("export --format ")
	if len(f.completions) < 2 {
		t.Fatalf("expected multiple format completions, got %v", f.completions)
	}
	if f.base != "export --format " {
		t.Errorf("base not preserved for mid-line completion: %q", f.base)
	}
	if line := f.base + "json"; line != "export --format json" {
		t.Errorf("format selection reconstruction wrong: %q", line)
	}

	// Unique prefix → single completion (auto-complete path, no menu).
	w := tabComplete("wh")
	if len(w.completions) != 1 || w.completions[0] != "whoami" {
		t.Errorf("expected single completion 'whoami', got %v", w.completions)
	}
}

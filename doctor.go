package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

func runDoctor() {
	printHeader("AWSSSO HEALTH CHECK")

	passed, warned, failed := 0, 0, 0

	check := func(ok bool, warn bool, msg string) {
		if ok {
			fmt.Printf("  %s✔%s %s\n", Green, Reset, msg)
			passed++
		} else if warn {
			fmt.Printf("  %s⚠%s %s\n", Yellow, Reset, msg)
			warned++
		} else {
			fmt.Printf("  %s✘%s %s\n", Red, Reset, msg)
			failed++
		}
	}

	// ── Binary ────────────────────────────────────────────────────────────────
	fmt.Printf("\n%sBinary%s\n", Bold, Reset)

	binaryPath, err := os.Executable()
	if err == nil {
		check(true, false, fmt.Sprintf("Binary found: %s", binaryPath))
	} else {
		check(false, false, "Cannot determine binary path")
	}

	if binaryPath != "" {
		if _, lookErr := exec.LookPath(filepath.Base(binaryPath)); lookErr == nil {
			check(true, false, "Binary is on PATH")
		} else {
			check(false, true, fmt.Sprintf("Binary not on PATH — add %s to your PATH", filepath.Dir(binaryPath)))
		}
	}

	// ── Config file ───────────────────────────────────────────────────────────
	fmt.Printf("\n%sAWS Config%s\n", Bold, Reset)

	configPath, _ := getAWSConfigPath()
	if _, statErr := os.Stat(configPath); statErr == nil {
		check(true, false, fmt.Sprintf("Config file found: %s", configPath))
	} else {
		check(false, false, fmt.Sprintf("Config file missing: %s", configPath))
	}

	config, loadErr := loadAWSConfig()
	if loadErr != nil {
		check(false, false, fmt.Sprintf("Config parse error: %v", loadErr))
		printSummary(passed, warned, failed)
		return
	}

	check(true, false, fmt.Sprintf("%d profile(s) configured", len(config.Profiles)))
	check(true, false, fmt.Sprintf("%d SSO session(s) configured", len(config.Sessions)))

	// ── Profile integrity ─────────────────────────────────────────────────────
	fmt.Printf("\n%sProfile Integrity%s\n", Bold, Reset)

	orphaned := []string{}
	incomplete := []string{}
	for name, p := range config.Profiles {
		if p.SSOSession != "" {
			if _, found := config.Sessions[p.SSOSession]; !found {
				orphaned = append(orphaned, name)
			}
		}
		if p.SSOAccountID == "" || p.SSORoleName == "" {
			if p.SSOSession != "" || p.SSOStartURL != "" {
				incomplete = append(incomplete, name)
			}
		}
	}

	if len(orphaned) == 0 {
		check(true, false, "All profiles reference valid SSO sessions")
	} else {
		check(false, false, fmt.Sprintf("%d profile(s) reference missing sessions: %s",
			len(orphaned), strings.Join(orphaned, ", ")))
	}

	if len(incomplete) == 0 {
		check(true, false, "All profiles have account and role configured")
	} else {
		check(false, true, fmt.Sprintf("%d profile(s) missing account/role: %s",
			len(incomplete), strings.Join(incomplete, ", ")))
	}

	// ── SSO token status ──────────────────────────────────────────────────────
	fmt.Printf("\n%sSSO Token Status%s\n", Bold, Reset)

	if len(config.Sessions) == 0 {
		check(false, true, "No SSO sessions configured — run 'awssso login' to set up")
	}

	for name := range config.Sessions {
		mock := &AWSProfile{SSOSession: name}
		tokenPath, pathErr := getSSOTokenPath(mock, config)
		if pathErr != nil {
			check(false, false, fmt.Sprintf("Session %-30s  cannot resolve token path", name))
			continue
		}

		token, readErr := readSSOToken(tokenPath)
		if readErr != nil {
			check(false, false, fmt.Sprintf("Session %-30s  %sNot logged in%s", name, Red, Reset))
			continue
		}

		if token.IsExpired() {
			exp, _ := time.Parse(time.RFC3339, token.ExpiresAt)
			ago := formatDuration(time.Since(exp))
			if token.RefreshToken != "" {
				check(false, true, fmt.Sprintf("Session %-30s  %sExpired%s %s ago (has refresh token)", name, Yellow, Reset, ago))
			} else {
				check(false, false, fmt.Sprintf("Session %-30s  %sExpired%s %s ago — run: awssso login --session %s", name, Red, Reset, ago, name))
			}
		} else {
			exp, _ := time.Parse(time.RFC3339, token.ExpiresAt)
			remaining := formatDuration(time.Until(exp))
			check(true, false, fmt.Sprintf("Session %-30s  %sValid%s (%s remaining)", name, Green, Reset, remaining))
		}
	}

	// ── Token cache directory ─────────────────────────────────────────────────
	fmt.Printf("\n%sToken Cache%s\n", Bold, Reset)

	home, _ := homeDir()
	cacheDir := filepath.Join(home, ".aws", "sso", "cache")
	if entries, readErr := os.ReadDir(cacheDir); readErr == nil {
		check(true, false, fmt.Sprintf("Cache directory found (%d file(s)): %s", len(entries), cacheDir))
	} else {
		check(false, true, fmt.Sprintf("Cache directory missing or unreadable: %s", cacheDir))
	}

	// ── Summary ───────────────────────────────────────────────────────────────
	printSummary(passed, warned, failed)
}

func printSummary(passed, warned, failed int) {
	fmt.Println()
	fmt.Printf("  %s✔ %d passed%s  %s⚠ %d warning(s)%s  %s✘ %d failed%s\n\n",
		Green, passed, Reset,
		Yellow, warned, Reset,
		Red, failed, Reset,
	)
	if failed > 0 {
		printError("Health check found issues — fix the items marked ✘ above.")
	} else if warned > 0 {
		printWarning("Health check passed with warnings.")
	} else {
		printSuccess("All checks passed!")
	}
}

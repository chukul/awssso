package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// runPrompt outputs a compact, coloured profile badge for embedding in a shell prompt.
// Run with --install to automatically patch your shell config — no manual editing needed.
func runPrompt(install bool) {
	if install {
		installPrompt()
		return
	}

	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		return // nothing when no profile is active
	}

	mock := &AWSProfile{Name: profile}
	env := detectEnvironment(mock)
	symbol := getEnvironmentSymbol(env)
	color := getEnvironmentColor(env)

	// Append token expiry warning when < 15 min remain
	suffix := ""
	if config, err := loadAWSConfig(); err == nil {
		if p, ok := config.Profiles[profile]; ok {
			if tokenPath, pathErr := getSSOTokenPath(p, config); pathErr == nil {
				if token, readErr := readSSOToken(tokenPath); readErr == nil {
					if token.IsExpired() {
						suffix = " ⚠"
					} else if expiry, parseErr := time.Parse(time.RFC3339, token.ExpiresAt); parseErr == nil {
						if remaining := time.Until(expiry); remaining < 15*time.Minute {
							suffix = fmt.Sprintf(" ~%dm", int(remaining.Minutes())+1)
						}
					}
				}
			}
		}
	}

	fmt.Printf("%s[%s %s%s]%s", color, symbol, profile, suffix, Reset)
}

// installPrompt detects the current shell and appends the prompt integration
// snippet to the appropriate config file — no manual editing required.
func installPrompt() {
	shell := detectShell()
	if shell == "" {
		printError("Could not detect shell. Set --shell manually: awssso prompt --install --shell zsh|bash|fish|powershell")
		os.Exit(1)
	}
	printInfo(fmt.Sprintf("Detected shell: %s", shell))

	switch shell {
	case "zsh":
		installPromptZsh()
	case "bash":
		installPromptBash()
	case "fish":
		installPromptFish()
	case "powershell":
		installPromptPowerShell()
	default:
		printError(fmt.Sprintf("Shell %q not supported for auto-install. Add the snippet manually.", shell))
		os.Exit(1)
	}
}

func installPromptZsh() {
	home, _ := homeDir()
	rc := filepath.Join(home, ".zshrc")

	snippet := "\n# awssso prompt integration\nPROMPT='$(awssso prompt) '$PROMPT\n"
	if alreadyInstalled(rc, "awssso prompt") {
		printInfo("awssso prompt is already in ~/.zshrc")
		return
	}
	if err := appendToFile(rc, snippet); err != nil {
		printError(fmt.Sprintf("Failed to update ~/.zshrc: %v", err))
		printInfo("Add this line manually to ~/.zshrc:")
		fmt.Printf("  %sPROMPT='$(awssso prompt) '$PROMPT%s\n", Dim, Reset)
		return
	}
	printSuccess("Prompt integration added to ~/.zshrc")
	printInfo("Restart your terminal or run:")
	fmt.Printf("  %ssource ~/.zshrc%s\n", Dim, Reset)
}

func installPromptBash() {
	home, _ := homeDir()
	rc := filepath.Join(home, ".bashrc")

	// On macOS bash reads ~/.bash_profile for login shells; check both
	if runtime.GOOS == "darwin" {
		if _, err := os.Stat(filepath.Join(home, ".bash_profile")); err == nil {
			rc = filepath.Join(home, ".bash_profile")
		}
	}

	snippet := "\n# awssso prompt integration\nPS1='\\[$(awssso prompt)\\]'$PS1\n"
	if alreadyInstalled(rc, "awssso prompt") {
		printInfo(fmt.Sprintf("awssso prompt is already in %s", rc))
		return
	}
	if err := appendToFile(rc, snippet); err != nil {
		printError(fmt.Sprintf("Failed to update %s: %v", rc, err))
		printInfo("Add this line manually:")
		fmt.Printf("  %sPS1='\\[$(awssso prompt)\\]'$PS1%s\n", Dim, Reset)
		return
	}
	printSuccess(fmt.Sprintf("Prompt integration added to %s", rc))
	printInfo("Restart your terminal or run:")
	fmt.Printf("  %ssource %s%s\n", Dim, rc, Reset)
}

func installPromptFish() {
	home, _ := homeDir()
	funcDir := filepath.Join(home, ".config", "fish", "functions")
	funcFile := filepath.Join(funcDir, "fish_prompt.fish")

	if err := os.MkdirAll(funcDir, 0755); err != nil {
		printError(fmt.Sprintf("Cannot create fish functions directory: %v", err))
		os.Exit(1)
	}

	// If a fish_prompt already exists, prepend to it rather than overwriting
	if _, err := os.Stat(funcFile); err == nil {
		if alreadyInstalled(funcFile, "awssso prompt") {
			printInfo("awssso prompt is already in fish_prompt.fish")
			return
		}
		// Read existing, inject awssso badge after the function header
		data, _ := os.ReadFile(funcFile)
		existing := string(data)
		injected := strings.Replace(existing,
			"function fish_prompt",
			"function fish_prompt\n    printf '%s ' (awssso prompt)",
			1)
		if err := os.WriteFile(funcFile, []byte(injected), 0644); err != nil {
			printError(fmt.Sprintf("Failed to update fish_prompt.fish: %v", err))
			return
		}
	} else {
		// No existing fish_prompt — write a minimal one
		content := `function fish_prompt
    printf '%s ' (awssso prompt)
    printf '%s> ' (prompt_pwd)
end
`
		if err := os.WriteFile(funcFile, []byte(content), 0644); err != nil {
			printError(fmt.Sprintf("Failed to write fish_prompt.fish: %v", err))
			return
		}
	}

	printSuccess(fmt.Sprintf("Prompt integration written to %s", funcFile))
	printInfo("Fish loads functions automatically — open a new terminal to see the badge.")
}

func installPromptPowerShell() {
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "$PROFILE").Output()
	if err != nil {
		if _, err2 := exec.LookPath("pwsh"); err2 == nil {
			out, err = exec.Command("pwsh", "-NoProfile", "-Command", "$PROFILE").Output()
		}
	}
	if err != nil {
		printError("Could not locate PowerShell profile path.")
		printInfo("Add this to your $PROFILE manually:")
		fmt.Printf("  %sfunction prompt { \"$(awssso prompt) PS $($executionContext.SessionState.Path.CurrentLocation)> \" }%s\n", Dim, Reset)
		return
	}

	profilePath := strings.TrimSpace(string(out))
	snippet := "\n# awssso prompt integration\nfunction prompt { \"$(awssso prompt) PS $($executionContext.SessionState.Path.CurrentLocation)> \" }\n"

	if alreadyInstalled(profilePath, "awssso prompt") {
		printInfo(fmt.Sprintf("awssso prompt is already in %s", profilePath))
		return
	}
	if err := os.MkdirAll(filepath.Dir(profilePath), 0755); err != nil {
		printError(fmt.Sprintf("Cannot create profile directory: %v", err))
		return
	}
	if err := appendToFile(profilePath, snippet); err != nil {
		printError(fmt.Sprintf("Failed to update PowerShell profile: %v", err))
		return
	}
	printSuccess(fmt.Sprintf("Prompt integration added to %s", profilePath))
	printInfo("Restart PowerShell or run:")
	fmt.Printf("  %s. \"%s\"%s\n", Dim, profilePath, Reset)
}

// alreadyInstalled returns true if the file already contains the marker string.
func alreadyInstalled(file, marker string) bool {
	data, err := os.ReadFile(file)
	if err != nil {
		return false
	}
	return strings.Contains(string(data), marker)
}

// appendToFile appends text to a file, creating it if it doesn't exist.
func appendToFile(path, text string) error {
	f, err := os.OpenFile(path, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	defer f.Close()
	_, err = f.WriteString(text)
	return err
}

package main

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// autoDetectFormat inspects the current working directory and returns the most
// appropriate export format. Falls back to FormatEnv when nothing is detected.
func autoDetectFormat() ExportFormat {
	entries, err := os.ReadDir(".")
	if err != nil {
		return FormatEnv
	}
	for _, e := range entries {
		name := strings.ToLower(e.Name())
		switch {
		case name == "terraform.tfvars" || strings.HasSuffix(name, ".tf"):
			return FormatTerraform
		case name == "dockerfile" || name == "docker-compose.yml" || name == "docker-compose.yaml":
			return FormatDocker
		}
	}
	return FormatEnv
}

func runCopy(profileName string, format string) {
	// Auto-detect format from working directory when not explicitly specified
	if strings.ToLower(format) == "env" {
		detected := autoDetectFormat()
		if detected != FormatEnv {
			format = string(detected)
			printInfo(fmt.Sprintf("Auto-detected format: %s", format))
		}
	}

	var exportFormat ExportFormat
	switch strings.ToLower(format) {
	case "env", "environment":
		exportFormat = FormatEnv
	case "terraform", "tf":
		exportFormat = FormatTerraform
	case "docker":
		exportFormat = FormatDocker
	case "json":
		exportFormat = FormatJSON
	case "yaml", "yml":
		exportFormat = FormatYAML
	case "kyaml", "kubernetes":
		exportFormat = FormatKYAML
	case "credential_process", "credential":
		exportFormat = FormatCredentialProcess
	default:
		printError(fmt.Sprintf("Unknown format %q. Supported: env, terraform, docker, json, yaml, kyaml, credential_process", format))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	if profileName == "" {
		profileName = pickProfileForConsole(config)
		if profileName == "" {
			return
		}
	}

	creds, profile, err := resolveCredentials(ctx, profileName, config)
	if err != nil {
		os.Exit(1)
	}

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	_ = recordRecentProfile(profile)

	output := exportCredentials(creds, exportFormat)

	if err := writeToClipboard(output); err != nil {
		printError(fmt.Sprintf("Failed to copy to clipboard: %v", err))
		printInfo("Printing to stdout instead:")
		fmt.Println(output)
		os.Exit(1)
	}

	env := detectEnvironment(profile)
	envSymbol := getEnvironmentSymbol(env)
	printSuccess(fmt.Sprintf("Credentials copied to clipboard! %s %s / %s", envSymbol, profileName, format))
	printInfo(fmt.Sprintf("Expires: %s", creds.Expiration))
}

// writeToClipboard writes text to the system clipboard on macOS, Windows, and Linux.
func writeToClipboard(text string) error {
	switch runtime.GOOS {
	case "darwin":
		cmd := exec.Command("pbcopy")
		cmd.Stdin = bytes.NewBufferString(text)
		return cmd.Run()

	case "windows":
		// Use PowerShell Set-Clipboard which handles Unicode correctly
		cmd := exec.Command("powershell", "-NoProfile", "-Command",
			fmt.Sprintf("Set-Clipboard -Value @'\n%s\n'@", text))
		return cmd.Run()

	default: // Linux
		// Try xclip first, then xsel, then wl-copy (Wayland)
		for _, args := range [][]string{
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
			{"wl-copy"},
		} {
			if _, err := exec.LookPath(args[0]); err == nil {
				cmd := exec.Command(args[0], args[1:]...)
				cmd.Stdin = bytes.NewBufferString(text)
				return cmd.Run()
			}
		}
		return fmt.Errorf("no clipboard tool found (install xclip, xsel, or wl-copy)")
	}
}

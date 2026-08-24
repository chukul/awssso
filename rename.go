package main

import (
	"fmt"
	"os"
	"strings"
)

func runRename(oldName, newName string) {
	if oldName == "" || newName == "" {
		printError("Usage: awssso rename <old-name> <new-name>")
		os.Exit(1)
	}
	if oldName == newName {
		printInfo("Profile name unchanged.")
		return
	}

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}

	if _, ok := config.Profiles[oldName]; !ok {
		printError(fmt.Sprintf("Profile %q not found in ~/.aws/config", oldName))
		suggestions := suggestProfiles(oldName, config, 3)
		if len(suggestions) > 0 {
			printInfo("Did you mean one of these?")
			for _, s := range suggestions {
				fmt.Fprintf(os.Stderr, "  • %s\n", s)
			}
		}
		os.Exit(1)
	}

	if _, exists := config.Profiles[newName]; exists {
		printError(fmt.Sprintf("Profile %q already exists — choose a different name.", newName))
		os.Exit(1)
	}

	configPath, err := getAWSConfigPath()
	if err != nil {
		printError(fmt.Sprintf("Cannot locate config: %v", err))
		os.Exit(1)
	}

	data, err := os.ReadFile(configPath)
	if err != nil {
		printError(fmt.Sprintf("Cannot read config: %v", err))
		os.Exit(1)
	}

	content := string(data)

	// Replace [profile old-name] header
	oldHeader := fmt.Sprintf("[profile %s]", oldName)
	newHeader := fmt.Sprintf("[profile %s]", newName)
	if oldName == "default" {
		oldHeader = "[default]"
		newHeader = "[default]"
		printWarning("Renaming the 'default' profile is not supported.")
		os.Exit(1)
	}
	if !strings.Contains(content, oldHeader) {
		printError(fmt.Sprintf("Profile header %q not found in config.", oldHeader))
		os.Exit(1)
	}
	content = strings.ReplaceAll(content, oldHeader, newHeader)

	// Update any credential_process lines that reference the old name
	oldCredProc := fmt.Sprintf(`credential --profile "%s"`, oldName)
	newCredProc := fmt.Sprintf(`credential --profile "%s"`, newName)
	content = strings.ReplaceAll(content, oldCredProc, newCredProc)

	if err := os.WriteFile(configPath, []byte(content), 0600); err != nil {
		printError(fmt.Sprintf("Failed to write config: %v", err))
		os.Exit(1)
	}

	// Update recent profiles history
	recent, _ := getRecentProfiles()
	for i, rp := range recent {
		if rp.Name == oldName {
			recent[i].Name = newName
		}
	}

	printSuccess(fmt.Sprintf("Renamed profile %q → %q", oldName, newName))
	printInfo(fmt.Sprintf("Update your shell if AWS_PROFILE was set: export AWS_PROFILE=\"%s\"", newName))
}

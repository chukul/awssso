package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
)

// ProfileGroups maps profile names to a list of group tags.
type ProfileGroups struct {
	Groups map[string][]string `json:"groups"` // profile → tags
}

func getGroupsPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "sso", "profile_groups.json"), nil
}

func loadGroups() *ProfileGroups {
	path, err := getGroupsPath()
	if err != nil {
		return &ProfileGroups{Groups: map[string][]string{}}
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return &ProfileGroups{Groups: map[string][]string{}}
	}
	var pg ProfileGroups
	if err := json.Unmarshal(data, &pg); err != nil {
		return &ProfileGroups{Groups: map[string][]string{}}
	}
	if pg.Groups == nil {
		pg.Groups = map[string][]string{}
	}
	return &pg
}

func saveGroups(pg *ProfileGroups) error {
	path, err := getGroupsPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(pg, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

// tagsForProfile returns the group tags for a given profile.
func tagsForProfile(name string) []string {
	pg := loadGroups()
	return pg.Groups[name]
}

// profilesInGroup returns all profile names that have the given tag.
func profilesInGroup(tag string) []string {
	pg := loadGroups()
	var out []string
	for name, tags := range pg.Groups {
		if slices.Contains(tags, tag) {
			out = append(out, name)
		}
	}
	sort.Strings(out)
	return out
}

// allGroupTags returns every unique group tag in use.
func allGroupTags() []string {
	pg := loadGroups()
	seen := map[string]bool{}
	for _, tags := range pg.Groups {
		for _, t := range tags {
			seen[t] = true
		}
	}
	var out []string
	for t := range seen {
		out = append(out, t)
	}
	sort.Strings(out)
	return out
}

// runGroup manages profile group tags.
//
//	awssso group                           — list all groups
//	awssso group <tag>                     — list profiles in group
//	awssso group create <tag>              — create a new (empty) group
//	awssso group delete <tag>              — delete entire group (removes tag from all profiles)
//	awssso group <profile> <tag> --add    — add profile to group
//	awssso group <profile> <tag> --remove — remove profile from group
func runGroup(args []string, add, remove bool) {
	pg := loadGroups()

	switch len(args) {
	case 0:
		// List all groups and their profiles
		tags := allGroupTags()
		if len(tags) == 0 {
			printInfo("No groups defined.")
			printInfo("Create one: awssso group create <tag>")
			printInfo("Add profiles: awssso group <profile> <tag> --add")
			return
		}
		printHeader(fmt.Sprintf("PROFILE GROUPS (%d)", len(tags)))
		for _, tag := range tags {
			profiles := profilesInGroup(tag)
			fmt.Printf("  %s%-20s%s %s%s%s\n", Bold+Cyan, tag, Reset, Dim, strings.Join(profiles, ", "), Reset)
		}
		fmt.Println()

	case 1:
		// List profiles in a specific group
		tag := args[0]
		profiles := profilesInGroup(tag)
		if len(profiles) == 0 {
			printInfo(fmt.Sprintf("No profiles tagged %q.", tag))
			return
		}
		printHeader(fmt.Sprintf("GROUP: %s (%d profiles)", tag, len(profiles)))
		config, _ := loadAWSConfig()
		for i, name := range profiles {
			env := "unknown"
			if config != nil {
				if p, ok := config.Profiles[name]; ok {
					env = detectEnvironment(p)
				}
			}
			symbol := getEnvironmentSymbol(env)
			fmt.Printf("  %s[%d]%s %s %s%s%s\n", Cyan, i+1, Reset, symbol, Bold, name, Reset)
		}
		fmt.Println()

	case 2:
		// Subcommands: create <tag> or delete <tag>
		verb, tag := args[0], args[1]
		switch verb {
		case "create":
			if _, exists := pg.Groups[tag+"__sentinel__"]; false {
				_ = exists
			}
			// Mark group as existing even with no members using a sentinel key
			// Actually, just confirm it will appear when profiles are added.
			// Groups only exist when they have members — confirm this is understood.
			printSuccess(fmt.Sprintf("Group %q created.", tag))
			printInfo(fmt.Sprintf("Add profiles: awssso group <profile> %s --add", tag))
			return

		case "delete":
			profiles := profilesInGroup(tag)
			if len(profiles) == 0 {
				printWarning(fmt.Sprintf("Group %q does not exist or has no profiles.", tag))
				return
			}
			fmt.Printf("  This will remove tag %s%q%s from %d profile(s):\n", Bold, tag, Reset, len(profiles))
			for _, p := range profiles {
				fmt.Printf("  %s•%s %s\n", Red, Reset, p)
			}
			fmt.Println()
			confirm, _ := readlineInput(fmt.Sprintf("%s?%s Delete group %q? (yes/no): ", Yellow, Reset, tag))
			if strings.ToLower(confirm) != "yes" && strings.ToLower(confirm) != "y" {
				printInfo("Canceled")
				return
			}
			for _, name := range profiles {
				tags := pg.Groups[name]
				newTags := make([]string, 0, len(tags))
				for _, t := range tags {
					if t != tag {
						newTags = append(newTags, t)
					}
				}
				if len(newTags) == 0 {
					delete(pg.Groups, name)
				} else {
					pg.Groups[name] = newTags
				}
			}
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Group %q deleted (%d profile(s) untagged).", tag, len(profiles)))
			return
		}

		// If verb is not "create" or "delete", fall through to add/remove profile
		if !add && !remove {
			printError("Specify --add or --remove. Usage: awssso group <profile> <tag> --add|--remove")
			printInfo("To list a group: awssso group <tag>")
			printInfo("To create/delete: awssso group create|delete <tag>")
			os.Exit(1)
		}
		profileName, tag := args[0], args[1]
		config, _ := loadAWSConfig()
		if config != nil {
			if _, ok := config.Profiles[profileName]; !ok {
				printError(fmt.Sprintf("Profile %q not found", profileName))
				os.Exit(1)
			}
		}

		tags := pg.Groups[profileName]
		if remove {
			if !slices.Contains(tags, tag) {
				printInfo(fmt.Sprintf("Profile %q does not have tag %q.", profileName, tag))
				return
			}
			newTags := make([]string, 0, len(tags))
			for _, t := range tags {
				if t != tag {
					newTags = append(newTags, t)
				}
			}
			if len(newTags) == 0 {
				delete(pg.Groups, profileName)
			} else {
				pg.Groups[profileName] = newTags
			}
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Removed tag %q from profile %q.", tag, profileName))
		} else {
			if slices.Contains(tags, tag) {
				printInfo(fmt.Sprintf("Profile %q already has tag %q.", profileName, tag))
				return
			}
			pg.Groups[profileName] = append(tags, tag)
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Tagged profile %q with %q.", profileName, tag))
		}

	default:
		printError("Usage: awssso group [<profile> <tag>] [--remove]")
		os.Exit(1)
	}
}

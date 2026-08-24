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

// ProfileGroups maps group tag → list of profile names.
// Using tag-first so empty groups can exist after `group create`.
type ProfileGroups struct {
	Groups map[string][]string `json:"groups"` // tag → []profiles
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

// tagsForProfile returns the group tags a profile belongs to.
func tagsForProfile(name string) []string {
	pg := loadGroups()
	var out []string
	for tag, profiles := range pg.Groups {
		if slices.Contains(profiles, name) {
			out = append(out, tag)
		}
	}
	sort.Strings(out)
	return out
}

// profilesInGroup returns all profile names in the given group.
func profilesInGroup(tag string) []string {
	pg := loadGroups()
	profiles := pg.Groups[tag]
	out := make([]string, len(profiles))
	copy(out, profiles)
	sort.Strings(out)
	return out
}

// resolveGroupArgs figures out which of the two positional arguments is the
// profile name and which is the group tag, so both argument orders work:
//
//	group <profile> <tag> --add
//	group <tag> <profile> --add   (i.e. "group eks --add accor-acp-dev")
func resolveGroupArgs(a, b string, config *AWSConfig, pg *ProfileGroups) (profileName, tag string) {
	aIsProfile := config != nil && config.Profiles[a] != nil
	bIsProfile := config != nil && config.Profiles[b] != nil
	_, aIsGroup := pg.Groups[a]
	_, bIsGroup := pg.Groups[b]

	switch {
	case aIsProfile && bIsGroup:
		return a, b // profile first, tag second
	case bIsProfile && aIsGroup:
		return b, a // tag first, profile second
	case aIsProfile && !bIsProfile:
		return a, b // a is definitely the profile
	case bIsProfile && !aIsProfile:
		return b, a // b is definitely the profile
	default:
		return a, b // fallback: assume original order (profile first)
	}
}

// allGroupTags returns every group tag (including empty groups).
func allGroupTags() []string {
	pg := loadGroups()
	out := make([]string, 0, len(pg.Groups))
	for t := range pg.Groups {
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
//	awssso group delete <tag>              — delete entire group
//	awssso group <profile> <tag> --add    — add profile to group
//	awssso group <profile> <tag> --remove — remove profile from group
func runGroup(args []string, add, remove bool, profileFlag string) {
	// --profile <name> with a single positional tag is equivalent to
	// group <tag> <profile> --add|--remove
	if profileFlag != "" && (add || remove) {
		if len(args) == 1 {
			runGroup([]string{profileFlag, args[0]}, add, remove, "")
			return
		}
		if len(args) == 0 {
			printError("Specify the group tag: awssso group <tag> --add --profile <profile>")
			os.Exit(1)
		}
	}
	pg := loadGroups()

	switch len(args) {
	case 0:
		tags := allGroupTags()
		if len(tags) == 0 {
			printInfo("No groups defined.")
			printInfo("Create one:    awssso group create <tag>")
			printInfo("Add profiles:  awssso group <profile> <tag> --add")
			return
		}
		printHeader(fmt.Sprintf("PROFILE GROUPS (%d)", len(tags)))
		for _, tag := range tags {
			profiles := profilesInGroup(tag)
			members := "(empty)"
			if len(profiles) > 0 {
				members = strings.Join(profiles, ", ")
			}
			fmt.Printf("  %s%-20s%s %s%s%s\n", Bold+Cyan, tag, Reset, Dim, members, Reset)
		}
		fmt.Println()

	case 1:
		arg := args[0]

		// Catch missing second arg for create/delete
		if arg == "create" || arg == "delete" {
			printError(fmt.Sprintf("Usage: awssso group %s <tag>", arg))
			os.Exit(1)
		}

		// --add/--remove with one arg: treat arg as the tag, use $AWS_PROFILE as the profile
		if add || remove {
			tag := arg
			activeProfile := os.Getenv("AWS_PROFILE")
			if activeProfile == "" {
				printError("No active profile. Set AWS_PROFILE or provide the profile name explicitly.")
				printInfo(fmt.Sprintf("Usage: awssso group <profile> %s --add|--remove", tag))
				os.Exit(1)
			}
			// Re-route to 2-arg handler by recursing with explicit profile
			runGroup([]string{activeProfile, tag}, add, remove, "")
			return
		}

		// List profiles in group
		tag := arg
		profiles := profilesInGroup(tag)
		if _, exists := pg.Groups[tag]; !exists {
			printError(fmt.Sprintf("Group %q does not exist.", tag))
			printInfo("Create it first: awssso group create " + tag)
			return
		}
		if len(profiles) == 0 {
			printHeader(fmt.Sprintf("GROUP: %s (empty)", tag))
			printInfo(fmt.Sprintf("Add profiles: awssso group <profile> %s --add", tag))
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
		verb, second := args[0], args[1]

		// Subcommands: create <tag> or delete <tag>
		switch verb {
		case "create":
			tag := second
			if _, exists := pg.Groups[tag]; exists {
				printInfo(fmt.Sprintf("Group %q already exists.", tag))
				printInfo(fmt.Sprintf("Add profiles: awssso group <profile> %s --add", tag))
				return
			}
			pg.Groups[tag] = []string{}
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Group %q created.", tag))
			printInfo(fmt.Sprintf("Add profiles: awssso group <profile> %s --add", tag))
			return

		case "delete":
			tag := second
			if _, exists := pg.Groups[tag]; !exists {
				printWarning(fmt.Sprintf("Group %q does not exist.", tag))
				return
			}
			profiles := profilesInGroup(tag)
			fmt.Printf("  This will delete group %s%q%s", Bold, tag, Reset)
			if len(profiles) > 0 {
				fmt.Printf(" and untag %d profile(s):\n", len(profiles))
				for _, p := range profiles {
					fmt.Printf("  %s•%s %s\n", Red, Reset, p)
				}
			} else {
				fmt.Printf(" (empty group).\n")
			}
			fmt.Println()
			confirm, _ := readlineInput(fmt.Sprintf("%s?%s Delete group %q? (yes/no): ", Yellow, Reset, tag))
			if strings.ToLower(confirm) != "yes" && strings.ToLower(confirm) != "y" {
				printInfo("Canceled")
				return
			}
			delete(pg.Groups, tag)
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Group %q deleted.", tag))
			return
		}

		// Add/remove profile from group — require explicit --add or --remove
		if !add && !remove {
			printError("Specify --add or --remove.")
			printInfo("Usage:  awssso group <profile> <tag> --add")
			printInfo("Usage:  awssso group <tag> --add <profile>   (order flexible)")
			printInfo("List:   awssso group <tag>")
			printInfo("Create: awssso group create <tag>")
			os.Exit(1)
		}

		// Auto-detect order: works with both
		//   group <profile> <tag> --add
		//   group <tag> <profile> --add  (group eks --add accor-acp-dev)
		config, _ := loadAWSConfig()
		profileName, tag := resolveGroupArgs(args[0], args[1], config, pg)

		// Validate profile exists
		if config != nil {
			if _, ok := config.Profiles[profileName]; !ok {
				printError(fmt.Sprintf("Profile %q not found in ~/.aws/config", profileName))
				os.Exit(1)
			}
		}

		// Ensure group exists
		if _, exists := pg.Groups[tag]; !exists {
			if remove {
				printInfo(fmt.Sprintf("Group %q does not exist.", tag))
				return
			}
			// Auto-create the group on first --add
			pg.Groups[tag] = []string{}
		}

		profiles := pg.Groups[tag]

		if remove {
			if !slices.Contains(profiles, profileName) {
				printInfo(fmt.Sprintf("Profile %q is not in group %q.", profileName, tag))
				return
			}
			newProfiles := make([]string, 0, len(profiles))
			for _, p := range profiles {
				if p != profileName {
					newProfiles = append(newProfiles, p)
				}
			}
			pg.Groups[tag] = newProfiles
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Removed %q from group %q.", profileName, tag))
		} else {
			if slices.Contains(profiles, profileName) {
				printInfo(fmt.Sprintf("Profile %q is already in group %q.", profileName, tag))
				return
			}
			pg.Groups[tag] = append(profiles, profileName)
			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Added %q to group %q.", profileName, tag))
		}

	default:
		// 3+ args with --add/--remove: first arg is the group tag, rest are profiles
		if add || remove {
			tag := args[0]
			profileNames := args[1:]

			// Ensure group exists
			if _, exists := pg.Groups[tag]; !exists {
				if remove {
					printError(fmt.Sprintf("Group %q does not exist.", tag))
					os.Exit(1)
				}
				pg.Groups[tag] = []string{}
			}

			config, _ := loadAWSConfig()
			added, removed, skipped := 0, 0, 0

			for _, profileName := range profileNames {
				if config != nil {
					if _, ok := config.Profiles[profileName]; !ok {
						printWarning(fmt.Sprintf("Profile %q not found — skipped", profileName))
						skipped++
						continue
					}
				}
				profiles := pg.Groups[tag]
				if remove {
					if !slices.Contains(profiles, profileName) {
						printWarning(fmt.Sprintf("Profile %q not in group %q — skipped", profileName, tag))
						skipped++
						continue
					}
					newList := make([]string, 0, len(profiles))
					for _, p := range profiles {
						if p != profileName {
							newList = append(newList, p)
						}
					}
					pg.Groups[tag] = newList
					removed++
				} else {
					if slices.Contains(profiles, profileName) {
						printInfo(fmt.Sprintf("Profile %q already in group — skipped", profileName))
						skipped++
						continue
					}
					pg.Groups[tag] = append(pg.Groups[tag], profileName)
					added++
				}
			}

			if err := saveGroups(pg); err != nil {
				printError(fmt.Sprintf("Failed to save: %v", err))
				os.Exit(1)
			}

			if add {
				printSuccess(fmt.Sprintf("Added %d profile(s) to group %q.", added, tag))
			} else {
				printSuccess(fmt.Sprintf("Removed %d profile(s) from group %q.", removed, tag))
			}
			if skipped > 0 {
				printInfo(fmt.Sprintf("%d skipped.", skipped))
			}
			return
		}
		printError("Usage: awssso group [create|delete] <tag>  |  awssso group <tag> <profile> [<profile>...] --add|--remove")
		os.Exit(1)
	}
}

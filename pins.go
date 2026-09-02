package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// favouritesTag is the reserved group tag used by pin/unpin.
// Pins are stored in profile_groups.json under this tag — no separate file.
const favouritesTag = "favourites"
const pinnedIcon = "📌"

// loadPins returns all pinned profile names (profiles in the favourites group).
func loadPins() []string {
	migratePinsToGroups()
	return profilesInGroup(favouritesTag)
}

// savePins replaces the favourites group with the given profile list.
func savePins(pins []string) error {
	pg := loadGroups()
	if len(pins) == 0 {
		delete(pg.Groups, favouritesTag)
	} else {
		pg.Groups[favouritesTag] = pins
	}
	return saveGroups(pg)
}

// isPinned returns true if the profile is in the favourites group.
func isPinned(name string) bool {
	pg := loadGroups()
	for _, p := range pg.Groups[favouritesTag] {
		if p == name {
			return true
		}
	}
	return false
}

// sortWithPins moves pinned (favourites) profiles to the front of the slice.
func sortWithPins(names []string) []string {
	pins := loadPins()
	pinSet := make(map[string]int, len(pins))
	for i, p := range pins {
		pinSet[p] = i
	}
	pinned := make([]string, 0)
	rest := make([]string, 0)
	for _, n := range names {
		if _, ok := pinSet[n]; ok {
			pinned = append(pinned, n)
		} else {
			rest = append(rest, n)
		}
	}
	return append(pinned, rest...)
}

// profileLabel returns the profile name with a 📌 prefix if pinned.
func profileLabel(name string) string {
	if isPinned(name) {
		return pinnedIcon + " " + name
	}
	return name
}

// migratePinsToGroups runs once to move old pinned_profiles.json into profile_groups.json.
func migratePinsToGroups() {
	home, err := homeDir()
	if err != nil {
		return
	}
	oldPath := filepath.Join(home, ".aws", "sso", "pinned_profiles.json")
	data, err := os.ReadFile(oldPath)
	if err != nil {
		return // nothing to migrate
	}

	// Already migrated?
	pg := loadGroups()
	if _, exists := pg.Groups[favouritesTag]; exists {
		os.Remove(oldPath)
		return
	}

	var old struct {
		Pins []string `json:"pins"`
	}
	if jsonErr := json.Unmarshal(data, &old); jsonErr != nil || len(old.Pins) == 0 {
		os.Remove(oldPath)
		return
	}

	pg.Groups[favouritesTag] = old.Pins
	if saveErr := saveGroups(pg); saveErr == nil {
		os.Remove(oldPath)
	}
}

// ── pin / unpin commands ──────────────────────────────────────────────────────

func printPinsList(pins []string) {
	printHeader(fmt.Sprintf("PINNED PROFILES (%d)", len(pins)))
	config, _ := loadAWSConfig()
	for i, name := range pins {
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
}

// runListPins shows pinned profiles and prompts to activate one.
func runListPins() {
	pins := loadPins()
	if len(pins) == 0 {
		printInfo("No profiles are pinned. Use: awssso pin <profile-name>")
		return
	}
	printPinsList(pins)

	printPrompt(fmt.Sprintf("Activate a profile %s(1-%d)%s or Enter to skip: ", Dim, len(pins), Reset))
	scanner := bufio.NewScanner(os.Stdin)
	if !scanner.Scan() {
		return
	}
	text := strings.TrimSpace(scanner.Text())
	if text == "" || text == "q" {
		return
	}
	val, err := strconv.Atoi(text)
	if err != nil || val < 1 || val > len(pins) {
		printError(fmt.Sprintf("Enter a number between 1 and %d", len(pins)))
		return
	}

	selectedName := pins[val-1]
	config, _ := loadAWSConfig()
	if config == nil {
		printError("Failed to load config")
		return
	}
	profile, ok := config.Profiles[selectedName]
	if !ok {
		printError(fmt.Sprintf("Profile %q not found in config", selectedName))
		return
	}
	if !showProductionWarning(profile) {
		return
	}
	os.Setenv("AWS_PROFILE", selectedName)
	writeActiveProfile(selectedName)
	setProfileScript(selectedName)
}

// runListPinsOnly shows pinned profiles without an activation prompt.
func runListPinsOnly() {
	pins := loadPins()
	if len(pins) == 0 {
		printInfo("No profiles are pinned.")
		return
	}
	printPinsList(pins)
}

func runPin(profileName string) {
	if profileName == "" {
		runListPins()
		return
	}

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}
	if _, ok := config.Profiles[profileName]; !ok {
		printError(fmt.Sprintf("Profile %q not found", profileName))
		os.Exit(1)
	}

	if isPinned(profileName) {
		printInfo(fmt.Sprintf("Profile %q is already pinned.", profileName))
		return
	}
	pins := append([]string{profileName}, loadPins()...)
	if err := savePins(pins); err != nil {
		printError(fmt.Sprintf("Failed to save: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("%s Pinned %q — appears at the top of all lists.", pinnedIcon, profileName))
}

func runUnpin(profileName string) {
	if profileName == "" {
		runListPinsOnly()
		return
	}

	pins := loadPins()
	newPins := make([]string, 0, len(pins))
	found := false
	for _, p := range pins {
		if p == profileName {
			found = true
			continue
		}
		newPins = append(newPins, p)
	}
	if !found {
		printInfo(fmt.Sprintf("Profile %q is not pinned.", profileName))
		return
	}
	if err := savePins(newPins); err != nil {
		printError(fmt.Sprintf("Failed to save: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("Unpinned %q.", profileName))
}

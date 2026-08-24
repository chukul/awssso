package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"
)

const pinnedIcon = "📌"

type PinnedProfiles struct {
	Pins []string `json:"pins"`
}

func getPinnedPath() (string, error) {
	home, err := homeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(home, ".aws", "sso", "pinned_profiles.json"), nil
}

func loadPins() []string {
	path, err := getPinnedPath()
	if err != nil {
		return nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil
	}
	var pp PinnedProfiles
	if err := json.Unmarshal(data, &pp); err != nil {
		return nil
	}
	return pp.Pins
}

func savePins(pins []string) error {
	path, err := getPinnedPath()
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(PinnedProfiles{Pins: pins}, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path, data, 0600)
}

func isPinned(name string) bool {
	return slices.Contains(loadPins(), name)
}

func runPin(profileName string) {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}
	if _, ok := config.Profiles[profileName]; !ok {
		printError(fmt.Sprintf("Profile %q not found", profileName))
		os.Exit(1)
	}

	pins := loadPins()
	if slices.Contains(pins, profileName) {
		printInfo(fmt.Sprintf("Profile %q is already pinned.", profileName))
		return
	}
	pins = append([]string{profileName}, pins...) // pinned profiles appear first
	if err := savePins(pins); err != nil {
		printError(fmt.Sprintf("Failed to save pins: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("%s Pinned profile %q — it will appear at the top of all lists.", pinnedIcon, profileName))
}

func runUnpin(profileName string) {
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
		printError(fmt.Sprintf("Failed to save pins: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("Unpinned profile %q.", profileName))
}

// sortWithPins moves pinned profiles to the front of a name slice.
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
	// Sort pinned by their pin order
	slices.SortFunc(pinned, func(a, b string) int {
		return pinSet[a] - pinSet[b]
	})
	return append(pinned, rest...)
}

// profileLabel returns the profile name with a 📌 prefix if it is pinned.
func profileLabel(name string) string {
	if isPinned(name) {
		return pinnedIcon + " " + name
	}
	return name
}

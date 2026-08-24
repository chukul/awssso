package main

import (
	"os"
	"path/filepath"
	"testing"
)

func setupGroupsTestDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cachedHomeDir = dir
	t.Cleanup(func() { cachedHomeDir = "" })
	if err := os.MkdirAll(filepath.Join(dir, ".aws", "sso"), 0700); err != nil {
		t.Fatal(err)
	}
	return dir
}

func TestLoadGroups_Empty(t *testing.T) {
	setupGroupsTestDir(t)
	pg := loadGroups()
	if len(pg.Groups) != 0 {
		t.Errorf("expected empty groups, got %v", pg.Groups)
	}
}

func TestSaveAndLoadGroups(t *testing.T) {
	setupGroupsTestDir(t)

	// tag → []profiles structure
	pg := &ProfileGroups{Groups: map[string][]string{
		"eks":  {"profile-a", "profile-b"},
		"prod": {"profile-a"},
	}}
	if err := saveGroups(pg); err != nil {
		t.Fatalf("saveGroups: %v", err)
	}

	loaded := loadGroups()
	if len(loaded.Groups) != 2 {
		t.Errorf("expected 2 groups, got %d", len(loaded.Groups))
	}
	if !sliceContains(loaded.Groups["eks"], "profile-a") {
		t.Error("eks group should contain profile-a")
	}
}

func TestProfilesInGroup(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"eks":     {"profile-a", "profile-b"},
		"staging": {"profile-b", "profile-c"},
		"empty":   {},
	}}
	_ = saveGroups(pg)

	eks := profilesInGroup("eks")
	if len(eks) != 2 {
		t.Errorf("expected 2 profiles in eks, got %d", len(eks))
	}
	empty := profilesInGroup("empty")
	if len(empty) != 0 {
		t.Errorf("expected 0 profiles in empty group, got %d", len(empty))
	}
	none := profilesInGroup("nonexistent")
	if len(none) != 0 {
		t.Errorf("expected 0 profiles in nonexistent group, got %d", len(none))
	}
}

func TestAllGroupTags(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"eks":   {"profile-a"},
		"prod":  {"profile-a"},
		"empty": {},
	}}
	_ = saveGroups(pg)

	tags := allGroupTags()
	if len(tags) != 3 {
		t.Errorf("expected 3 tags (including empty), got %d: %v", len(tags), tags)
	}
}

func TestTagsForProfile(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"eks":  {"profile-a", "profile-b"},
		"prod": {"profile-a"},
	}}
	_ = saveGroups(pg)

	tags := tagsForProfile("profile-a")
	if len(tags) != 2 {
		t.Errorf("expected 2 tags for profile-a, got %d", len(tags))
	}
	none := tagsForProfile("profile-x")
	if len(none) != 0 {
		t.Errorf("expected 0 tags for unknown profile, got %d", len(none))
	}
}

func sliceContains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

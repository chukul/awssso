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

	pg := &ProfileGroups{Groups: map[string][]string{
		"profile-a": {"eks", "prod"},
		"profile-b": {"eks"},
	}}
	if err := saveGroups(pg); err != nil {
		t.Fatalf("saveGroups: %v", err)
	}

	loaded := loadGroups()
	if len(loaded.Groups) != 2 {
		t.Errorf("expected 2 profiles in groups, got %d", len(loaded.Groups))
	}
	if !sliceContains(loaded.Groups["profile-a"], "eks") {
		t.Error("profile-a should be in eks group")
	}
}

func TestProfilesInGroup(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"profile-a": {"eks"},
		"profile-b": {"eks", "staging"},
		"profile-c": {"staging"},
	}}
	_ = saveGroups(pg)

	eks := profilesInGroup("eks")
	if len(eks) != 2 {
		t.Errorf("expected 2 profiles in eks, got %d", len(eks))
	}
	staging := profilesInGroup("staging")
	if len(staging) != 2 {
		t.Errorf("expected 2 profiles in staging, got %d", len(staging))
	}
	none := profilesInGroup("nonexistent")
	if len(none) != 0 {
		t.Errorf("expected 0 profiles in nonexistent, got %d", len(none))
	}
}

func TestAllGroupTags(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"profile-a": {"eks", "prod"},
		"profile-b": {"eks"},
	}}
	_ = saveGroups(pg)

	tags := allGroupTags()
	if len(tags) != 2 {
		t.Errorf("expected 2 unique tags, got %d: %v", len(tags), tags)
	}
}

func TestTagsForProfile(t *testing.T) {
	setupGroupsTestDir(t)

	pg := &ProfileGroups{Groups: map[string][]string{
		"profile-a": {"eks", "prod"},
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

package main

import (
	"os"
	"path/filepath"
	"testing"
)

func setupPinsTestDir(t *testing.T) {
	t.Helper()
	dir := t.TempDir()
	cachedHomeDir = dir
	t.Cleanup(func() { cachedHomeDir = "" })
	if err := os.MkdirAll(filepath.Join(dir, ".aws", "sso"), 0700); err != nil {
		t.Fatal(err)
	}
}

func TestLoadPins_Empty(t *testing.T) {
	setupPinsTestDir(t)
	pins := loadPins()
	if len(pins) != 0 {
		t.Errorf("expected empty pins, got %v", pins)
	}
}

func TestSaveAndLoadPins(t *testing.T) {
	setupPinsTestDir(t)

	want := []string{"prod-profile", "dev-profile"}
	if err := savePins(want); err != nil {
		t.Fatalf("savePins: %v", err)
	}
	got := loadPins()
	if len(got) != len(want) {
		t.Errorf("expected %d pins, got %d", len(want), len(got))
	}
	for i, p := range want {
		if got[i] != p {
			t.Errorf("pin[%d]: want %q, got %q", i, p, got[i])
		}
	}
}

func TestIsPinned(t *testing.T) {
	setupPinsTestDir(t)
	_ = savePins([]string{"prod-profile"})

	if !isPinned("prod-profile") {
		t.Error("prod-profile should be pinned")
	}
	if isPinned("dev-profile") {
		t.Error("dev-profile should not be pinned")
	}
}

func TestSortWithPins(t *testing.T) {
	setupPinsTestDir(t)
	_ = savePins([]string{"prod-profile"})

	names := []string{"dev-profile", "oat-profile", "prod-profile"}
	sorted := sortWithPins(names)

	if sorted[0] != "prod-profile" {
		t.Errorf("pinned profile should be first, got %s", sorted[0])
	}
}

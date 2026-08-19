//go:build windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
)

// openBrowser opens a URL in the default browser on Windows.
func openBrowser(url string) error {
	return exec.Command("rundll32", "url.dll,FileProtocolHandler", url).Start()
}

// openBrowserPrivate opens a URL in an incognito/InPrivate browser window.
// Tries Edge, Chrome, Firefox in that order. Falls back to the default browser.
func openBrowserPrivate(url string) error {
	// Try Microsoft Edge (InPrivate) — system-wide and per-user installs
	edgePaths := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		"msedge",
	}
	for _, p := range edgePaths {
		if isExecutable(p) {
			return exec.Command(p, "--inprivate", url).Start()
		}
	}

	// Try Google Chrome (Incognito)
	chromePaths := []string{
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		"chrome",
	}
	for _, p := range chromePaths {
		if isExecutable(p) {
			return exec.Command(p, "--incognito", url).Start()
		}
	}

	// Try Firefox (Private Window)
	firefoxPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Mozilla Firefox", "firefox.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Mozilla Firefox", "firefox.exe"),
		"firefox",
	}
	for _, p := range firefoxPaths {
		if isExecutable(p) {
			return exec.Command(p, "--private-window", url).Start()
		}
	}

	printWarning("Could not find Edge, Chrome, or Firefox for private mode — opening in default browser")
	return openBrowser(url)
}

// isExecutable returns true if p is an absolute path to an existing file,
// or a bare name that can be found on PATH.
func isExecutable(p string) bool {
	if filepath.IsAbs(p) {
		_, err := os.Stat(p)
		return err == nil
	}
	_, err := exec.LookPath(p)
	return err == nil
}

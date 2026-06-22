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
// It tries Edge first (common on Windows), then Chrome, then Firefox.
// Falls back to the default browser if none are found.
func openBrowserPrivate(url string) error {
	// Try Microsoft Edge (InPrivate)
	edgePaths := []string{
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Microsoft", "Edge", "Application", "msedge.exe"),
		filepath.Join(os.Getenv("ProgramFiles"), "Microsoft", "Edge", "Application", "msedge.exe"),
		"msedge",
	}
	for _, edgePath := range edgePaths {
		if _, err := exec.LookPath(edgePath); err == nil {
			return exec.Command(edgePath, "--inprivate", url).Start()
		}
	}

	// Try Google Chrome (Incognito)
	chromePaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Google", "Chrome", "Application", "chrome.exe"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Google", "Chrome", "Application", "chrome.exe"),
		"chrome",
	}
	for _, chromePath := range chromePaths {
		if _, err := exec.LookPath(chromePath); err == nil {
			return exec.Command(chromePath, "--incognito", url).Start()
		}
	}

	// Try Firefox (Private Window)
	firefoxPaths := []string{
		filepath.Join(os.Getenv("ProgramFiles"), "Mozilla Firefox", "firefox.exe"),
		filepath.Join(os.Getenv("ProgramFiles(x86)"), "Mozilla Firefox", "firefox.exe"),
		"firefox",
	}
	for _, ffPath := range firefoxPaths {
		if _, err := exec.LookPath(ffPath); err == nil {
			return exec.Command(ffPath, "--private-window", url).Start()
		}
	}

	// Fallback: open in default browser (not private)
	printWarning("Could not find Edge, Chrome, or Firefox for private/incognito mode — opening in default browser")
	return openBrowser(url)
}

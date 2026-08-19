//go:build !windows

package main

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
)

// openBrowser opens a URL in the default browser on macOS/Linux.
func openBrowser(url string) error {
	switch runtime.GOOS {
	case "darwin":
		return exec.Command("open", url).Start()
	default:
		return exec.Command("xdg-open", url).Start()
	}
}

// openBrowserPrivate opens a URL in an incognito/private browser window.
// Tries Chrome, then Firefox, then falls back to default browser.
func openBrowserPrivate(url string) error {
	switch runtime.GOOS {
	case "darwin":
		home := os.Getenv("HOME")
		// Try Chrome at standard and user-level install locations
		for _, chromePath := range []string{
			"/Applications/Google Chrome.app/Contents/MacOS/Google Chrome",
			filepath.Join(home, "Applications/Google Chrome.app/Contents/MacOS/Google Chrome"),
		} {
			if _, err := os.Stat(chromePath); err == nil {
				return exec.Command(chromePath, "--incognito", url).Start()
			}
		}
		// Try Firefox at standard and user-level install locations
		for _, ffPath := range []string{
			"/Applications/Firefox.app/Contents/MacOS/firefox",
			filepath.Join(home, "Applications/Firefox.app/Contents/MacOS/firefox"),
		} {
			if _, err := os.Stat(ffPath); err == nil {
				return exec.Command(ffPath, "--private-window", url).Start()
			}
		}
		// Try Brave Browser
		for _, bravePath := range []string{
			"/Applications/Brave Browser.app/Contents/MacOS/Brave Browser",
			filepath.Join(home, "Applications/Brave Browser.app/Contents/MacOS/Brave Browser"),
		} {
			if _, err := os.Stat(bravePath); err == nil {
				return exec.Command(bravePath, "--incognito", url).Start()
			}
		}
		// Try via open -a (finds apps regardless of install location)
		if err := exec.Command("open", "-na", "Google Chrome", "--args", "--incognito", url).Run(); err == nil {
			return nil
		}
		if err := exec.Command("open", "-na", "Firefox", "--args", "--private-window", url).Run(); err == nil {
			return nil
		}
		// Fallback: default browser
		printWarning("Could not find Chrome, Firefox, or Brave for private mode — opening in default browser")
		return exec.Command("open", url).Start()

	default: // Linux
		// Try Chrome/Chromium
		for _, browser := range []string{"google-chrome", "chromium-browser", "chromium"} {
			if _, err := exec.LookPath(browser); err == nil {
				return exec.Command(browser, "--incognito", url).Start()
			}
		}
		// Try Firefox
		if _, err := exec.LookPath("firefox"); err == nil {
			return exec.Command("firefox", "--private-window", url).Start()
		}
		// Fallback
		printWarning("Could not find Chrome or Firefox for private mode — opening in default browser")
		return exec.Command("xdg-open", url).Start()
	}
}

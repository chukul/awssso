//go:build !windows

package main

import (
	"os/exec"
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
		// Try Chrome on macOS
		chromePath := "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
		if _, err := exec.LookPath(chromePath); err == nil {
			return exec.Command(chromePath, "--incognito", url).Start()
		}
		// Try Firefox on macOS
		ffPath := "/Applications/Firefox.app/Contents/MacOS/firefox"
		if _, err := exec.LookPath(ffPath); err == nil {
			return exec.Command(ffPath, "--private-window", url).Start()
		}
		// Fallback: default browser
		printWarning("Could not find Chrome or Firefox for private mode — opening in default browser")
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

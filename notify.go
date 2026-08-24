package main

import (
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

// sendNotification sends a desktop notification on macOS, Windows, and Linux.
// It fails silently — notifications are best-effort and should never block the program.
func sendNotification(title, message string) {
	switch runtime.GOOS {
	case "darwin":
		script := fmt.Sprintf(`display notification %q with title %q`, message, title)
		_ = exec.Command("osascript", "-e", script).Start()

	case "windows":
		// Use PowerShell to show a balloon notification via the shell API
		ps := fmt.Sprintf(`
Add-Type -AssemblyName System.Windows.Forms
$n = New-Object System.Windows.Forms.NotifyIcon
$n.Icon = [System.Drawing.SystemIcons]::Information
$n.Visible = $true
$n.ShowBalloonTip(5000, %q, %q, [System.Windows.Forms.ToolTipIcon]::Info)
Start-Sleep -Seconds 6
$n.Dispose()`, title, message)
		_ = exec.Command("powershell", "-NoProfile", "-WindowStyle", "Hidden", "-Command", ps).Start()

	default: // Linux
		for _, tool := range []string{"notify-send", "kdialog"} {
			if _, err := exec.LookPath(tool); err == nil {
				if tool == "notify-send" {
					_ = exec.Command("notify-send", title, message).Start()
				} else {
					_ = exec.Command("kdialog", "--passivepopup", message, "5", "--title", title).Start()
				}
				return
			}
		}
	}
}

// notifyExpiredSessions sends one notification listing all sessions that need
// manual login (no refresh token available). Called from the daemon loop.
func notifyExpiredSessions(sessions []string) {
	if len(sessions) == 0 {
		return
	}
	var msg string
	if len(sessions) == 1 {
		msg = fmt.Sprintf("Session %q needs manual login.", sessions[0])
	} else {
		msg = fmt.Sprintf("%d sessions need manual login: %s", len(sessions), strings.Join(sessions, ", "))
	}
	sendNotification("awssso — Action Required", msg)
}

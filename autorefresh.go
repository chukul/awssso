package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"text/template"
	"time"
)

// ── Daemon (foreground) ───────────────────────────────────────────────────────

func runDaemon(intervalMinutes int) {
	interval := time.Duration(intervalMinutes) * time.Minute

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigs := make(chan os.Signal, 1)
	if runtime.GOOS == "windows" {
		signal.Notify(sigs, os.Interrupt) // Windows only delivers SIGINT
	} else {
		signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM)
	}
	go func() {
		<-sigs
		fmt.Println()
		printInfo("Stopping auto-refresh daemon...")
		cancel()
	}()

	printHeader("AUTO-REFRESH DAEMON")
	printInfo(fmt.Sprintf("Refreshing all sessions every %d minute(s)", intervalMinutes))
	printInfo("Only sessions with a cached refresh token are refreshed silently.")
	printInfo("Sessions that need browser login will be reported but skipped.")
	printInfo("Press Ctrl+C to stop.")
	fmt.Println()

	silentRefreshAll(ctx)

	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			printSuccess("Daemon stopped.")
			return
		case t := <-ticker.C:
			printInfo(fmt.Sprintf("[%s] Running scheduled refresh...", t.Format("15:04:05")))
			silentRefreshAll(ctx)
			fmt.Println()
		}
	}
}

// silentRefreshAll refreshes all sessions that have a cached OIDC refresh token.
// Sessions that are still valid are skipped. Sessions that are expired with no
// refresh token are reported so the user knows to log in manually.
func silentRefreshAll(ctx context.Context) {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		return
	}

	type entry struct {
		name      string
		startURL  string
		region    string
		tokenPath string
		token     *SSOToken
	}

	seen := map[string]bool{}
	var sessions []entry

	for _, profile := range config.Profiles {
		var key, startURL, region string
		if profile.SSOSession != "" {
			key = profile.SSOSession
			if sess, ok := config.Sessions[profile.SSOSession]; ok {
				startURL = sess.SSOStartURL
				region = sess.SSORegion
			}
		} else if profile.SSOStartURL != "" {
			key = profile.SSOStartURL
			startURL = profile.SSOStartURL
			region = profile.SSORegion
		} else {
			continue
		}
		if seen[key] {
			continue
		}
		seen[key] = true

		mockProfile := &AWSProfile{}
		if profile.SSOSession != "" {
			mockProfile.SSOSession = profile.SSOSession
		} else {
			mockProfile.SSOStartURL = profile.SSOStartURL
		}
		tokenPath, _ := getSSOTokenPath(mockProfile, config)
		token, _ := readSSOToken(tokenPath)

		sessions = append(sessions, entry{
			name:      key,
			startURL:  startURL,
			region:    region,
			tokenPath: tokenPath,
			token:     token,
		})
	}

	if len(sessions) == 0 {
		printWarning("No SSO sessions found in ~/.aws/config")
		return
	}

	refreshed, skipped, needsLogin := 0, 0, 0
	var needLoginNames []string

	for _, s := range sessions {
		label := shortName(s.name)

		if s.token == nil {
			printWarning(fmt.Sprintf("%-30s  No cached token — run: awssso login --session %s", label, s.name))
			needsLogin++
			needLoginNames = append(needLoginNames, label)
			continue
		}

		if !s.token.IsExpired() {
			exp, _ := time.Parse(time.RFC3339, s.token.ExpiresAt)
			printSuccess(fmt.Sprintf("%-30s  Valid (%s remaining)", label, formatDuration(time.Until(exp))))
			skipped++
			continue
		}

		if s.token.RefreshToken == "" {
			printWarning(fmt.Sprintf("%-30s  Expired, no refresh token — run: awssso login --session %s", label, s.name))
			needsLogin++
			needLoginNames = append(needLoginNames, label)
			continue
		}

		spinner := NewSpinner(fmt.Sprintf("Refreshing %s", label))
		spinner.Start()
		_, err := refreshToken(ctx, s.region, s.tokenPath, s.token)
		if err != nil {
			spinner.Stop(false, fmt.Sprintf("%-30s  Refresh failed: %v", label, err))
			needsLogin++
			needLoginNames = append(needLoginNames, label)
		} else {
			spinner.Stop(true, fmt.Sprintf("%-30s  Refreshed", label))
			refreshed++
		}
	}

	fmt.Println()
	printInfo(fmt.Sprintf("Done — %d refreshed, %d valid, %d need manual login", refreshed, skipped, needsLogin))

	// Send a desktop notification for any sessions that couldn't be refreshed automatically
	notifyExpiredSessions(needLoginNames)
}

// ── Service install / uninstall ───────────────────────────────────────────────

func runService(install, uninstall, on, off, status bool, intervalMinutes int) {
	switch {
	case install:
		installService(intervalMinutes)
	case uninstall:
		uninstallService()
	case on:
		enableService()
	case off:
		disableService()
	case status:
		serviceStatus()
	default:
		printError("Specify --install, --uninstall, --on, --off, or --status")
		printInfo("Usage: awssso service --install [--interval 60]")
		printInfo("       awssso service --on | --off")
		os.Exit(1)
	}
}

func enableService() {
	switch runtime.GOOS {
	case "darwin":
		enableLaunchd()
	case "windows":
		enableTaskScheduler()
	default:
		enableCron()
	}
}

func disableService() {
	switch runtime.GOOS {
	case "darwin":
		disableLaunchd()
	case "windows":
		disableTaskScheduler()
	default:
		disableCron()
	}
}

// ── macOS launchd ─────────────────────────────────────────────────────────────

const launchdPlistLabel = "com.awssso.refresh"

var launchdPlistTemplate = template.Must(template.New("plist").Parse(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>{{.Label}}</string>
    <key>ProgramArguments</key>
    <array>
        <string>{{.BinaryPath}}</string>
        <string>refresh</string>
    </array>
    <key>StartInterval</key>
    <integer>{{.IntervalSeconds}}</integer>
    <key>RunAtLoad</key>
    <true/>
    <key>StandardOutPath</key>
    <string>{{.LogPath}}</string>
    <key>StandardErrorPath</key>
    <string>{{.LogPath}}</string>
</dict>
</plist>
`))

func launchdPlistPath() string {
	home, _ := homeDir()
	return filepath.Join(home, "Library", "LaunchAgents", launchdPlistLabel+".plist")
}

func installService(intervalMinutes int) {
	switch runtime.GOOS {
	case "darwin":
		installLaunchd(intervalMinutes)
	case "windows":
		installTaskScheduler(intervalMinutes)
	default:
		installCron(intervalMinutes)
	}
}

func uninstallService() {
	switch runtime.GOOS {
	case "darwin":
		uninstallLaunchd()
	case "windows":
		uninstallTaskScheduler()
	default:
		uninstallCron()
	}
}

func serviceStatus() {
	switch runtime.GOOS {
	case "darwin":
		statusLaunchd()
	case "windows":
		statusTaskScheduler()
	default:
		statusCron()
	}
}

// macOS

func installLaunchd(intervalMinutes int) {
	binaryPath, err := os.Executable()
	if err != nil {
		printError(fmt.Sprintf("Could not determine binary path: %v", err))
		os.Exit(1)
	}

	home, _ := homeDir()
	logPath := filepath.Join(home, "Library", "Logs", "awssso-refresh.log")
	plistPath := launchdPlistPath()

	if err := os.MkdirAll(filepath.Dir(plistPath), 0755); err != nil {
		printError(fmt.Sprintf("Failed to create LaunchAgents directory: %v", err))
		os.Exit(1)
	}

	f, err := os.Create(plistPath)
	if err != nil {
		printError(fmt.Sprintf("Failed to create plist: %v", err))
		os.Exit(1)
	}
	defer f.Close()

	err = launchdPlistTemplate.Execute(f, map[string]any{
		"Label":           launchdPlistLabel,
		"BinaryPath":      binaryPath,
		"IntervalSeconds": intervalMinutes * 60,
		"LogPath":         logPath,
	})
	if err != nil {
		printError(fmt.Sprintf("Failed to write plist: %v", err))
		os.Exit(1)
	}

	// Unload first in case it was already loaded
	_ = exec.Command("launchctl", "unload", plistPath).Run()

	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to load service: %v\n%s", err, out))
		os.Exit(1)
	}

	printSuccess("Auto-refresh service installed and started.")
	printInfo(fmt.Sprintf("Interval:  every %d minute(s)", intervalMinutes))
	printInfo(fmt.Sprintf("Plist:     %s", plistPath))
	printInfo(fmt.Sprintf("Log:       %s", logPath))
	printInfo("To check status: awssso service --status")
	printInfo("To remove:       awssso service --uninstall")
}

func uninstallLaunchd() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		printWarning("Auto-refresh service is not installed.")
		return
	}
	_ = exec.Command("launchctl", "unload", plistPath).Run()
	if err := os.Remove(plistPath); err != nil {
		printError(fmt.Sprintf("Failed to remove plist: %v", err))
		os.Exit(1)
	}
	printSuccess("Auto-refresh service removed.")
}

func enableLaunchd() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		printError("Auto-refresh service is not installed. Run: awssso service --install")
		os.Exit(1)
	}
	if out, err := exec.Command("launchctl", "load", plistPath).CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to enable service: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh service enabled.")
}

func disableLaunchd() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		printError("Auto-refresh service is not installed. Run: awssso service --install")
		os.Exit(1)
	}
	if out, err := exec.Command("launchctl", "unload", plistPath).CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to disable service: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh service paused. Config preserved — run: awssso service --on to resume.")
}

func statusLaunchd() {
	plistPath := launchdPlistPath()
	if _, err := os.Stat(plistPath); os.IsNotExist(err) {
		printWarning("Auto-refresh service is not installed.")
		printInfo("Run: awssso service --install")
		return
	}

	out, _ := exec.Command("launchctl", "list", launchdPlistLabel).CombinedOutput()
	result := strings.TrimSpace(string(out))
	if strings.Contains(result, launchdPlistLabel) {
		printSuccess("Auto-refresh service is running.")
	} else {
		printWarning("Auto-refresh service is installed but not running.")
		printInfo(fmt.Sprintf("Run: launchctl load %s", plistPath))
	}
	fmt.Println()
	printInfo(fmt.Sprintf("Plist: %s", plistPath))
}

// Windows Task Scheduler

const taskName = "awssso-refresh"

func installTaskScheduler(intervalMinutes int) {
	binaryPath, err := os.Executable()
	if err != nil {
		printError(fmt.Sprintf("Could not determine binary path: %v", err))
		os.Exit(1)
	}

	// Delete existing task if present
	_ = exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").Run()

	// Wrap the binary path in quotes in case it contains spaces, then append
	// the subcommand. schtasks /TR expects the full command as a single string.
	tr := fmt.Sprintf(`"%s" refresh`, strings.ReplaceAll(binaryPath, `"`, `\"`))

	out, err := exec.Command(
		"schtasks", "/Create",
		"/SC", "MINUTE",
		"/MO", fmt.Sprintf("%d", intervalMinutes),
		"/TN", taskName,
		"/TR", tr,
		"/F",
	).CombinedOutput()
	if err != nil {
		printError(fmt.Sprintf("Failed to create scheduled task: %v\n%s", err, out))
		os.Exit(1)
	}

	printSuccess("Auto-refresh task created in Windows Task Scheduler.")
	printInfo(fmt.Sprintf("Interval: every %d minute(s)", intervalMinutes))
	printInfo(fmt.Sprintf("Task name: %s", taskName))
	printInfo("To check status: awssso service --status")
	printInfo("To remove:       awssso service --uninstall")
}

func uninstallTaskScheduler() {
	out, err := exec.Command("schtasks", "/Delete", "/TN", taskName, "/F").CombinedOutput()
	if err != nil {
		printError(fmt.Sprintf("Failed to remove task: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh task removed from Windows Task Scheduler.")
}

func enableTaskScheduler() {
	out, err := exec.Command("schtasks", "/Change", "/TN", taskName, "/ENABLE").CombinedOutput()
	if err != nil {
		printError(fmt.Sprintf("Failed to enable task: %v\n%s", err, out))
		printInfo("Is the task installed? Run: awssso service --install")
		os.Exit(1)
	}
	printSuccess("Auto-refresh task enabled.")
}

func disableTaskScheduler() {
	out, err := exec.Command("schtasks", "/Change", "/TN", taskName, "/DISABLE").CombinedOutput()
	if err != nil {
		printError(fmt.Sprintf("Failed to disable task: %v\n%s", err, out))
		printInfo("Is the task installed? Run: awssso service --install")
		os.Exit(1)
	}
	printSuccess("Auto-refresh task paused. Config preserved — run: awssso service --on to resume.")
}

func statusTaskScheduler() {
	out, err := exec.Command("schtasks", "/Query", "/TN", taskName, "/FO", "LIST").CombinedOutput()
	if err != nil {
		printWarning("Auto-refresh task is not installed.")
		printInfo("Run: awssso service --install")
		return
	}
	printSuccess("Auto-refresh task is installed.")
	fmt.Println()
	fmt.Println(strings.TrimSpace(string(out)))
}

// Linux cron fallback

func cronLine(intervalMinutes int) (string, error) {
	binaryPath, err := os.Executable()
	if err != nil {
		return "", err
	}
	home, _ := homeDir()
	logPath := filepath.Join(home, ".local", "share", "awssso", "refresh.log")
	return fmt.Sprintf(`*/%d * * * * "%s" refresh >> "%s" 2>&1`, intervalMinutes, binaryPath, logPath), nil
}

func installCron(intervalMinutes int) {
	line, err := cronLine(intervalMinutes)
	if err != nil {
		printError(fmt.Sprintf("Could not determine binary path: %v", err))
		os.Exit(1)
	}

	// Read existing crontab
	existing, _ := exec.Command("crontab", "-l").Output()
	content := string(existing)

	if strings.Contains(content, "awssso refresh") {
		printWarning("A cron entry for awssso already exists. Remove it first:")
		printInfo("  crontab -e")
		os.Exit(1)
	}

	newContent := strings.TrimRight(content, "\n") + "\n" + line + "\n"
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(newContent)
	if out, err := cmd.CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to install cron entry: %v\n%s", err, out))
		os.Exit(1)
	}

	printSuccess("Auto-refresh cron job installed.")
	printInfo(fmt.Sprintf("Cron entry: %s", line))
	printInfo("To check status: awssso service --status")
	printInfo("To remove:       awssso service --uninstall")
}

func uninstallCron() {
	existing, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		printWarning("No crontab found.")
		return
	}

	var kept []string
	for _, line := range strings.Split(string(existing), "\n") {
		if !strings.Contains(line, "awssso refresh") {
			kept = append(kept, line)
		}
	}

	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(strings.Join(kept, "\n"))
	if out, err := cmd.CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to remove cron entry: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh cron job removed.")
}

func enableCron() {
	existing, err := exec.Command("crontab", "-l").Output()
	if err != nil {
		printError("No crontab found. Run: awssso service --install")
		os.Exit(1)
	}
	content := string(existing)
	if !strings.Contains(content, "awssso refresh") {
		printError("No awssso cron entry found. Run: awssso service --install")
		os.Exit(1)
	}
	// Uncomment any commented-out awssso refresh lines
	updated := ""
	for _, line := range strings.Split(content, "\n") {
		if strings.HasPrefix(line, "#") && strings.Contains(line, "awssso refresh") {
			updated += strings.TrimPrefix(line, "#") + "\n"
		} else {
			updated += line + "\n"
		}
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(updated)
	if out, err := cmd.CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to enable cron entry: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh cron job enabled.")
}

func disableCron() {
	existing, err := exec.Command("crontab", "-l").Output()
	if err != nil || !strings.Contains(string(existing), "awssso refresh") {
		printError("No awssso cron entry found. Run: awssso service --install")
		os.Exit(1)
	}
	// Comment out the awssso refresh line instead of removing it
	updated := ""
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.Contains(line, "awssso refresh") && !strings.HasPrefix(line, "#") {
			updated += "#" + line + "\n"
		} else {
			updated += line + "\n"
		}
	}
	cmd := exec.Command("crontab", "-")
	cmd.Stdin = strings.NewReader(updated)
	if out, err := cmd.CombinedOutput(); err != nil {
		printError(fmt.Sprintf("Failed to disable cron entry: %v\n%s", err, out))
		os.Exit(1)
	}
	printSuccess("Auto-refresh cron job paused. Config preserved — run: awssso service --on to resume.")
}

func statusCron() {
	existing, err := exec.Command("crontab", "-l").Output()
	if err != nil || !strings.Contains(string(existing), "awssso refresh") {
		printWarning("Auto-refresh cron job is not installed.")
		printInfo("Run: awssso service --install")
		return
	}
	for _, line := range strings.Split(string(existing), "\n") {
		if strings.Contains(line, "awssso refresh") {
			printSuccess("Auto-refresh cron job is installed.")
			printInfo(fmt.Sprintf("Entry: %s", line))
			return
		}
	}
}

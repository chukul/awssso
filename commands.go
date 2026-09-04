package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso/types"
)

// tryAutoConfigureProfile attempts to configure a profile's account and role
// automatically by:
//  1. Finding an existing valid SSO token in any configured session.
//  2. Searching for an AWS account whose name matches the profile name.
//  3. If found and only one role is available, saving silently.
//     If multiple roles are available, showing only the role picker.
//
// Returns true if the profile was successfully configured, false if the caller
// should fall back to the full interactive switch.
func tryAutoConfigureProfile(ctx context.Context, profileName string, config *AWSConfig) bool {
	// Find any session with a valid token
	var validToken string
	var validRegion string
	var validSession string

	for sessName := range config.Sessions {
		mock := &AWSProfile{SSOSession: sessName}
		t, region, err := resolveValidToken(ctx, mock, config)
		if err == nil {
			validToken = t.AccessToken
			validRegion = region
			validSession = sessName
			break
		}
	}
	if validToken == "" {
		return false // no valid token — need full login
	}

	// Silently fetch all accounts
	spinner := NewSpinner("Looking up account for profile")
	spinner.Start()
	accounts, err := fetchAccounts(ctx, validRegion, validToken)
	if err != nil {
		spinner.Stop(false, "Could not fetch accounts")
		return false
	}
	spinner.Stop(true, fmt.Sprintf("Fetched %d accounts", len(accounts)))

	// Try to find an account whose name matches the profile name
	matched := findMatchingAccount(accounts, profileName)
	if matched == nil {
		printInfo(fmt.Sprintf("No account found matching %q — switching to account search.", profileName))
		fmt.Println()
		return false
	}

	printSuccess(fmt.Sprintf("Matched account: %s%s%s (%s)", Bold, *matched.AccountName, Reset, *matched.AccountId))

	// Fetch roles for the matched account
	roleSpinner := NewSpinner("Fetching roles")
	roleSpinner.Start()
	roles, err := fetchAccountRoles(ctx, validRegion, *matched.AccountId, validToken)
	if err != nil || len(roles) == 0 {
		roleSpinner.Stop(false, "Could not fetch roles")
		return false
	}
	roleSpinner.Stop(true, fmt.Sprintf("%d role(s) available", len(roles)))

	var roleName string
	if len(roles) == 1 {
		roleName = *roles[0].RoleName
		printInfo(fmt.Sprintf("Auto-selected role: %s%s%s", Bold, roleName, Reset))
	} else {
		// Show just the role list — no account search needed
		fmt.Println()
		printHeader("SELECT ROLE")
		for i, r := range roles {
			fmt.Printf("  %s[%d]%s %s\n", Cyan, i+1, Reset, Bold+*r.RoleName+Reset)
		}
		fmt.Println()
		for {
			text, ok := readlineInput(fmt.Sprintf("%s?%s Select a role %s(1-%d)%s or %sq%s to quit: ",
				Yellow, Reset, Dim, len(roles), Reset, Bold, Reset))
			if !ok || text == "q" || text == "quit" {
				return false
			}
			val, err := strconv.Atoi(text)
			if err == nil && val >= 1 && val <= len(roles) {
				roleName = *roles[val-1].RoleName
				break
			}
			printError(fmt.Sprintf("Enter a number between 1 and %d", len(roles)))
		}
	}

	// Determine the region from the session
	sess := config.Sessions[validSession]
	region := sess.SSORegion

	// Save the profile
	newProfile := &AWSProfile{
		Name:         profileName,
		SSOSession:   validSession,
		SSOAccountID: *matched.AccountId,
		SSORoleName:  roleName,
		Region:       region,
	}
	if err := writeAWSProfile(profileName, newProfile); err != nil {
		printError(fmt.Sprintf("Failed to save profile: %v", err))
		return false
	}
	printSuccess(fmt.Sprintf("Profile %q configured.", profileName))
	fmt.Println()
	return true
}

// findMatchingAccount searches for an account whose name closely matches the
// profile name. Tries exact match first, then prefix, then substring.
func findMatchingAccount(accounts []types.AccountInfo, profileName string) *types.AccountInfo {
	// Strip trailing role suffix (e.g. "_AWSAdministratorAccess") if present
	accountPart := profileName
	if idx := strings.LastIndex(profileName, "_"); idx > 0 {
		candidate := profileName[:idx]
		// Only strip if the part after _ looks like a role (has uppercase or is long)
		if len(profileName[idx+1:]) > 3 {
			accountPart = candidate
		}
	}
	lower := strings.ToLower(accountPart)

	// 1. Exact match
	for i, a := range accounts {
		if a.AccountName != nil && strings.EqualFold(*a.AccountName, accountPart) {
			return &accounts[i]
		}
	}
	// 2. Account name contains the profile part
	for i, a := range accounts {
		if a.AccountName != nil && strings.Contains(strings.ToLower(*a.AccountName), lower) {
			return &accounts[i]
		}
	}
	return nil
}

// orgDefaults returns the SSO Start URL and region from the first existing session
// in config, giving the user a sensible default when configuring a new profile.
func orgDefaults(config *AWSConfig) (url, region string) {
	for _, sess := range config.Sessions {
		if sess.SSOStartURL != "" {
			return sess.SSOStartURL, sess.SSORegion
		}
	}
	return "", ""
}

// isUnauthorized returns true when an AWS SDK error indicates a 401 / invalid token.
func isUnauthorized(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "401") ||
		strings.Contains(msg, "UnauthorizedException") ||
		strings.Contains(msg, "token not found") ||
		strings.Contains(msg, "token is invalid")
}

// resolveValidToken loads a cached SSO token for the given profile, refreshing it
// automatically if expired. Returns the valid token and the resolved SSO region.
// This eliminates the repeated load→read→check→refresh boilerplate across commands.
func resolveValidToken(ctx context.Context, profile *AWSProfile, config *AWSConfig) (*SSOToken, string, error) {
	cachePath, err := getSSOTokenPath(profile, config)
	if err != nil {
		return nil, "", fmt.Errorf("failed to resolve token path: %w", err)
	}

	token, err := readSSOToken(cachePath)
	if err != nil {
		return nil, "", fmt.Errorf("no cached SSO token found (run: awssso login --profile %s)", profile.Name)
	}

	ssoRegion := resolveSSORegion(profile, config, token)

	if token.IsExpired() {
		token, err = refreshToken(ctx, ssoRegion, cachePath, token)
		if err != nil {
			return nil, "", fmt.Errorf("failed to refresh expired token (run: awssso login --profile %s): %w", profile.Name, err)
		}
	}

	return token, ssoRegion, nil
}

// needsPrivateBrowser returns true if the profile's session requires a private browser.
// Logic:
//  1. sso_login_private = true → always private
//  2. sso_login_private = false → never private
//  3. Auto-detect: when multiple sessions share the same URL with different emails,
//     the FIRST session alphabetically is the "default" (no private needed).
//     All others get private mode.
func needsPrivateBrowser(config *AWSConfig, profile *AWSProfile) bool {
	if profile.SSOSession == "" {
		return false
	}

	sess, found := config.Sessions[profile.SSOSession]
	if !found {
		return false
	}

	// Explicit override
	if sess.LoginPrivate == "true" {
		return true
	}
	if sess.LoginPrivate == "false" {
		return false
	}

	// Auto-detect: find all sessions sharing the same start URL
	startURL := sess.SSOStartURL
	if startURL == "" {
		return false
	}

	type peer struct {
		name  string
		email string
	}
	var peers []peer
	for name, other := range config.Sessions {
		if other.SSOStartURL == startURL {
			peers = append(peers, peer{name: name, email: other.SSOAccountEmail})
		}
	}

	// Single session on this URL — no conflict
	if len(peers) <= 1 {
		return false
	}

	// Check if there are actually different emails
	hasConflict := false
	for _, p := range peers {
		if p.email != "" && p.email != sess.SSOAccountEmail {
			hasConflict = true
			break
		}
	}
	if !hasConflict {
		return false
	}

	// Multiple sessions with different emails — first alphabetically is the "default"
	slices.SortFunc(peers, func(a, b peer) int {
		if a.name < b.name {
			return -1
		}
		if a.name > b.name {
			return 1
		}
		return 0
	})

	// If this session is the first → default browser identity, no private needed
	return peers[0].name != profile.SSOSession
}

// getProfileName resolves the effective profile name from flag, env, or default.
func getProfileName(flagProfile string) string {
	if flagProfile != "" {
		return flagProfile
	}
	if envProfile := os.Getenv("AWS_PROFILE"); envProfile != "" {
		return envProfile
	}
	return "default"
}

// loadProfile looks up a profile by name and validates it has SSO config.
func loadProfile(config *AWSConfig, profileName string) (*AWSProfile, error) {
	profile, ok := config.Profiles[profileName]
	if !ok {
		printError(fmt.Sprintf("Profile %q not found in ~/.aws/config", profileName))
		suggestions := suggestProfiles(profileName, config, 3)
		if len(suggestions) > 0 {
			printInfo("Did you mean one of these?")
			for _, s := range suggestions {
				fmt.Fprintf(os.Stderr, "  • %s\n", s)
			}
		}
		return nil, fmt.Errorf("profile not found")
	}
	if profile.SSOAccountID == "" || profile.SSORoleName == "" {
		printError(fmt.Sprintf("Profile %q is missing account/role configuration", profileName))
		printInfo(fmt.Sprintf("Run: awssso create --profile %s to configure", profileName))
		return nil, fmt.Errorf("incomplete profile")
	}
	return profile, nil
}

// runLoginGroup logs in to every unique SSO session used by profiles in a group.
func runLoginGroup(group string, private bool) {
	members := profilesInGroup(group)
	if len(members) == 0 {
		printError(fmt.Sprintf("Group %q is empty or does not exist.", group))
		printInfo(fmt.Sprintf("Add profiles: awssso group %s --add <profile>", group))
		os.Exit(1)
	}

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}

	// Collect unique sessions used by group members
	type sessionEntry struct {
		name     string
		startURL string
		region   string
		email    string
	}
	seen := map[string]bool{}
	var sessions []sessionEntry

	for _, profileName := range members {
		p, ok := config.Profiles[profileName]
		if !ok {
			printWarning(fmt.Sprintf("Profile %q not found in config — skipped", profileName))
			continue
		}
		key := p.SSOSession
		if key == "" {
			key = p.SSOStartURL
		}
		if key == "" || seen[key] {
			continue
		}
		seen[key] = true

		entry := sessionEntry{}
		if p.SSOSession != "" {
			entry.name = p.SSOSession
			if sess, found := config.Sessions[p.SSOSession]; found {
				entry.startURL = sess.SSOStartURL
				entry.region = sess.SSORegion
				entry.email = sess.SSOAccountEmail
			}
		} else {
			entry.name = sessionNameFromURL(p.SSOStartURL)
			entry.startURL = p.SSOStartURL
			entry.region = p.SSORegion
		}
		sessions = append(sessions, entry)
	}

	if len(sessions) == 0 {
		printError("No SSO sessions found for profiles in this group.")
		os.Exit(1)
	}

	printHeader(fmt.Sprintf("LOGIN — GROUP: %s (%d session(s))", group, len(sessions)))

	for _, s := range sessions {
		printInfo(fmt.Sprintf("Logging in to session: %s%s%s", Bold, s.name, Reset))
		if s.email != "" {
			printInfo(fmt.Sprintf("Identity: %s%s%s", Magenta, s.email, Reset))
		}
		fmt.Println()

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)

		mockProfile := &AWSProfile{SSOSession: s.name}
		if s.name == sessionNameFromURL(s.startURL) {
			mockProfile = &AWSProfile{SSOStartURL: s.startURL}
		}
		cachePath, pathErr := getSSOTokenPath(mockProfile, config)
		if pathErr != nil {
			printError(fmt.Sprintf("Cannot resolve token path for %q: %v", s.name, pathErr))
			cancel()
			continue
		}

		usePrivate := private
		if !usePrivate {
			if sess, found := config.Sessions[s.name]; found && sess.LoginPrivate == "true" {
				usePrivate = true
			}
		}

		_, loginErr := loginSSOWithHint(ctx, s.startURL, s.region, cachePath, s.name, s.email, usePrivate)
		cancel()
		if loginErr != nil {
			printError(fmt.Sprintf("Login failed for %q: %v", s.name, loginErr))
		} else {
			printSuccess(fmt.Sprintf("Logged in to session %q", s.name))
		}
		fmt.Println()
	}
}

func runLogin(profileName string, sessionName string, private bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		printInfo("Ensure ~/.aws/config exists and is properly formatted")
		os.Exit(1)
	}

	// If --session is provided, login directly to that session (multi-identity support)
	if sessionName != "" {
		runLoginSession(ctx, config, sessionName, private)
		return
	}

	profileName = getProfileName(profileName)
	printInfo(fmt.Sprintf("Logging in with profile: %s%s%s", Bold, profileName, Reset))

	profile := getOrConfigureProfile(config, profileName)

	startURL := resolveStartURL(profile, config)
	ssoRegion := resolveSSORegion(profile, config, nil)

	if startURL == "" || ssoRegion == "" {
		printError(fmt.Sprintf("Profile %q is missing SSO configuration", profileName))
		printInfo("Required fields: sso_start_url and sso_region (or sso_session)")
		os.Exit(1)
	}

	cachePath, err := getSSOTokenPath(profile, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve token cache path: %v", err))
		os.Exit(1)
	}

	// Resolve email hint for multi-session awareness
	var emailHint string
	var resolvedSessionName string
	if profile.SSOSession != "" {
		resolvedSessionName = profile.SSOSession
		if session, found := config.Sessions[profile.SSOSession]; found {
			emailHint = session.SSOAccountEmail
		}
	}

	_, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, resolvedSessionName, emailHint, private)
	if err != nil {
		printError(fmt.Sprintf("Login failed: %v", err))
		printInfo("Check your SSO configuration and network connectivity")
		os.Exit(1)
	}

	printSuccess(fmt.Sprintf("Successfully logged in to profile %q!", profileName))
	printInfo("You can now use this profile with AWS CLI and other tools")
}

// runLoginSession handles login when targeting a specific SSO session directly.
func runLoginSession(ctx context.Context, config *AWSConfig, sessionName string, private bool) {
	session, found := config.Sessions[sessionName]
	if !found {
		printError(fmt.Sprintf("SSO session %q not found in ~/.aws/config", sessionName))
		printInfo("Available sessions:")
		for name := range config.Sessions {
			fmt.Fprintf(os.Stderr, "  • %s\n", name)
		}
		os.Exit(1)
	}

	printInfo(fmt.Sprintf("Logging in to SSO session: %s%s%s", Bold, sessionName, Reset))
	if session.SSOAccountEmail != "" {
		printInfo(fmt.Sprintf("Identity: %s%s%s", Bold+Magenta, session.SSOAccountEmail, Reset))
	}

	mockProfile := &AWSProfile{SSOSession: sessionName}
	cachePath, err := getSSOTokenPath(mockProfile, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve token cache path: %v", err))
		os.Exit(1)
	}

	_, err = loginSSOWithHint(ctx, session.SSOStartURL, session.SSORegion, cachePath, sessionName, session.SSOAccountEmail, private)
	if err != nil {
		printError(fmt.Sprintf("Login failed: %v", err))
		os.Exit(1)
	}

	printSuccess(fmt.Sprintf("Successfully logged in to session %q!", sessionName))

	// Show which profiles use this session
	var linkedProfiles []string
	for name, p := range config.Profiles {
		if p.SSOSession == sessionName {
			linkedProfiles = append(linkedProfiles, name)
		}
	}
	if len(linkedProfiles) > 0 {
		printInfo(fmt.Sprintf("Profiles using this session: %s", strings.Join(linkedProfiles, ", ")))
	}
}

func runCredential(profileName string) {
	profileName = getProfileName(profileName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	creds, profile, err := resolveCredentials(ctx, profileName, config)
	if err != nil {
		os.Exit(1)
	}

	_ = recordRecentProfile(profile)

	if err := json.NewEncoder(os.Stdout).Encode(creds); err != nil {
		printError(fmt.Sprintf("Failed to encode credentials: %v", err))
		os.Exit(1)
	}
}

// resolveCredentials gets temporary AWS credentials for a profile, automatically
// recovering from three common failures:
//
//  1. Missing account/role → runs switch to let the user configure it, then retries.
//  2. Expired or missing SSO token → re-authenticates automatically, then retries.
//  3. Account/role not found in AWS SSO → shows a clear error and offers to
//     reconfigure the profile with a different account/role.
func resolveCredentials(ctx context.Context, profileName string, config *AWSConfig) (*CredentialResponse, *AWSProfile, error) {
	// ── Step 1: load profile — auto-configure if account/role is missing ──────
	// Check directly (not via loadProfile) to avoid printing duplicate error messages
	// when we're about to auto-recover anyway.
	rawProfile, exists := config.Profiles[profileName]
	if !exists {
		printError(fmt.Sprintf("Profile %q not found in ~/.aws/config", profileName))
		suggestions := suggestProfiles(profileName, config, 3)
		if len(suggestions) > 0 {
			printInfo("Did you mean one of these?")
			for _, s := range suggestions {
				fmt.Fprintf(os.Stderr, "  • %s\n", s)
			}
		}
		return nil, nil, fmt.Errorf("profile not found")
	}

	if rawProfile.SSOAccountID == "" || rawProfile.SSORoleName == "" {
		printWarning(fmt.Sprintf("Profile %q has no account or role configured.", profileName))
		fmt.Println()

		// Ask before making any permanent change to the profile
		answer, _ := readlineInput(fmt.Sprintf("%s?%s Configure it now? (Y/n): ", Yellow, Reset))
		if strings.ToLower(strings.TrimSpace(answer)) == "n" {
			printInfo("Skipped. Run: awssso create --profile " + profileName)
			return nil, nil, fmt.Errorf("incomplete profile")
		}
		fmt.Println()

		// Try to auto-configure using an existing valid SSO token — avoids
		// prompting for URL/region and searching through hundreds of accounts.
		configured := tryAutoConfigureProfile(ctx, profileName, config)
		if !configured {
			// No valid token or no matching account found — fall back to the
			// full interactive switch (URL/region prompts + account search).
			printInfo("Starting full account/role setup...")
			fmt.Println()
			runSwitch(profileName, "", false, false)
		}

		// Reload config after configuration
		config, _ = loadAWSConfig()
		rawProfile = config.Profiles[profileName]
		if rawProfile == nil || rawProfile.SSOAccountID == "" || rawProfile.SSORoleName == "" {
			printError("Profile still incomplete after setup — please try again.")
			return nil, nil, fmt.Errorf("incomplete profile")
		}
	}

	profile, err := loadProfile(config, profileName)
	if err != nil {
		return nil, nil, err
	}

	// ── Step 2: get a valid token — auto-login if expired or missing ──────────
	token, ssoRegion, err := resolveValidToken(ctx, profile, config)
	if err != nil {
		printWarning("Session expired or not logged in — re-authenticating...")
		fmt.Println()

		startURL := resolveStartURL(profile, config)
		resolvedRegion := resolveSSORegion(profile, config, nil)
		cachePath, pathErr := getSSOTokenPath(profile, config)
		if pathErr != nil {
			printError(fmt.Sprintf("Cannot resolve token path: %v", pathErr))
			return nil, nil, pathErr
		}
		sessionName := profile.SSOSession
		emailHint := ""
		if sessionName != "" {
			if sess, found := config.Sessions[sessionName]; found {
				emailHint = sess.SSOAccountEmail
			}
		}
		usePrivate := needsPrivateBrowser(config, profile)

		token, err = loginSSOWithHint(ctx, startURL, resolvedRegion, cachePath, sessionName, emailHint, usePrivate)
		if err != nil {
			printError(fmt.Sprintf("Re-authentication failed: %v", err))
			return nil, nil, err
		}
		ssoRegion = resolvedRegion
		printSuccess("Re-authenticated successfully.")
		fmt.Println()
	}

	// ── Step 3: fetch credentials — handle account-not-found gracefully ───────
	creds, err := fetchRoleCredentials(ctx, ssoRegion, profile.SSOAccountID, profile.SSORoleName, token.AccessToken)
	if err != nil {
		var unauthErr *types.UnauthorizedException
		if errors.As(err, &unauthErr) {
			printError(fmt.Sprintf(
				"Account %s%s%s / role %s%s%s is no longer accessible in AWS SSO.",
				Bold, profile.SSOAccountID, Reset,
				Bold, profile.SSORoleName, Reset,
			))
			printWarning("The account or permission set may have been removed from your SSO access.")
			fmt.Println()
			answer, _ := readlineInput(fmt.Sprintf("%s?%s Reconfigure this profile with a different account/role? (y/N): ", Yellow, Reset))
			if strings.ToLower(answer) == "y" {
				fmt.Println()
				runSwitch(profileName, "", false)
				// After reconfiguring, the user can re-run the original command.
				printInfo("Profile updated. Re-run your command to use the new configuration.")
			}
			return nil, nil, err
		}
		if strings.Contains(err.Error(), "ExpiredToken") {
			printError("AWS credentials have expired mid-session. Re-run the command to re-authenticate.")
			return nil, nil, err
		}
		printError(fmt.Sprintf("Failed to fetch credentials: %v", err))
		return nil, nil, err
	}

	return creds, profile, nil
}

func runSwitch(profileName string, sessionName string, private bool, showNextSteps ...bool) {
	nextSteps := len(showNextSteps) == 0 || showNextSteps[0] // default true
	profileName = getProfileName(profileName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	// If --session is provided, use that session directly instead of resolving from profile.
	// This ensures the correct identity is used to fetch accounts (multi-session support).
	var profile *AWSProfile
	var resolvedSessionName string

	if sessionName != "" {
		session, found := config.Sessions[sessionName]
		if !found {
			printError(fmt.Sprintf("SSO session %q not found in ~/.aws/config", sessionName))
			printInfo("Available sessions:")
			for name := range config.Sessions {
				fmt.Fprintf(os.Stderr, "  • %s\n", name)
			}
			os.Exit(1)
		}
		resolvedSessionName = sessionName
		printInfo(fmt.Sprintf("Using SSO session: %s%s%s", Bold, sessionName, Reset))
		if session.SSOAccountEmail != "" {
			printInfo(fmt.Sprintf("Identity: %s%s%s", Bold+Magenta, session.SSOAccountEmail, Reset))
		}
		// Create a virtual profile from the session for login/token resolution
		profile = &AWSProfile{
			Name:       profileName,
			SSOSession: sessionName,
			Region:     session.SSORegion,
		}
	} else {
		profile = getOrConfigureProfile(config, profileName)
		resolvedSessionName = profile.SSOSession
	}

	cachePath, err := getSSOTokenPath(profile, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve token path: %v", err))
		os.Exit(1)
	}

	ssoRegion := resolveSSORegion(profile, config, nil)
	startURL := resolveStartURL(profile, config)

	// Resolve email hint for login
	var emailHint string
	if resolvedSessionName != "" {
		if session, found := config.Sessions[resolvedSessionName]; found {
			emailHint = session.SSOAccountEmail
		}
	}

	token, err := readSSOToken(cachePath)
	if err != nil || token.IsExpired() {
		if err == nil && token.RefreshToken != "" {
			printWarning("SSO token is expired. Attempting refresh...")
			token, err = refreshToken(ctx, ssoRegion, cachePath, token)
			if err == nil {
				printSuccess("SSO token refreshed successfully.")
			}
		}

		if err != nil || token == nil {
			printInfo("No valid SSO session found. Initiating interactive login...")
			token, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, resolvedSessionName, emailHint, private)
			if err != nil {
				printError(fmt.Sprintf("Login failed: %v", err))
				os.Exit(1)
			}
		}
	}

	accSpinner := NewSpinner("Fetching available AWS accounts")
	accSpinner.Start()
	accounts, err := fetchAccounts(ctx, ssoRegion, token.AccessToken)
	if err != nil {
		accSpinner.Stop(false, "Failed to fetch accounts")

		// Token rejected by AWS (401 / UnauthorizedException) — force re-login
		if isUnauthorized(err) {
			printWarning("SSO token is invalid or has been revoked — re-authenticating...")
			fmt.Println()
			token, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, resolvedSessionName, emailHint, private)
			if err != nil {
				printError(fmt.Sprintf("Login failed: %v", err))
				os.Exit(1)
			}
			// Retry with the fresh token
			retrySpinner := NewSpinner("Fetching available AWS accounts")
			retrySpinner.Start()
			accounts, err = fetchAccounts(ctx, ssoRegion, token.AccessToken)
			if err != nil {
				retrySpinner.Stop(false, "Failed to fetch accounts")
				printError(fmt.Sprintf("Error fetching accounts: %v", err))
				os.Exit(1)
			}
			retrySpinner.Stop(true, "Fetched available AWS accounts")
		} else {
			printError(fmt.Sprintf("Error fetching accounts: %v", err))
			os.Exit(1)
		}
	} else {
		accSpinner.Stop(true, "Fetched available AWS accounts")
	}

	if len(accounts) == 0 {
		printWarning("No AWS accounts found for this SSO session")
		printInfo("This may indicate a permissions issue with your SSO configuration")
		return
	}

	selectedAccount := selectAccount(accounts)
	acctID := *selectedAccount.AccountId
	acctName := *selectedAccount.AccountName

	roleSpinner := NewSpinner("Fetching roles for account " + acctName)
	roleSpinner.Start()
	roles, err := fetchAccountRoles(ctx, ssoRegion, acctID, token.AccessToken)
	if err != nil {
		roleSpinner.Stop(false, "Failed to fetch roles")
		printError(fmt.Sprintf("Error fetching roles: %v", err))
		os.Exit(1)
	}
	roleSpinner.Stop(true, "Fetched available roles")

	if len(roles) == 0 {
		printWarning(fmt.Sprintf("No roles found in account %s", acctName))
		printInfo("Contact your AWS administrator to request role access")
		return
	}

	printHeader("AVAILABLE ROLES")
	for i, role := range roles {
		roleName := ""
		if role.RoleName != nil {
			roleName = *role.RoleName
		}
		fmt.Printf("  %s[%d]%s %s\n", Cyan, i+1, Reset, Bold+roleName+Reset)
	}
	fmt.Println()

	var roleChoice int
	for {
		text, ok := readlineInput(fmt.Sprintf("%s?%s Select a role %s(1-%d)%s or %sq%s to quit: ",
			Yellow, Reset, Dim, len(roles), Reset, Bold, Reset))
		if !ok || text == "q" || text == "quit" || text == "exit" {
			printInfo("Canceled by user")
			os.Exit(0)
		}
		val, err := strconv.Atoi(text)
		if err == nil && val >= 1 && val <= len(roles) {
			roleChoice = val
			break
		}
		printError(fmt.Sprintf("Invalid input %q. Please enter a number between 1 and %d", text, len(roles)))
	}

	selectedRole := roles[roleChoice-1]
	roleName := *selectedRole.RoleName

	defaultProfileName := fmt.Sprintf("%s_%s", acctName, roleName)
	customProfileName, _ := readlineInput(fmt.Sprintf("%s?%s Enter profile name %s(press Enter for %q)%s: ",
		Yellow, Reset, Dim, defaultProfileName, Reset))
	if customProfileName == "" {
		customProfileName = defaultProfileName
		printInfo(fmt.Sprintf("Using profile name: %s", customProfileName))
	}

	newProfile := &AWSProfile{
		Name:         customProfileName,
		SSOAccountID: acctID,
		SSORoleName:  roleName,
		Region:       profile.Region,
	}

	if resolvedSessionName != "" {
		newProfile.SSOSession = resolvedSessionName
	} else if profile.SSOSession != "" {
		newProfile.SSOSession = profile.SSOSession
	} else {
		sessionName := sessionNameFromURL(profile.SSOStartURL)
		if err := writeSSOSession(sessionName, profile.SSOStartURL, profile.SSORegion); err != nil {
			printWarning(fmt.Sprintf("Failed to create sso-session section: %v", err))
			newProfile.SSOStartURL = profile.SSOStartURL
			newProfile.SSORegion = profile.SSORegion
		} else {
			oldPath, _ := getSSOTokenPath(&AWSProfile{SSOStartURL: profile.SSOStartURL}, config)
			newPath, _ := getSSOTokenPath(&AWSProfile{SSOSession: sessionName}, config)
			migrateSSOTokenCache(oldPath, newPath)
			newProfile.SSOSession = sessionName
		}
	}

	if err = writeAWSProfile(customProfileName, newProfile); err != nil {
		printError(fmt.Sprintf("Failed to save profile: %v", err))
		os.Exit(1)
	}

	printSuccess(fmt.Sprintf("Profile %q saved to ~/.aws/config!", customProfileName))
	writeActiveProfile(customProfileName)
	if nextSteps {
		runNextSteps(ctx, customProfileName, newProfile, config)
	}
}

// runNextSteps shows an interactive post-switch menu so the user can immediately
// activate the profile, export credentials, or open the AWS Console.
// Accepts a single option (1), multiple space- or comma-separated options (1 2),
// a range (1-3), or "all".
func runNextSteps(ctx context.Context, profileName string, profile *AWSProfile, config *AWSConfig) {
	var activateCmd string
	if runtime.GOOS == "windows" {
		activateCmd = fmt.Sprintf(`$env:AWS_PROFILE="%s"`, profileName)
	} else {
		activateCmd = fmt.Sprintf(`export AWS_PROFILE="%s"`, profileName)
	}

	printHeader("WHAT'S NEXT?")
	fmt.Printf("  %s[1]%s Activate profile in your shell\n", Cyan, Reset)
	fmt.Printf("     %s%s%s\n\n", Dim, activateCmd, Reset)
	fmt.Printf("  %s[2]%s Export AWS credentials  %s(for Terraform, Docker, etc.)%s\n\n", Cyan, Reset, Dim, Reset)
	fmt.Printf("  %s[3]%s Open AWS Console in browser\n\n", Cyan, Reset)

	input, ok := readlineInput(fmt.Sprintf("%s?%s Select %s(1  1 2  1-3  all)%s or Enter to skip: ",
		Yellow, Reset, Dim, Reset))
	if !ok || input == "" {
		return
	}

	var selected []int
	if input == "all" {
		selected = []int{1, 2, 3}
	} else {
		indices := parseSelection(input, 3)
		if len(indices) == 0 {
			printError(fmt.Sprintf("Invalid input %q. Use 1, 1 2, 1-3, or all.", input))
			return
		}
		for _, idx := range indices {
			selected = append(selected, idx+1) // parseSelection returns 0-based
		}
	}

	for _, opt := range selected {
		fmt.Println()
		switch opt {
		case 1:
			printInfo("Copy and run in your terminal:")
			fmt.Printf("\n  %s%s%s\n", Bold+Green, activateCmd, Reset)

		case 2:
			credCtx, credCancel := context.WithTimeout(context.Background(), 30*time.Second)
			spinner := NewSpinner("Fetching temporary credentials")
			spinner.Start()
			token, ssoRegion, err := resolveValidToken(credCtx, profile, config)
			if err != nil {
				credCancel()
				spinner.Stop(false, "Could not retrieve token")
				printError(fmt.Sprintf("Token expired — run: awssso login --profile %s", profileName))
				break
			}
			creds, err := fetchRoleCredentials(credCtx, ssoRegion, profile.SSOAccountID, profile.SSORoleName, token.AccessToken)
			credCancel()
			if err != nil {
				spinner.Stop(false, "Could not fetch credentials")
				printError(fmt.Sprintf("Failed: %v", err))
				break
			}
			spinner.Stop(true, "Credentials fetched")
			fmt.Println()
			fmt.Println(exportCredentials(creds, FormatEnv))

		case 3:
			runConsole(profileName)
		}
	}
}

// selectAccount handles the interactive account search and selection flow.
func selectAccount(accounts []types.AccountInfo) types.AccountInfo {
	printHeader("AVAILABLE AWS ACCOUNTS")
	fmt.Printf("  %sTotal:%s %d accounts found. Type to search by name or ID.\n\n", Dim, Reset, len(accounts))

	var filteredAccounts []types.AccountInfo
	for {
		searchTerm, ok := readlineInput(fmt.Sprintf("%s?%s Search accounts %s(Enter to list all, q to quit)%s: ",
			Yellow, Reset, Dim, Reset))
		if !ok {
			os.Exit(1)
		}
		if searchTerm == "q" || searchTerm == "quit" || searchTerm == "exit" {
			printInfo("Canceled by user")
			os.Exit(0)
		}

		filteredAccounts = filterAccounts(accounts, searchTerm)

		if len(filteredAccounts) == 0 {
			printError("No accounts match your search. Try a different term.")
			continue
		}
		if len(filteredAccounts) > 50 {
			printWarning(fmt.Sprintf("%d accounts match — too many to show. Narrow your search.", len(filteredAccounts)))
			continue
		}
		break
	}

	for i, acct := range filteredAccounts {
		name := ""
		if acct.AccountName != nil {
			name = *acct.AccountName
		}
		id := ""
		if acct.AccountId != nil {
			id = *acct.AccountId
		}
		fmt.Printf("  %s[%d]%s %s %s(%s)%s\n", Cyan, i+1, Reset, Bold+name+Reset, Dim, id, Reset)
	}
	fmt.Println()

	for {
		text, ok := readlineInput(fmt.Sprintf("%s?%s Select an account %s(1-%d)%s or %sq%s to quit: ",
			Yellow, Reset, Dim, len(filteredAccounts), Reset, Bold, Reset))
		if !ok {
			os.Exit(1)
		}
		if text == "q" || text == "quit" || text == "exit" {
			printInfo("Canceled by user")
			os.Exit(0)
		}
		val, err := strconv.Atoi(text)
		if err == nil && val >= 1 && val <= len(filteredAccounts) {
			return filteredAccounts[val-1]
		}
		printError(fmt.Sprintf("Invalid input %q. Please enter a number between 1 and %d", text, len(filteredAccounts)))
	}
}

func getOrConfigureProfile(config *AWSConfig, profileName string) *AWSProfile {
	profile, ok := config.Profiles[profileName]
	if ok {
		hasSSO := false
		if profile.SSOSession != "" {
			if session, found := config.Sessions[profile.SSOSession]; found {
				if session.SSOStartURL != "" && session.SSORegion != "" {
					hasSSO = true
				}
			}
		} else if profile.SSOStartURL != "" && profile.SSORegion != "" {
			hasSSO = true
		}
		if hasSSO {
			return profile
		}
	}

	printHeader("CONFIGURE SSO PROFILE")
	fmt.Printf("Profile %q needs SSO configuration. Let's set it up!\n\n", profileName)

	// Use an existing session's URL/region as default so the user only needs to press Enter
	defaultURL, defaultRegion := orgDefaults(config)

	var startURL string
	for {
		var prompt string
		if defaultURL != "" {
			prompt = fmt.Sprintf("%s?%s SSO Start URL %s(press Enter for %s)%s: ",
				Yellow, Reset, Dim, defaultURL, Reset)
		} else {
			prompt = fmt.Sprintf("%s?%s SSO Start URL %s(e.g. https://my-sso.awsapps.com/start/)%s: ",
				Yellow, Reset, Dim, Reset)
		}
		input, ok := readlineInput(prompt)
		if !ok {
			os.Exit(1)
		}
		if input == "" && defaultURL != "" {
			startURL = defaultURL
			printInfo(fmt.Sprintf("Using: %s", startURL))
		} else {
			startURL = input
		}
		if startURL == "" {
			printError("SSO Start URL cannot be empty")
			continue
		}
		if !strings.HasPrefix(startURL, "https://") {
			printWarning("SSO Start URL should begin with https://")
			confirm, _ := readlineInput(fmt.Sprintf("%s?%s Continue anyway? (y/N): ", Yellow, Reset))
			if strings.ToLower(confirm) != "y" && strings.ToLower(confirm) != "yes" {
				continue
			}
		}
		break
	}

	var ssoRegion string
	for {
		var prompt string
		if defaultRegion != "" {
			prompt = fmt.Sprintf("%s?%s SSO Region %s(press Enter for %s)%s: ",
				Yellow, Reset, Dim, defaultRegion, Reset)
		} else {
			prompt = fmt.Sprintf("%s?%s SSO Region %s(e.g. us-east-1, eu-west-1)%s: ",
				Yellow, Reset, Dim, Reset)
		}
		input, ok := readlineInput(prompt)
		if !ok {
			os.Exit(1)
		}
		if input == "" && defaultRegion != "" {
			ssoRegion = defaultRegion
			printInfo(fmt.Sprintf("Using: %s", ssoRegion))
		} else {
			ssoRegion = input
		}
		if ssoRegion == "" {
			printError("SSO Region cannot be empty")
			continue
		}
		break
	}

	awsRegion, _ := readlineInput(fmt.Sprintf("%s?%s AWS Default Region %s(press Enter for %s)%s: ",
		Yellow, Reset, Dim, ssoRegion, Reset))
	if awsRegion == "" {
		awsRegion = ssoRegion
		printInfo(fmt.Sprintf("Using region: %s", awsRegion))
	}

	sessionName := sessionNameFromURL(startURL)
	if err := writeSSOSession(sessionName, startURL, ssoRegion); err != nil {
		printWarning(fmt.Sprintf("Failed to create sso-session: %v", err))
	}

	newProfile := &AWSProfile{
		Name:       profileName,
		SSOSession: sessionName,
		Region:     awsRegion,
	}

	if err := writeAWSProfile(profileName, newProfile); err != nil {
		printError(fmt.Sprintf("Failed to write profile: %v", err))
		os.Exit(1)
	}

	printSuccess(fmt.Sprintf("Profile %q configured and saved to ~/.aws/config", profileName))
	return newProfile
}

func runConsole(profileName string) {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	// If no explicit --profile was given, show interactive profile picker.
	if profileName == "" {
		profileName = pickProfileForConsole(config, false)
		if profileName == "" {
			return
		}
	}

	profile, err := loadProfile(config, profileName)
	if err != nil {
		os.Exit(1)
	}

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	printHeader(fmt.Sprintf("AWS CONSOLE - PROFILE: %s", profileName))
	fmt.Printf("  %sAccount:%s %s\n", Dim, Reset, profile.SSOAccountID)
	fmt.Printf("  %sRole:%s    %s\n", Dim, Reset, profile.SSORoleName)
	fmt.Printf("  %sRegion:%s  %s\n\n", Dim, Reset, profile.Region)

	// Try to resolve token — if expired/missing, auto-login
	token, ssoRegion, err := resolveValidToken(ctx, profile, config)
	if err != nil {
		printWarning("Session expired or not logged in — initiating login...")

		sessionName := profile.SSOSession
		emailHint := ""
		if sessionName != "" {
			if sess, found := config.Sessions[sessionName]; found {
				emailHint = sess.SSOAccountEmail
			}
		}

		startURL := resolveStartURL(profile, config)
		ssoRegion = resolveSSORegion(profile, config, nil)
		cachePath, pathErr := getSSOTokenPath(profile, config)
		if pathErr != nil {
			printError(fmt.Sprintf("Failed to resolve token path: %v", pathErr))
			os.Exit(1)
		}

		// Use private mode only when this session's start URL is shared with other sessions
		// (multi-identity scenario). If it's the only session using the URL, default browser is fine.
		usePrivate := needsPrivateBrowser(config, profile)
		if usePrivate {
			printInfo(fmt.Sprintf("Multi-identity detected — opening private browser for: %s", emailHint))
		}

		token, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, sessionName, emailHint, usePrivate)
		if err != nil {
			printError(fmt.Sprintf("Login failed: %v", err))
			os.Exit(1)
		}
		printSuccess("Logged in successfully!")
		fmt.Println()
	}

	credSpinner := NewSpinner("Fetching temporary credentials")
	credSpinner.Start()
	creds, err := fetchRoleCredentials(ctx, ssoRegion, profile.SSOAccountID, profile.SSORoleName, token.AccessToken)
	if err != nil {
		credSpinner.Stop(false, "Failed to fetch credentials")
		printError(fmt.Sprintf("Error: %v", err))
		os.Exit(1)
	}
	credSpinner.Stop(true, "Credentials obtained")

	_ = recordRecentProfile(profile)

	if err = openAWSConsole(ctx, profile.Region, creds); err != nil {
		printError(fmt.Sprintf("Failed to open console: %v", err))
		os.Exit(1)
	}
}

// pickProfileForConsole shows a grouped, interactive profile picker.
// When activeOnly is true only profiles with an Active SSO token are shown —
// used by export so users are never offered a profile that needs login first.
// Returns the selected profile name, or empty string if cancelled.
func pickProfileForConsole(config *AWSConfig, activeOnly bool) string {
	type profileRow struct {
		name      string
		id        string
		role      string
		region    string
		env       string
		session   string
		status    string
		remaining string
	}

	var allRows []profileRow
	for name, p := range config.Profiles {
		session := p.SSOSession
		if session == "" && p.SSOStartURL != "" {
			session = "(inline)"
		}

		status, remaining := profileTokenStatus(p, config)

		allRows = append(allRows, profileRow{
			name:      name,
			id:        p.SSOAccountID,
			role:      p.SSORoleName,
			region:    p.Region,
			env:       detectEnvironment(p),
			session:   session,
			status:    status,
			remaining: remaining,
		})
	}

	// For credential export: drop everything that isn't immediately usable.
	if activeOnly {
		var active []profileRow
		for _, r := range allRows {
			if r.status == "Active" {
				active = append(active, r)
			}
		}
		if len(active) == 0 {
			printWarning("No active sessions found.")
			printInfo("Run 'awssso login' or 'awssso refresh' to authenticate first.")
			return ""
		}
		allRows = active
	}

	if len(allRows) == 0 {
		printWarning("No AWS profiles with SSO configuration found")
		printInfo("Run 'awssso create' to create one")
		return ""
	}

	// Group by session
	type sessionGroup struct {
		session string
		email   string
		status  string
		rows    []profileRow
	}
	groupMap := make(map[string]*sessionGroup)
	groupOrder := []string{}

	for _, r := range allRows {
		key := r.session
		if key == "" {
			key = "(no session)"
		}
		if _, exists := groupMap[key]; !exists {
			email := ""
			if sess, found := config.Sessions[key]; found {
				email = sess.SSOAccountEmail
			}
			groupMap[key] = &sessionGroup{session: key, email: email}
			groupOrder = append(groupOrder, key)
		}
		groupMap[key].rows = append(groupMap[key].rows, r)
	}

	// Sort groups
	slices.SortFunc(groupOrder, func(a, b string) int {
		aSpecial := strings.HasPrefix(a, "(")
		bSpecial := strings.HasPrefix(b, "(")
		if aSpecial != bSpecial {
			if aSpecial {
				return 1
			}
			return -1
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})

	// Sort profiles within each group by environment priority
	envPriority := map[string]int{"production": 0, "staging": 1, "oat": 2, "development": 3, "unknown": 4}
	for _, g := range groupMap {
		slices.SortFunc(g.rows, func(a, b profileRow) int {
			pa, pb := envPriority[a.env], envPriority[b.env]
			if pa != pb {
				return pa - pb
			}
			if a.name < b.name {
				return -1
			}
			if a.name > b.name {
				return 1
			}
			return 0
		})
	}

	// Determine session-level status
	for _, key := range groupOrder {
		g := groupMap[key]
		best := "No SSO"
		for _, r := range g.rows {
			if r.status == "Active" {
				best = "Active"
				break
			} else if r.status == "Expired" && best != "Active" {
				best = "Expired"
			} else if r.status == "Not Logged In" && best == "No SSO" {
				best = "Not Logged In"
			}
		}
		g.status = best
	}

	// Display picker
	printHeader("SELECT PROFILE")
	fmt.Printf("  %sExpired sessions auto-login. %s[No SSO]%s profiles auto-configure when selected.%s\n", Dim, Yellow, Dim, Reset)

	globalIdx := 0
	flatRows := []profileRow{}

	for _, key := range groupOrder {
		g := groupMap[key]

		sessionStatusColor := Dim
		switch g.status {
		case "Active":
			sessionStatusColor = Green
		case "Expired":
			sessionStatusColor = Yellow
		case "Not Logged In":
			sessionStatusColor = Red
		}

		sessionLabel := g.session
		if g.email != "" {
			sessionLabel = fmt.Sprintf("%s %s(%s)%s", g.session, Magenta, g.email, Reset)
		}
		fmt.Printf("\n  %s┌─ Session: %s%s  [%s%s%s]%s\n",
			Dim, Reset+Bold, sessionLabel, sessionStatusColor, g.status, Reset, Reset)

		for _, r := range g.rows {
			globalIdx++
			flatRows = append(flatRows, r)

			envSymbol := ""
			if r.env != "unknown" {
				envSymbol = getEnvironmentSymbol(r.env) + " "
			}

			statusColor := Dim
			switch r.status {
			case "Active":
				statusColor = Green
			case "Expired":
				statusColor = Yellow
			case "Not Logged In":
				statusColor = Red
			}

			remainingStr := ""
			if r.remaining != "" {
				remainingStr = fmt.Sprintf(" %s%s%s", Dim, r.remaining, Reset)
			}

			fmt.Printf("  %s│%s   %s[%d]%s %s%-26s%s %s[%s%s%s]%s%s\n",
				Dim, Reset,
				Cyan, globalIdx, Reset,
				envSymbol,
				Bold+r.name+Reset, "",
				Dim, statusColor, r.status, Reset, Dim,
				remainingStr)
			if r.id != "" {
				fmt.Printf("  %s│%s        %s%s / %s (%s)%s\n",
					Dim, Reset, Dim, r.id, r.role, r.region, Reset)
			}
		}
		fmt.Printf("  %s└─%s\n", Dim, Reset)
	}

	fmt.Println()

	for {
		text, ok := readlineInput(fmt.Sprintf("%s?%s Select profile %s(1-%d)%s or %sq%s to quit: ",
			Yellow, Reset, Dim, len(flatRows), Reset, Bold, Reset))
		if !ok || text == "" || text == "q" || text == "quit" || text == "exit" {
			printInfo("Canceled")
			return ""
		}
		val, err := strconv.Atoi(text)
		if err == nil && val >= 1 && val <= len(flatRows) {
			return flatRows[val-1].name
		}
		printError(fmt.Sprintf("Invalid input %q. Enter a number between 1 and %d", text, len(flatRows)))
	}
}

func runRefresh(profileName string, sessionName string, extraSessions []string, force bool, private bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	switch {
	case len(extraSessions) > 0:
		// Positional args: awssso refresh session-a session-b session-c
		refreshMultipleSessions(ctx, config, extraSessions, force, private)
	case sessionName != "":
		refreshSingleSession(ctx, config, sessionName, force, private)
	case profileName != "":
		refreshSingleProfile(ctx, config, profileName, force, private)
	default:
		// No args: show interactive picker
		runRefreshPicker(ctx, config, force, private)
	}
}

// runRefreshPicker shows a numbered list of sessions and lets the user select
// multiple by number (e.g. "1 2 3", "1,3", "1-3", or "all").
func runRefreshPicker(ctx context.Context, config *AWSConfig, force bool, private bool) {
	if len(config.Sessions) == 0 {
		printWarning("No SSO sessions found in ~/.aws/config")
		printInfo("Run 'awssso create' or 'awssso login' to create one")
		return
	}

	type row struct {
		name      string
		startURL  string
		region    string
		status    string
		remaining string
	}

	var rows []row
	for name, sess := range config.Sessions {
		mock := &AWSProfile{SSOSession: name}
		status, remaining := profileTokenStatus(mock, config)
		rows = append(rows, row{
			name:     name,
			startURL: sess.SSOStartURL,
			region:   sess.SSORegion,
			status:   status,
			remaining: remaining,
		})
	}

	// Sort: expired first, then by name
	slices.SortFunc(rows, func(a, b row) int {
		if a.status == "Expired" && b.status != "Expired" {
			return -1
		}
		if a.status != "Expired" && b.status == "Expired" {
			return 1
		}
		if a.name < b.name {
			return -1
		}
		return 1
	})

	printHeader(fmt.Sprintf("SELECT SESSIONS TO REFRESH (%d)", len(rows)))
	fmt.Printf("  %sSupports: single (1), multiple (1 2 3 or 1,2,3), ranges (1-3), or all%s\n\n", Dim, Reset)

	for i, r := range rows {
		statusColor := Green
		switch r.status {
		case "Expired":
			statusColor = Yellow
		case "Not Logged In":
			statusColor = Red
		}
		remainingStr := ""
		if r.remaining != "" {
			remainingStr = fmt.Sprintf("  %s%s%s", Dim, r.remaining, Reset)
		}
		fmt.Printf("  %s[%d]%s %-30s %s%s%s%s\n",
			Cyan, i+1, Reset,
			Bold+r.name+Reset,
			statusColor, r.status, Reset,
			remainingStr)
	}
	fmt.Println()

	var selected []string

	for {
		text, ok := readlineInput(fmt.Sprintf("%s?%s Select sessions %s(1-%d, 1 2 3, 1-3, or all)%s: ",
			Yellow, Reset, Dim, len(rows), Reset))
		if !ok || text == "" || text == "q" || text == "quit" {
			printInfo("Canceled")
			return
		}
		if text == "all" {
			for _, r := range rows {
				selected = append(selected, r.name)
			}
			break
		}
		indices := parseSelection(text, len(rows))
		if len(indices) == 0 {
			printError("Invalid input. Use numbers like 1 2 3, 1,3, or 1-3")
			continue
		}
		for _, idx := range indices {
			selected = append(selected, rows[idx].name)
		}
		break
	}

	fmt.Println()
	refreshMultipleSessions(ctx, config, selected, force, private)
}

// refreshMultipleSessions refreshes a list of sessions by name, running them in parallel.
func refreshMultipleSessions(ctx context.Context, config *AWSConfig, sessionNames []string, force bool, private bool) {
	// Validate all names first
	var valid []string
	for _, name := range sessionNames {
		if _, found := config.Sessions[name]; !found {
			printError(fmt.Sprintf("Session %q not found in ~/.aws/config", name))
			suggestions := []string{}
			for k := range config.Sessions {
				suggestions = append(suggestions, k)
			}
			if len(suggestions) > 0 {
				printInfo(fmt.Sprintf("Available sessions: %s", strings.Join(suggestions, ", ")))
			}
			continue
		}
		valid = append(valid, name)
	}

	if len(valid) == 0 {
		os.Exit(1)
	}

	printHeader(fmt.Sprintf("REFRESHING %d SESSION(S)", len(valid)))

	var wg sync.WaitGroup
	results := make(chan string, len(valid))

	for _, name := range valid {
		wg.Add(1)
		go func(sessionName string) {
			defer wg.Done()
			session := config.Sessions[sessionName]
			mockProfile := &AWSProfile{SSOSession: sessionName}
			cachePath, err := getSSOTokenPath(mockProfile, config)
			if err != nil {
				results <- fmt.Sprintf("✘ %-28s  failed to resolve token path: %v", sessionName, err)
				return
			}

			token, err := readSSOToken(cachePath)
			if err != nil {
				results <- fmt.Sprintf("✘ %-28s  no cached token — run: awssso login --session %s", sessionName, sessionName)
				return
			}

			if !token.IsExpired() && !force {
				exp, _ := time.Parse(time.RFC3339, token.ExpiresAt)
				results <- fmt.Sprintf("✔ %-28s  already valid (%s remaining)", sessionName, formatDuration(time.Until(exp)))
				return
			}

			if token.RefreshToken != "" {
				_, err = refreshToken(ctx, session.SSORegion, cachePath, token)
				if err != nil {
					results <- fmt.Sprintf("✘ %-28s  refresh failed: %v", sessionName, err)
				} else {
					results <- fmt.Sprintf("✔ %-28s  refreshed via OIDC", sessionName)
				}
				return
			}

			// Need browser login
			usePrivate := private || needsPrivateBrowser(config, mockProfile)
			_, err = loginSSOWithHint(ctx, session.SSOStartURL, session.SSORegion, cachePath, sessionName, session.SSOAccountEmail, usePrivate)
			if err != nil {
				results <- fmt.Sprintf("✘ %-28s  login failed: %v", sessionName, err)
			} else {
				results <- fmt.Sprintf("✔ %-28s  refreshed via login", sessionName)
			}
		}(name)
	}

	wg.Wait()
	close(results)

	for r := range results {
		if strings.HasPrefix(r, "✔") {
			fmt.Printf("  %s%s%s\n", Green, r, Reset)
		} else {
			fmt.Printf("  %s%s%s\n", Red, r, Reset)
		}
	}
}

// refreshSingleSession refreshes a specific SSO session by name (multi-identity support).
func refreshSingleSession(ctx context.Context, config *AWSConfig, sessionName string, force bool, private bool) {
	session, found := config.Sessions[sessionName]
	if !found {
		printError(fmt.Sprintf("SSO session %q not found in ~/.aws/config", sessionName))
		os.Exit(1)
	}

	mockProfile := &AWSProfile{SSOSession: sessionName}
	cachePath, err := getSSOTokenPath(mockProfile, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve token path: %v", err))
		os.Exit(1)
	}

	token, err := readSSOToken(cachePath)
	if err != nil {
		printError(fmt.Sprintf("No cached token found for session %q - run login first", sessionName))
		printInfo(fmt.Sprintf("Run: awssso login --session %s", sessionName))
		os.Exit(1)
	}

	if token.RefreshToken != "" {
		if !token.IsExpired() && !force {
			printSuccess(fmt.Sprintf("Token for session %q is still valid (use --force to refresh anyway)", sessionName))
			return
		}
		spinner := NewSpinner(fmt.Sprintf("Refreshing token for session %q", sessionName))
		spinner.Start()
		_, err = refreshToken(ctx, session.SSORegion, cachePath, token)
		if err != nil {
			spinner.Stop(false, "Failed to refresh token")
			printError(err.Error())
			os.Exit(1)
		}
		spinner.Stop(true, fmt.Sprintf("Refreshed token for session %q", sessionName))
		return
	}

	if !token.IsExpired() && !force {
		printSuccess(fmt.Sprintf("Token for session %q is still valid (no refresh token available)", sessionName))
		return
	}

	// Auto-detect private mode, allow --private as override
	usePrivate := private || needsPrivateBrowser(config, mockProfile)

	printInfo(fmt.Sprintf("No refresh token available for session %q - initiating device authorization...", sessionName))
	_, err = loginSSOWithHint(ctx, session.SSOStartURL, session.SSORegion, cachePath, sessionName, session.SSOAccountEmail, usePrivate)
	if err != nil {
		printError(fmt.Sprintf("Login failed: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("Successfully refreshed token for session %q via login", sessionName))
}

func refreshSingleProfile(ctx context.Context, config *AWSConfig, profileName string, force bool, private bool) {
	profile, err := loadProfile(config, profileName)
	if err != nil {
		os.Exit(1)
	}

	cachePath, err := getSSOTokenPath(profile, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve token path: %v", err))
		os.Exit(1)
	}

	token, err := readSSOToken(cachePath)
	if err != nil {
		printError(fmt.Sprintf("No cached token found for profile %q - run login first", profileName))
		os.Exit(1)
	}

	ssoRegion := resolveSSORegion(profile, config, token)
	startURL := resolveStartURL(profile, config)

	if token.RefreshToken != "" {
		if !token.IsExpired() && !force {
			printSuccess(fmt.Sprintf("Token for profile %q is still valid (use --force to refresh anyway)", profileName))
			return
		}
		spinner := NewSpinner(fmt.Sprintf("Refreshing token for profile %q", profileName))
		spinner.Start()
		_, err = refreshToken(ctx, ssoRegion, cachePath, token)
		if err != nil {
			spinner.Stop(false, "Failed to refresh token")
			printError(err.Error())
			os.Exit(1)
		}
		spinner.Stop(true, fmt.Sprintf("Refreshed token for profile %q", profileName))
		return
	}

	if !token.IsExpired() && !force {
		printSuccess(fmt.Sprintf("Token for profile %q is still valid (no refresh token available for proactive refresh)", profileName))
		return
	}

	// Auto-detect private mode, allow --private as override
	usePrivate := private || needsPrivateBrowser(config, profile)

	// Resolve session name and email for identity hints
	sessionName := profile.SSOSession
	emailHint := ""
	if sessionName != "" {
		if sess, found := config.Sessions[sessionName]; found {
			emailHint = sess.SSOAccountEmail
		}
	}

	printInfo(fmt.Sprintf("No refresh token available for profile %q - initiating device authorization...", profileName))
	_, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, sessionName, emailHint, usePrivate)
	if err != nil {
		printError(fmt.Sprintf("Login failed: %v", err))
		os.Exit(1)
	}
	printSuccess(fmt.Sprintf("Successfully refreshed token for profile %q via login", profileName))
}

func refreshAllSessions(ctx context.Context, config *AWSConfig, force bool, private bool) {
	type sessEntry struct {
		item     *SessionItem
		profiles []string
	}
	statusMap := make(map[string]*sessEntry)
	for _, profile := range config.Profiles {
		var sessionKey string
		var startURL string
		var ssoRegion string

		if profile.SSOSession != "" {
			sessionKey = profile.SSOSession
			if session, found := config.Sessions[profile.SSOSession]; found {
				startURL = session.SSOStartURL
				ssoRegion = session.SSORegion
			}
		} else if profile.SSOStartURL != "" {
			sessionKey = profile.SSOStartURL
			startURL = profile.SSOStartURL
			ssoRegion = profile.SSORegion
		} else {
			continue
		}

		if _, ok := statusMap[sessionKey]; !ok {
			statusMap[sessionKey] = &sessEntry{
				item: &SessionItem{
					Name:     sessionKey,
					StartURL: startURL,
					Region:   ssoRegion,
				},
			}
		}
		statusMap[sessionKey].profiles = append(statusMap[sessionKey].profiles, profile.Name)
	}

	if len(statusMap) == 0 {
		printWarning("No AWS SSO sessions found in configuration")
		return
	}

	for _, se := range statusMap {
		s := se.item
		mockProfile := &AWSProfile{}
		if strings.HasPrefix(s.Name, "http") {
			mockProfile.SSOStartURL = s.Name
		} else {
			mockProfile.SSOSession = s.Name
		}
		tokenPath, err := getSSOTokenPath(mockProfile, config)
		if err == nil {
			s.TokenPath = tokenPath
			token, err := readSSOToken(tokenPath)
			if err == nil {
				s.Token = token
				s.HasCache = true
				s.IsExpired = token.IsExpired()
			}
		}
	}

	// Fix #6: Deduplicate sessions before refreshing to avoid file write races.
	// Each session is unique in statusMap by key, so parallel refresh is safe per-session.
	var toOIDCRefresh []*sessEntry
	var toLogin []*sessEntry
	for _, se := range statusMap {
		s := se.item
		if s.Token == nil {
			toLogin = append(toLogin, se)
			continue
		}
		needsRefresh := force || s.IsExpired
		if !needsRefresh {
			continue
		}
		if s.Token.RefreshToken != "" {
			toOIDCRefresh = append(toOIDCRefresh, se)
		} else {
			toLogin = append(toLogin, se)
		}
	}

	if len(toOIDCRefresh) > 0 {
		// Fix #9: Typo "OIDc" -> "OIDC"
		printHeader(fmt.Sprintf("REFRESHING %d SESSION(S) VIA OIDC", len(toOIDCRefresh)))
		var wg sync.WaitGroup
		results := make(chan string, len(toOIDCRefresh))
		for _, se := range toOIDCRefresh {
			wg.Add(1)
			go func(session *SessionItem) {
				defer wg.Done()
				_, err := refreshToken(ctx, session.Region, session.TokenPath, session.Token)
				if err != nil {
					results <- fmt.Sprintf("✘ %s: %v", shortName(session.Name), err)
				} else {
					results <- fmt.Sprintf("✔ %s: refreshed successfully", shortName(session.Name))
				}
			}(se.item)
		}
		wg.Wait()
		close(results)
		for r := range results {
			if strings.HasPrefix(r, "✔") {
				fmt.Printf("  %s%s%s\n", Green, r, Reset)
			} else {
				fmt.Printf("  %s%s%s\n", Red, r, Reset)
			}
		}
	}

	if len(toLogin) > 0 {
		printHeader(fmt.Sprintf("LOGGING IN %d SESSION(S) VIA DEVICE AUTHORIZATION", len(toLogin)))
		for _, se := range toLogin {
			s := se.item
			// Resolve email hint for multi-identity awareness
			emailHint := ""
			if session, found := config.Sessions[s.Name]; found {
				emailHint = session.SSOAccountEmail
			}

			// Auto-detect private mode per-session, allow --private as override
			mockProfile := &AWSProfile{SSOSession: s.Name}
			usePrivate := private || needsPrivateBrowser(config, mockProfile)

			printInfo(fmt.Sprintf("Opening browser for session %q...", shortName(s.Name)))
			if emailHint != "" {
				printInfo(fmt.Sprintf("  Identity: %s", emailHint))
			}
			if usePrivate {
				printInfo("  Mode: private/incognito")
			}
			_, err := loginSSOWithHint(ctx, s.StartURL, s.Region, s.TokenPath, s.Name, emailHint, usePrivate)
			if err != nil {
				printError(fmt.Sprintf("Login failed for %q: %v", shortName(s.Name), err))
			} else {
				printSuccess(fmt.Sprintf("Successfully logged in session %q", shortName(s.Name)))
			}
		}
	}

	if len(toOIDCRefresh) == 0 && len(toLogin) == 0 {
		valid := 0
		notLogged := 0
		for _, se := range statusMap {
			s := se.item
			if s.HasCache && !s.IsExpired {
				valid++
			} else if !s.HasCache {
				notLogged++
			}
		}
		if valid > 0 {
			printSuccess(fmt.Sprintf("All %d session(s) are valid (%d not logged in)", valid, notLogged))
		} else {
			printSuccess("No expired sessions to refresh")
		}
	}
}

func runWhoami(profileName string) {
	profileName = getProfileName(profileName)

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	profile, ok := config.Profiles[profileName]
	if !ok {
		printError(fmt.Sprintf("Profile %q not found", profileName))
		os.Exit(1)
	}

	env := detectEnvironment(profile)
	envColor := getEnvironmentColor(env)
	envSymbol := getEnvironmentSymbol(env)

	printHeader("CURRENT AWS IDENTITY")

	fmt.Printf("  %sProfile:%s       %s%s%s\n", Dim, Reset, Bold, profileName, Reset)
	fmt.Printf("  %sEnvironment:%s  %s %s%s%s\n", Dim, Reset, envSymbol, envColor, strings.ToUpper(env), Reset)
	fmt.Printf("  %sAccount ID:%s   %s\n", Dim, Reset, profile.SSOAccountID)
	fmt.Printf("  %sRole:%s         %s\n", Dim, Reset, profile.SSORoleName)
	fmt.Printf("  %sRegion:%s       %s\n", Dim, Reset, profile.Region)

	cachePath, err := getSSOTokenPath(profile, config)
	if err == nil {
		token, tokenErr := readSSOToken(cachePath)
		if tokenErr == nil {
			fmt.Println()
			if token.IsExpired() {
				// Auto-refresh silently if a refresh token is available
				ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
				defer cancel()
				ssoRegion := resolveSSORegion(profile, config, token)
				refreshed, refreshErr := refreshToken(ctx, ssoRegion, cachePath, token)
				if refreshErr == nil {
					token = refreshed
					expiry, _ := time.Parse(time.RFC3339, token.ExpiresAt)
					remaining := formatDuration(time.Until(expiry))
					printSuccess(fmt.Sprintf("SSO Token: Refreshed (%s remaining)", remaining))
				} else {
					printWarning("SSO Token: EXPIRED — run: awssso login")
				}
			} else {
				expiry, _ := time.Parse(time.RFC3339, token.ExpiresAt)
				remaining := formatDuration(time.Until(expiry))
				printSuccess(fmt.Sprintf("SSO Token: Valid (%s remaining)", remaining))
			}
		}
	}

	fmt.Println()
	printHeader("USAGE")
	if runtime.GOOS == "windows" {
		fmt.Printf("  %sPowerShell:%s\n", Bold, Reset)
		fmt.Printf("    %s$env:AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
		fmt.Printf("\n  %sBash/Zsh:%s\n", Bold, Reset)
		fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
	} else {
		fmt.Printf("  %sBash/Zsh:%s\n", Bold, Reset)
		fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
	}
	fmt.Println()
}

// runProfilesFiltered shows profiles filtered by group tag. With no group, shows all.
func runProfilesFiltered(group string) {
	if group == "" {
		runProfiles()
		return
	}
	members := profilesInGroup(group)
	if len(members) == 0 {
		printWarning(fmt.Sprintf("No profiles tagged %q.", group))
		printInfo(fmt.Sprintf("Add a profile to this group: awssso group <profile> %s", group))
		return
	}

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}

	printHeader(fmt.Sprintf("GROUP: %s (%d profiles)", group, len(members)))
	for i, name := range members {
		p, ok := config.Profiles[name]
		if !ok {
			continue
		}
		env := detectEnvironment(p)
		symbol := getEnvironmentSymbol(env)
		status, remaining := profileTokenStatus(p, config)
		statusColor := tokenStatusColor(status)
		remainingStr := ""
		if remaining != "" {
			remainingStr = fmt.Sprintf(" %s%s%s", Dim, remaining, Reset)
		}
		fmt.Printf("  %s[%d]%s %s %s%-28s%s %s[%s%s%s]%s%s\n",
			Cyan, i+1, Reset, symbol,
			Bold, name, Reset,
			Dim, statusColor, status, Reset, Dim,
			remainingStr)
	}
	fmt.Println()

	text, ok := readlineInput(fmt.Sprintf("%s?%s Activate a profile %s(1-%d)%s or Enter to skip: ",
		Yellow, Reset, Dim, len(members), Reset))
	if !ok || text == "" {
		return
	}
	val, err := strconv.Atoi(text)
	if err != nil || val < 1 || val > len(members) {
		printError(fmt.Sprintf("Enter a number between 1 and %d", len(members)))
		return
	}
	selectedName := members[val-1]
	profile := config.Profiles[selectedName]
	if !showProductionWarning(profile) {
		return
	}
	os.Setenv("AWS_PROFILE", selectedName)
	writeActiveProfile(selectedName)
	setProfileScript(selectedName)
}

// profileTokenStatus returns the token status string and remaining time for a profile.
func profileTokenStatus(p *AWSProfile, config *AWSConfig) (status, remaining string) {
	if p.SSOSession == "" && p.SSOStartURL == "" {
		return "No SSO", ""
	}
	mock := &AWSProfile{}
	if p.SSOSession != "" {
		mock.SSOSession = p.SSOSession
	} else {
		mock.SSOStartURL = p.SSOStartURL
	}
	tokenPath, err := getSSOTokenPath(mock, config)
	if err != nil {
		return "No SSO", ""
	}
	token, err := readSSOToken(tokenPath)
	if err != nil {
		return "Not Logged In", ""
	}
	if token.IsExpired() {
		exp, _ := time.Parse(time.RFC3339, token.ExpiresAt)
		return "Expired", fmt.Sprintf("expired %s ago", formatDuration(time.Since(exp)))
	}
	exp, _ := time.Parse(time.RFC3339, token.ExpiresAt)
	return "Active", fmt.Sprintf("%s remaining", formatDuration(time.Until(exp)))
}

func tokenStatusColor(status string) string {
	switch status {
	case "Active":
		return Green
	case "Expired":
		return Yellow
	default:
		return Red
	}
}

func runProfiles() {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	if len(config.Profiles) == 0 {
		printWarning("No AWS profiles found in ~/.aws/config")
		printInfo("Run 'awssso create' to create one")
		return
	}

	printHeader(fmt.Sprintf("AWS PROFILES (%d)", len(config.Profiles)))
	fmt.Printf("  %sSelect a profile to activate. Expired sessions will auto-login.%s\n", Dim, Reset)

	type profileRow struct {
		name      string
		id        string
		role      string
		region    string
		env       string
		status    string
		remaining string
		session   string
	}

	// Build profile rows with status
	allRows := []profileRow{}
	for name, p := range config.Profiles {
		env := detectEnvironment(p)
		session := p.SSOSession
		if session == "" && p.SSOStartURL != "" {
			session = "(inline)"
		}
		status, remaining := profileTokenStatus(p, config)

		allRows = append(allRows, profileRow{
			name:      name,
			id:        p.SSOAccountID,
			role:      p.SSORoleName,
			region:    p.Region,
			env:       env,
			status:    status,
			remaining: remaining,
			session:   session,
		})
	}

	// Group profiles by session
	type sessionGroup struct {
		session string
		email   string
		rows    []profileRow
		status  string
	}
	groupMap := make(map[string]*sessionGroup)
	groupOrder := []string{}

	for _, r := range allRows {
		key := r.session
		if key == "" {
			key = "(no session)"
		}
		if _, exists := groupMap[key]; !exists {
			email := ""
			if sess, found := config.Sessions[key]; found {
				email = sess.SSOAccountEmail
			}
			groupMap[key] = &sessionGroup{session: key, email: email}
			groupOrder = append(groupOrder, key)
		}
		groupMap[key].rows = append(groupMap[key].rows, r)
	}

	// Sort groups: named sessions first (alphabetical), then "(inline)", then "(no session)"
	slices.SortFunc(groupOrder, func(a, b string) int {
		aSpecial := strings.HasPrefix(a, "(")
		bSpecial := strings.HasPrefix(b, "(")
		if aSpecial != bSpecial {
			if aSpecial {
				return 1
			}
			return -1
		}
		if a < b {
			return -1
		}
		if a > b {
			return 1
		}
		return 0
	})

	// Sort profiles within each group by environment priority (prod first, then staging, dev, unknown)
	envPriority := map[string]int{"production": 0, "staging": 1, "oat": 2, "development": 3, "unknown": 4}
	for _, g := range groupMap {
		slices.SortFunc(g.rows, func(a, b profileRow) int {
			pa, pb := envPriority[a.env], envPriority[b.env]
			if pa != pb {
				return pa - pb
			}
			if a.name < b.name {
				return -1
			}
			if a.name > b.name {
				return 1
			}
			return 0
		})
	}

	// Determine session-level status (best status among its profiles)
	for _, key := range groupOrder {
		g := groupMap[key]
		best := "No SSO"
		for _, r := range g.rows {
			if r.status == "Active" {
				best = "Active"
				break
			} else if r.status == "Expired" && best != "Active" {
				best = "Expired"
			} else if r.status == "Not Logged In" && best == "No SSO" {
				best = "Not Logged In"
			}
		}
		g.status = best
	}

	// Display
	currentProfile := os.Getenv("AWS_PROFILE")
	globalIdx := 0

	for _, key := range groupOrder {
		g := groupMap[key]

		// Session header
		sessionStatusColor := Dim
		switch g.status {
		case "Active":
			sessionStatusColor = Green
		case "Expired":
			sessionStatusColor = Yellow
		case "Not Logged In":
			sessionStatusColor = Red
		}

		sessionLabel := g.session
		if g.email != "" {
			sessionLabel = fmt.Sprintf("%s %s(%s)%s", g.session, Magenta, g.email, Reset)
		}
		fmt.Printf("\n  %s┌─ Session: %s%s  [%s%s%s]%s\n",
			Dim, Reset+Bold, sessionLabel, sessionStatusColor, g.status, Reset, Reset)

		// Profiles in this group
		for _, r := range g.rows {
			globalIdx++
			statusColor := Dim
			switch r.status {
			case "Active":
				statusColor = Green
			case "Expired":
				statusColor = Yellow
			case "Not Logged In":
				statusColor = Red
			}

			activeMarker := "  "
			if r.name == currentProfile {
				activeMarker = fmt.Sprintf("%s▶%s ", Green, Reset)
			}

			envSymbol := ""
			if r.env != "unknown" {
				envSymbol = getEnvironmentSymbol(r.env) + " "
			}

			remainingStr := ""
			if r.remaining != "" {
				remainingStr = fmt.Sprintf(" %s%s%s", Dim, r.remaining, Reset)
			}

			fmt.Printf("  %s│%s %s%s[%d]%s %s%-28s%s %s[%s%s%s]%s%s\n",
				Dim, Reset,
				activeMarker,
				Cyan, globalIdx, Reset,
				envSymbol,
				Bold+r.name+Reset, "",
				Dim, statusColor, r.status, Reset,
				Dim,
				remainingStr)

			if r.id != "" {
				fmt.Printf("  %s│%s       %s%s%s / %s%s  %s(%s)%s\n",
					Dim, Reset,
					Dim, r.id, Reset,
					r.role, Reset,
					Dim, r.region, Reset)
			}
		}
		fmt.Printf("  %s└─%s\n", Dim, Reset)
	}

	fmt.Println()

	if currentProfile != "" {
		printInfo(fmt.Sprintf("Currently active: %s%s%s", Bold+Green, currentProfile, Reset))
	} else {
		printInfo("No active profile set (AWS_PROFILE is not set)")
	}
	fmt.Println()

	// Flatten rows in display order for selection
	flatRows := []profileRow{}
	for _, key := range groupOrder {
		flatRows = append(flatRows, groupMap[key].rows...)
	}

	text, ok := readlineInput(fmt.Sprintf("%s?%s Select profile to activate %s(1-%d)%s or Enter to skip: ",
		Yellow, Reset, Dim, len(flatRows), Reset))
	if !ok || text == "" {
		return
	}

	val, err := strconv.Atoi(text)
	if err != nil || val < 1 || val > len(flatRows) {
		printError(fmt.Sprintf("Invalid input %q. Enter a number between 1 and %d", text, len(flatRows)))
		return
	}

	selectedName := flatRows[val-1].name
	profile := config.Profiles[selectedName]

	if !showProductionWarning(profile) {
		return
	}

	selectedRow := flatRows[val-1]

	// Profile has no SSO account/role — auto-configure before activating
	if selectedRow.status == "No SSO" || profile.SSOAccountID == "" || profile.SSORoleName == "" {
		printWarning(fmt.Sprintf("Profile %q has no account or role configured.", selectedName))
		fmt.Println()
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()
		configured := tryAutoConfigureProfile(ctx, selectedName, config)
		if !configured {
			runSwitch(selectedName, "", false, false)
		}
		// Reload after configuration
		config, _ = loadAWSConfig()
		profile = config.Profiles[selectedName]
		if profile == nil || profile.SSOAccountID == "" || profile.SSORoleName == "" {
			printError("Profile still incomplete — please try again.")
			return
		}
	}

	// If the session is expired, auto-login before activating
	if selectedRow.status == "Expired" || selectedRow.status == "Not Logged In" {
		printWarning("Session expired — initiating login...")

		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		sessionName := profile.SSOSession
		emailHint := ""
		if sessionName != "" {
			if sess, found := config.Sessions[sessionName]; found {
				emailHint = sess.SSOAccountEmail
			}
		}

		startURL := resolveStartURL(profile, config)
		ssoRegion := resolveSSORegion(profile, config, nil)
		cachePath, pathErr := getSSOTokenPath(profile, config)
		if pathErr != nil {
			printError(fmt.Sprintf("Failed to resolve token path: %v", pathErr))
			return
		}

		usePrivate := needsPrivateBrowser(config, profile)
		if usePrivate {
			printInfo(fmt.Sprintf("Multi-identity detected — opening private browser for: %s", emailHint))
		}

		_, loginErr := loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, sessionName, emailHint, usePrivate)
		if loginErr != nil {
			printError(fmt.Sprintf("Login failed: %v", loginErr))
			return
		}
		printSuccess("Logged in successfully!")
		fmt.Println()
	}

	// Set for the current process and persist so the parent REPL can sync
	os.Setenv("AWS_PROFILE", selectedName)
	writeActiveProfile(selectedName)

	// Write a helper script that the user can source to activate the profile
	setProfileScript(selectedName)
}

// setProfileScript writes activation helpers and shows the user how to set the profile.
func setProfileScript(profileName string) {
	printSuccess(fmt.Sprintf("Selected profile: %s%s%s", Bold, profileName, Reset))
	fmt.Println()

	home, err := homeDir()
	printHeader("ACTIVATE PROFILE")

	if runtime.GOOS == "windows" {
		if err == nil {
			cmdPath := filepath.Join(home, ".aws", "activate.cmd")
			cmdContent := fmt.Sprintf("@echo off\nset AWS_PROFILE=%s\necho AWS_PROFILE set to %s\n", profileName, profileName)
			if writeErr := os.WriteFile(cmdPath, []byte(cmdContent), 0644); writeErr == nil {
				printInfo(fmt.Sprintf("Activation script written to: %s", cmdPath))
			}

			ps1Path := filepath.Join(home, ".aws", "activate.ps1")
			ps1Content := fmt.Sprintf("$env:AWS_PROFILE=\"%s\"\nWrite-Host \"AWS_PROFILE set to %s\" -ForegroundColor Green\n", profileName, profileName)
			_ = os.WriteFile(ps1Path, []byte(ps1Content), 0644)
		}
		fmt.Printf("  %sPowerShell:%s\n", Bold, Reset)
		fmt.Printf("    %s$env:AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
		fmt.Printf("    %s# or run: . ~/.aws/activate.ps1%s\n\n", Dim, Reset)
		fmt.Printf("  %sCmd:%s\n", Bold, Reset)
		fmt.Printf("    %sset AWS_PROFILE=%s%s\n", Dim, profileName, Reset)
		fmt.Printf("    %s# or run: %%USERPROFILE%%\\.aws\\activate.cmd%s\n\n", Dim, Reset)
		fmt.Printf("  %sBash/Zsh:%s\n", Bold, Reset)
		fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n\n", Dim, profileName, Reset)
	} else {
		if err == nil {
			shPath := filepath.Join(home, ".aws", "activate.sh")
			shContent := fmt.Sprintf("export AWS_PROFILE=\"%s\"\necho \"AWS_PROFILE set to %s\"\n", profileName, profileName)
			if writeErr := os.WriteFile(shPath, []byte(shContent), 0644); writeErr == nil {
				printInfo(fmt.Sprintf("Activation script written to: %s", shPath))
			}
		}
		fmt.Printf("  %sBash/Zsh:%s\n", Bold, Reset)
		fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
		fmt.Printf("    %s# or run: source ~/.aws/activate.sh%s\n\n", Dim, Reset)
	}
}

func runExport(profileName string, format string, clipboard bool) {
	var exportFormat ExportFormat
	switch strings.ToLower(format) {
	case "env", "environment":
		exportFormat = FormatEnv
	case "terraform", "tf":
		exportFormat = FormatTerraform
	case "docker":
		exportFormat = FormatDocker
	case "json":
		exportFormat = FormatJSON
	case "yaml", "yml":
		exportFormat = FormatYAML
	case "kyaml", "kubernetes":
		exportFormat = FormatKYAML
	case "credential_process", "credential":
		exportFormat = FormatCredentialProcess
	case "profile":
		exportFormat = FormatProfile
	default:
		printError(fmt.Sprintf("Unknown format %q. Supported: env, terraform, docker, json, yaml, kyaml, credential_process, profile", format))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	// No --profile given → show interactive picker.
	// For credential formats show only Active profiles; for --format profile
	// any profile is valid (no credentials needed).
	if profileName == "" {
		profileName = pickProfileForConsole(config, exportFormat != FormatProfile)
		if profileName == "" {
			return
		}
	}

	// "profile" format — just outputs the AWS_PROFILE activation line.
	// No credentials needed, works for any profile including [No SSO] ones.
	if exportFormat == FormatProfile {
		var output string
		if runtime.GOOS == "windows" {
			output = fmt.Sprintf(`$env:AWS_PROFILE="%s"`, profileName)
		} else {
			output = fmt.Sprintf(`export AWS_PROFILE="%s"`, profileName)
		}
		if clipboard {
			if err := writeToClipboard(output); err != nil {
				printError(fmt.Sprintf("Failed to copy to clipboard: %v", err))
				fmt.Println(output)
				os.Exit(1)
			}
			printSuccess(fmt.Sprintf("Copied to clipboard: %s", output))
			return
		}
		fmt.Println(output)
		return
	}

	creds, profile, err := resolveCredentials(ctx, profileName, config)
	if err != nil {
		os.Exit(1)
	}

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	_ = recordRecentProfile(profile)

	output := exportCredentials(creds, exportFormat)

	env := detectEnvironment(profile)
	envSymbol := getEnvironmentSymbol(env)

	if clipboard {
		if err := writeToClipboard(output); err != nil {
			printError(fmt.Sprintf("Failed to copy to clipboard: %v", err))
			printInfo("Printing to stdout instead:")
			fmt.Println(output)
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Credentials copied to clipboard! %s %s / %s format", envSymbol, profileName, format))
		printInfo(fmt.Sprintf("Expires: %s", creds.Expiration))
		return
	}

	fmt.Fprintf(os.Stderr, "\n%s Export for profile: %s%s%s (%s %s)\n", Dim, Bold, profileName, Dim, envSymbol, env)
	fmt.Fprintf(os.Stderr, " Format: %s\n%s\n", format, Reset)
	fmt.Println(output)
}

// runSessions lists all configured SSO sessions with their identity info and token status.
// This is essential for multi-identity setups where multiple sessions share the same start URL.
func runSessions() {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	if len(config.Sessions) == 0 {
		printWarning("No SSO sessions found in ~/.aws/config")
		printInfo("Sessions are created automatically when you run 'awssso create' or 'awssso login'")
		printInfo("You can also add them manually:")
		fmt.Fprintf(os.Stderr, "\n  %s[sso-session my-session]%s\n", Dim, Reset)
		fmt.Fprintf(os.Stderr, "  %ssso_start_url = https://d-123.awsapps.com/start/%s\n", Dim, Reset)
		fmt.Fprintf(os.Stderr, "  %ssso_region = eu-west-1%s\n", Dim, Reset)
		fmt.Fprintf(os.Stderr, "  %ssso_account_email = user@example.com%s\n\n", Dim, Reset)
		return
	}

	printHeader(fmt.Sprintf("SSO SESSIONS (%d)", len(config.Sessions)))

	// Group profiles by session
	sessionProfiles := map[string][]string{}
	for name, p := range config.Profiles {
		if p.SSOSession != "" {
			sessionProfiles[p.SSOSession] = append(sessionProfiles[p.SSOSession], name)
		}
	}

	// Detect sessions sharing the same start URL
	urlSessions := map[string][]string{}
	for name, s := range config.Sessions {
		urlSessions[s.SSOStartURL] = append(urlSessions[s.SSOStartURL], name)
	}

	i := 0
	for name, session := range config.Sessions {
		i++

		// Check token status
		mockProfile := &AWSProfile{SSOSession: name}
		status := "Not Logged In"
		remaining := ""
		statusColor := Red

		if tokenPath, err := getSSOTokenPath(mockProfile, config); err == nil {
			if token, err := readSSOToken(tokenPath); err == nil {
				if token.IsExpired() {
					status = "Expired"
					statusColor = Yellow
					if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
						remaining = fmt.Sprintf("Expired %s ago", formatDuration(time.Since(parsed)))
					}
				} else {
					status = "Active"
					statusColor = Green
					if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
						remaining = fmt.Sprintf("Expires in %s", formatDuration(time.Until(parsed)))
					}
				}
			}
		}

		remainingStr := ""
		if remaining != "" {
			remainingStr = fmt.Sprintf(" %s(%s)%s", Dim, remaining, Reset)
		}

		fmt.Printf("  %s[%d]%s %s%-25s%s %s[%s%s%s]%s%s\n",
			Cyan, i, Reset,
			Bold, name, Reset,
			Dim, statusColor, status, Reset, Dim,
			remainingStr)

		fmt.Printf("       %sStart URL:%s %s\n", Dim, Reset, session.SSOStartURL)
		fmt.Printf("       %sRegion:%s    %s\n", Dim, Reset, session.SSORegion)

		if session.SSOAccountEmail != "" {
			fmt.Printf("       %sEmail:%s     %s%s%s\n", Dim, Reset, Magenta, session.SSOAccountEmail, Reset)
		}

		// Show shared URL warning
		if peers := urlSessions[session.SSOStartURL]; len(peers) > 1 {
			otherSessions := []string{}
			for _, peer := range peers {
				if peer != name {
					otherSessions = append(otherSessions, peer)
				}
			}
			fmt.Printf("       %s⚡ Shares start URL with:%s %s\n", Yellow, Reset, strings.Join(otherSessions, ", "))
		}

		// Show linked profiles
		if profiles := sessionProfiles[name]; len(profiles) > 0 {
			fmt.Printf("       %sProfiles:%s  %s\n", Dim, Reset, strings.Join(profiles, ", "))
		} else {
			fmt.Printf("       %sProfiles:%s  %s(none)%s\n", Dim, Reset, Dim, Reset)
		}
		fmt.Println()
	}

	// Helpful tips for multi-identity setups
	hasSharedURLs := false
	for _, sessions := range urlSessions {
		if len(sessions) > 1 {
			hasSharedURLs = true
			break
		}
	}

	if hasSharedURLs {
		printHeader("MULTI-IDENTITY TIPS")
		fmt.Printf("  You have sessions sharing the same SSO Start URL.\n")
		fmt.Printf("  Each session maintains a %sseparate token cache%s, so you can be\n", Bold, Reset)
		fmt.Printf("  logged in to multiple identities simultaneously.\n\n")
		fmt.Printf("  %sLogin to a specific session:%s\n", Bold, Reset)
		fmt.Printf("    %sawssso login --session <session-name>%s\n\n", Dim, Reset)
		fmt.Printf("  %sRefresh a specific session:%s\n", Bold, Reset)
		fmt.Printf("    %sawssso refresh --session <session-name>%s\n\n", Dim, Reset)
		fmt.Printf("  %sAdd email identity tracking:%s\n", Bold, Reset)
		fmt.Printf("    Add %ssso_account_email = user@example.com%s to your [sso-session] section\n\n", Cyan, Reset)
	}
}

// runDelete handles the 'delete' command for removing one or more profiles from ~/.aws/config.
func runDelete(profileNames []string) {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	// If profile names provided as args, delete them directly
	if len(profileNames) > 0 {
		for _, name := range profileNames {
			if err := removeAWSProfile(name); err != nil {
				printError(fmt.Sprintf("Failed to remove profile %q: %v", name, err))
			} else {
				printSuccess(fmt.Sprintf("Profile %q removed from ~/.aws/config", name))
			}
		}
		return
	}

	// Interactive mode: show profiles and let user select which to delete
	if len(config.Profiles) == 0 {
		printWarning("No AWS profiles found in ~/.aws/config")
		return
	}

	printHeader(fmt.Sprintf("DELETE PROFILES (%d available)", len(config.Profiles)))

	type profileRow struct {
		name    string
		id      string
		role    string
		session string
	}
	rows := []profileRow{}

	for name, p := range config.Profiles {
		session := p.SSOSession
		if session == "" && p.SSOStartURL != "" {
			session = "(inline)"
		}
		rows = append(rows, profileRow{
			name:    name,
			id:      p.SSOAccountID,
			role:    p.SSORoleName,
			session: session,
		})
	}

	for i, r := range rows {
		fmt.Printf("  %s[%d]%s %s%-30s%s", Cyan, i+1, Reset, Bold, r.name, Reset)
		if r.id != "" {
			fmt.Printf(" %s(%s / %s)%s", Dim, r.id, r.role, Reset)
		}
		if r.session != "" {
			fmt.Printf(" %s[session: %s]%s", Dim, r.session, Reset)
		}
		fmt.Println()
	}

	fmt.Println()
	printWarning("This will permanently remove the selected profile(s) from ~/.aws/config")
	fmt.Println()

	for {
		text, ok := readlineInput(fmt.Sprintf("%s?%s Enter number(s) to delete %s(e.g. 1,3,5 or 1-3)%s, or %sq%s to cancel: ",
			Yellow, Reset, Dim, Reset, Bold, Reset))
		if !ok || text == "" || text == "q" || text == "quit" || text == "exit" {
			printInfo("Canceled")
			return
		}

		// Parse selection (supports: "1", "1,3,5", "1-3", "1-3,5")
		indices := parseSelection(text, len(rows))
		if len(indices) == 0 {
			printError("Invalid selection. Use numbers like 1,3,5 or ranges like 1-3")
			continue
		}

		// Show confirmation
		fmt.Println()
		printWarning("About to delete:")
		for _, idx := range indices {
			fmt.Printf("  %s•%s %s\n", Red, Reset, rows[idx].name)
		}
		fmt.Println()
		confirm, _ := readlineInput(fmt.Sprintf("%s?%s Confirm deletion? (yes/no): ", Yellow, Reset))
		if strings.ToLower(confirm) != "yes" && strings.ToLower(confirm) != "y" {
			printInfo("Canceled")
			return
		}

		// Delete in reverse order to avoid index shifting issues
		deleted := 0
		for i := len(indices) - 1; i >= 0; i-- {
			name := rows[indices[i]].name
			if err := removeAWSProfile(name); err != nil {
				printError(fmt.Sprintf("Failed to remove %q: %v", name, err))
			} else {
				printSuccess(fmt.Sprintf("Removed profile %q", name))
				deleted++
			}
		}

		if deleted > 0 {
			printSuccess(fmt.Sprintf("Done! %d profile(s) deleted.", deleted))
		}
		return
	}
}

// parseSelection parses a selection string into zero-based indices.
// Accepts: "1,3,5"  "1-3"  "1-3,5"  "1 2 3"  "1 2,4 6-8"
func parseSelection(input string, maxItems int) []int {
	seen := map[int]bool{}
	// Normalise spaces to commas so "1 2 3" and "1,2,3" are equivalent
	input = strings.NewReplacer(" ", ",").Replace(input)
	parts := strings.Split(input, ",")

	for _, part := range parts {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}

		if strings.Contains(part, "-") {
			rangeParts := strings.SplitN(part, "-", 2)
			start, err1 := strconv.Atoi(strings.TrimSpace(rangeParts[0]))
			end, err2 := strconv.Atoi(strings.TrimSpace(rangeParts[1]))
			if err1 != nil || err2 != nil || start < 1 || end < 1 || start > maxItems || end > maxItems {
				return nil
			}
			if start > end {
				start, end = end, start
			}
			for i := start; i <= end; i++ {
				seen[i-1] = true
			}
		} else {
			val, err := strconv.Atoi(part)
			if err != nil || val < 1 || val > maxItems {
				return nil
			}
			seen[val-1] = true
		}
	}

	indices := []int{}
	for idx := range seen {
		indices = append(indices, idx)
	}

	slices.Sort(indices)

	return indices
}

// runShell spawns a system shell with the given profile's AWS credentials in the environment.
// This allows users to run terraform, aws-cli, and other tools without manually exporting credentials.
// If profileName is empty, uses the active profile from AWS_PROFILE environment variable.
func runShell(profileName string) {
	if profileName == "" {
		profileName = os.Getenv("AWS_PROFILE")
	}
	if profileName == "" {
		printWarning("No active profile. Use 'profiles' to activate one, or run: awssso shell --profile <name>")
		return
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		return
	}

	creds, _, err := resolveCredentials(ctx, profileName, config)
	if err != nil {
		printError(fmt.Sprintf("Failed to resolve credentials: %v", err))
		return
	}

	// Build environment with AWS credentials
	env := os.Environ()
	env = append(env,
		fmt.Sprintf("AWS_ACCESS_KEY_ID=%s", creds.AccessKeyId),
		fmt.Sprintf("AWS_SECRET_ACCESS_KEY=%s", creds.SecretAccessKey),
		fmt.Sprintf("AWS_SESSION_TOKEN=%s", creds.SessionToken),
		fmt.Sprintf("AWS_PROFILE=%s", profileName),
	)

	// Determine shell and spawn
	var shell string
	var args []string

	if runtime.GOOS == "windows" {
		// Prefer legacy powershell.exe (always present on Windows); fall back
		// to pwsh (PowerShell 7+) on systems where only the newer version is installed.
		if _, err := exec.LookPath("powershell.exe"); err == nil {
			shell = "powershell.exe"
		} else {
			shell = "pwsh"
		}
		args = []string{}
	} else {
		shell = os.Getenv("SHELL")
		if shell == "" {
			shell = "/bin/bash"
		}
		args = []string{}
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Spawning %s with profile: %s", filepath.Base(shell), profileName))
	if runtime.GOOS != "windows" {
		printInfo("Type 'exit' to return to awssso")
	}
	fmt.Println()

	cmd := exec.Command(shell, args...)
	cmd.Stdin = os.Stdin
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.Env = env

	_ = cmd.Run()

	fmt.Println()
}

// runRecreate bulk-creates profiles by name. For each name it:
//  1. Searches AWS accounts for one whose name matches the profile name.
//  2. Fetches roles for that account.
//  3. Picks the role automatically — preferring the --role flag value when
//     provided, then the only role when there is just one, then the first role.
//  4. Saves the profile to ~/.aws/config using exactly the name given.
func runRecreate(profileNames []string, defaultRole, sessionName string) {
	if len(profileNames) == 0 {
		printError("Usage: awssso recreate <profile> [<profile>...] [--role <role>] [--session <session>]")
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load config: %v", err))
		os.Exit(1)
	}

	// Resolve a valid SSO token — prefer the specified session, fall back to any active one.
	var validToken, validRegion, validSession string
	if sessionName != "" {
		mock := &AWSProfile{SSOSession: sessionName}
		if t, r, e := resolveValidToken(ctx, mock, config); e == nil {
			validToken, validRegion, validSession = t.AccessToken, r, sessionName
		} else {
			printError(fmt.Sprintf("Session %q is not active: %v", sessionName, e))
			os.Exit(1)
		}
	} else {
		for name := range config.Sessions {
			mock := &AWSProfile{SSOSession: name}
			if t, r, e := resolveValidToken(ctx, mock, config); e == nil {
				validToken, validRegion, validSession = t.AccessToken, r, name
				break
			}
		}
	}
	if validToken == "" {
		printError("No active SSO session found. Run: awssso login")
		os.Exit(1)
	}

	// Fetch all accounts once.
	spinner := NewSpinner("Fetching AWS accounts")
	spinner.Start()
	accounts, err := fetchAccounts(ctx, validRegion, validToken)
	if err != nil {
		spinner.Stop(false, "Failed to fetch accounts")
		printError(fmt.Sprintf("%v", err))
		os.Exit(1)
	}
	spinner.Stop(true, fmt.Sprintf("Fetched %d accounts", len(accounts)))

	sess := config.Sessions[validSession]
	region := sess.SSORegion

	printHeader(fmt.Sprintf("RECREATE %d PROFILE(S)", len(profileNames)))
	if defaultRole != "" {
		printInfo(fmt.Sprintf("Default role: %s%s%s", Bold, defaultRole, Reset))
	}
	fmt.Println()

	created, failed := 0, 0
	for _, profileName := range profileNames {
		matched := findMatchingAccount(accounts, profileName)
		if matched == nil {
			fmt.Printf("  %s✗%s %-40s  no matching account found\n", Red, Reset, profileName)
			failed++
			continue
		}

		roles, err := fetchAccountRoles(ctx, validRegion, *matched.AccountId, validToken)
		if err != nil || len(roles) == 0 {
			fmt.Printf("  %s✗%s %-40s  could not fetch roles\n", Red, Reset, profileName)
			failed++
			continue
		}

		// Pick role: prefer --role match, then single role, then first role.
		roleName := *roles[0].RoleName
		if defaultRole != "" {
			for _, r := range roles {
				if strings.EqualFold(*r.RoleName, defaultRole) {
					roleName = *r.RoleName
					break
				}
			}
		}

		newProfile := &AWSProfile{
			Name:         profileName,
			SSOSession:   validSession,
			SSOAccountID: *matched.AccountId,
			SSORoleName:  roleName,
			Region:       region,
		}
		if err := writeAWSProfile(profileName, newProfile); err != nil {
			fmt.Printf("  %s✗%s %-40s  failed to save: %v\n", Red, Reset, profileName, err)
			failed++
			continue
		}
		fmt.Printf("  %s✓%s %-40s  %s / %s\n", Green, Reset, profileName, *matched.AccountName, roleName)
		created++
	}

	fmt.Println()
	if failed > 0 {
		printWarning(fmt.Sprintf("%d created, %d failed.", created, failed))
	} else {
		printSuccess(fmt.Sprintf("All %d profile(s) created.", created))
	}
}

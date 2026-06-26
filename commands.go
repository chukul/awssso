package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso/types"
)

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

// needsPrivateBrowser returns true if the profile's session shares its start URL
// with other sessions (multi-identity scenario). In that case, a private/incognito
// browser is needed to avoid authenticating with the wrong cached identity.
func needsPrivateBrowser(config *AWSConfig, profile *AWSProfile) bool {
	startURL := resolveStartURL(profile, config)
	if startURL == "" {
		return false
	}

	// Count how many sessions use this same start URL
	count := 0
	for _, sess := range config.Sessions {
		if sess.SSOStartURL == startURL {
			count++
		}
	}

	// If more than one session shares the URL, we need private mode
	return count > 1
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
		printInfo(fmt.Sprintf("Run: awssso switch --profile %s to configure", profileName))
		return nil, fmt.Errorf("incomplete profile")
	}
	return profile, nil
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

	scanner := bufio.NewScanner(os.Stdin)
	profile := getOrConfigureProfile(scanner, config, profileName)

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

	profile, err := loadProfile(config, profileName)
	if err != nil {
		os.Exit(1)
	}

	token, ssoRegion, err := resolveValidToken(ctx, profile, config)
	if err != nil {
		printError(err.Error())
		os.Exit(1)
	}

	creds, err := fetchRoleCredentials(ctx, ssoRegion, profile.SSOAccountID, profile.SSORoleName, token.AccessToken)
	if err != nil {
		var unauthErr *types.UnauthorizedException
		if errors.As(err, &unauthErr) || strings.Contains(err.Error(), "ExpiredToken") {
			printError(fmt.Sprintf("SSO Session is invalid or expired. Run: awssso login --profile %s", profileName))
		} else {
			printError(fmt.Sprintf("Failed to fetch role credentials: %v", err))
		}
		os.Exit(1)
	}

	_ = recordRecentProfile(profile)

	if err := json.NewEncoder(os.Stdout).Encode(creds); err != nil {
		printError(fmt.Sprintf("Failed to encode credentials: %v", err))
		os.Exit(1)
	}
}

func runSwitch(profileName string, sessionName string, private bool) {
	profileName = getProfileName(profileName)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	scanner := bufio.NewScanner(os.Stdin)

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
		profile = getOrConfigureProfile(scanner, config, profileName)
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
		printError(fmt.Sprintf("Error fetching accounts: %v", err))
		os.Exit(1)
	}
	accSpinner.Stop(true, "Fetched available AWS accounts")

	if len(accounts) == 0 {
		printWarning("No AWS accounts found for this SSO session")
		printInfo("This may indicate a permissions issue with your SSO configuration")
		return
	}

	selectedAccount := selectAccount(scanner, accounts)
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
		printPrompt(fmt.Sprintf("Select a role %s(1-%d)%s or %sq%s to quit: ", Dim, len(roles), Reset, Bold, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "q" || text == "quit" || text == "exit" {
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
	printPrompt(fmt.Sprintf("Enter profile name %s(press Enter for %q)%s: ", Dim, defaultProfileName, Reset))
	var customProfileName string
	if scanner.Scan() {
		customProfileName = strings.TrimSpace(scanner.Text())
	}
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
	printHeader("NEXT STEPS")
	fmt.Printf("  %s1.%s Set the profile as active:\n", Cyan, Reset)
	fmt.Printf("     %s$env:AWS_PROFILE=\"%s\"%s\n\n", Dim, customProfileName, Reset)
	fmt.Printf("  %s2.%s Use AWS CLI or other tools:\n", Cyan, Reset)
	fmt.Printf("     %saws s3 ls%s\n\n", Dim, Reset)
	fmt.Printf("  %s3.%s Open AWS Console:\n", Cyan, Reset)
	fmt.Printf("     %sawssso console --profile %s%s\n", Dim, customProfileName, Reset)
}

// selectAccount handles the interactive account search and selection flow.
func selectAccount(scanner *bufio.Scanner, accounts []types.AccountInfo) types.AccountInfo {
	printHeader("AVAILABLE AWS ACCOUNTS")
	fmt.Printf("  %sTotal:%s %d accounts found. Type to search by name or ID.\n\n", Dim, Reset, len(accounts))

	var filteredAccounts []types.AccountInfo
	for {
		printPrompt(fmt.Sprintf("Search accounts %s(or press Enter to list all, or %sq%s to quit)%s: ", Dim, Bold, Reset, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		searchTerm := strings.TrimSpace(scanner.Text())
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
		printPrompt(fmt.Sprintf("Select an account %s(1-%d)%s or %sq%s to quit: ", Dim, len(filteredAccounts), Reset, Bold, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		text := strings.TrimSpace(scanner.Text())
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

func getOrConfigureProfile(scanner *bufio.Scanner, config *AWSConfig, profileName string) *AWSProfile {
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

	var startURL string
	for {
		printPrompt(fmt.Sprintf("SSO Start URL %s(e.g. https://my-sso.awsapps.com/start/)%s: ", Dim, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		startURL = strings.TrimSpace(scanner.Text())
		if startURL == "" {
			printError("SSO Start URL cannot be empty")
			continue
		}
		if !strings.HasPrefix(startURL, "https://") {
			printWarning("SSO Start URL should begin with https://")
			printPrompt("Continue anyway? (y/N): ")
			if scanner.Scan() {
				confirm := strings.ToLower(strings.TrimSpace(scanner.Text()))
				if confirm != "y" && confirm != "yes" {
					continue
				}
			}
		}
		break
	}

	var ssoRegion string
	for {
		printPrompt(fmt.Sprintf("SSO Region %s(e.g. us-east-1, eu-west-1)%s: ", Dim, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		ssoRegion = strings.TrimSpace(scanner.Text())
		if ssoRegion == "" {
			printError("SSO Region cannot be empty")
			continue
		}
		break
	}

	printPrompt(fmt.Sprintf("AWS Default Region %s(press Enter for %s)%s: ", Dim, ssoRegion, Reset))
	var awsRegion string
	if !scanner.Scan() {
		os.Exit(1)
	}
	awsRegion = strings.TrimSpace(scanner.Text())
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
		profileName = pickProfileForConsole(config)
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

// pickProfileForConsole shows a grouped, interactive profile picker for the console command.
// Displays session status (Active/Expired/Not Logged In) for each profile.
// Returns the selected profile name, or empty string if cancelled.
func pickProfileForConsole(config *AWSConfig) string {
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
		if p.SSOAccountID == "" || p.SSORoleName == "" {
			continue
		}
		session := p.SSOSession
		if session == "" && p.SSOStartURL != "" {
			session = "(inline)"
		}

		status := "No SSO"
		remaining := ""
		if p.SSOSession != "" || p.SSOStartURL != "" {
			mockProfile := &AWSProfile{}
			if p.SSOSession != "" {
				mockProfile.SSOSession = p.SSOSession
			} else {
				mockProfile.SSOStartURL = p.SSOStartURL
			}
			if tokenPath, err := getSSOTokenPath(mockProfile, config); err == nil {
				if token, err := readSSOToken(tokenPath); err == nil {
					if token.IsExpired() {
						status = "Expired"
						if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
							remaining = fmt.Sprintf("Expired %s ago", formatDuration(time.Since(parsed)))
						}
					} else {
						status = "Active"
						if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
							remaining = fmt.Sprintf("%s left", formatDuration(time.Until(parsed)))
						}
					}
				} else {
					status = "Not Logged In"
				}
			}
		}

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

	if len(allRows) == 0 {
		printWarning("No AWS profiles with SSO configuration found")
		printInfo("Run 'awssso switch' to create one")
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
	printHeader("OPEN AWS CONSOLE — SELECT PROFILE")
	fmt.Printf("  %sExpired sessions will auto-login (private browser for multi-identity sessions).%s\n", Dim, Reset)

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

			fmt.Printf("  %s│%s   %s[%d]%s %s%-26s%s %s[%s%s%s]%s%s  %s%s / %s (%s)%s\n",
				Dim, Reset,
				Cyan, globalIdx, Reset,
				envSymbol,
				Bold+r.name+Reset, "",
				Dim, statusColor, r.status, Reset, Dim,
				remainingStr,
				Dim, r.id, r.role, r.region, Reset)
		}
		fmt.Printf("  %s└─%s\n", Dim, Reset)
	}

	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	for {
		printPrompt(fmt.Sprintf("Select profile %s(1-%d)%s or %sq%s to quit: ", Dim, len(flatRows), Reset, Bold, Reset))
		if !scanner.Scan() {
			return ""
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" || text == "q" || text == "quit" || text == "exit" {
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

func runRefresh(profileName string, sessionName string, force bool, private bool) {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	if sessionName != "" {
		refreshSingleSession(ctx, config, sessionName, force, private)
	} else if profileName != "" {
		refreshSingleProfile(ctx, config, profileName, force, private)
	} else {
		refreshAllSessions(ctx, config, force, private)
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

	printInfo(fmt.Sprintf("No refresh token available for session %q - initiating device authorization...", sessionName))
	_, err = loginSSOWithHint(ctx, session.SSOStartURL, session.SSORegion, cachePath, sessionName, session.SSOAccountEmail, private)
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

	printInfo(fmt.Sprintf("No refresh token available for profile %q - initiating device authorization...", profileName))
	printInfo("A browser window will open for you to authenticate.")
	_, err = loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, "", "", private)
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
			printInfo(fmt.Sprintf("Opening browser for session %q...", shortName(s.Name)))
			if emailHint != "" {
				printInfo(fmt.Sprintf("  Identity: %s", emailHint))
			}
			_, err := loginSSOWithHint(ctx, s.StartURL, s.Region, s.TokenPath, s.Name, emailHint, private)
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
		token, err := readSSOToken(cachePath)
		if err == nil {
			fmt.Println()
			if token.IsExpired() {
				printWarning("SSO Token: EXPIRED")
				printInfo("Run: awssso login")
			} else {
				expiry, _ := time.Parse(time.RFC3339, token.ExpiresAt)
				remaining := formatDuration(time.Until(expiry))
				printSuccess(fmt.Sprintf("SSO Token: Valid (%s remaining)", remaining))
			}
		}
	}

	fmt.Println()
	printHeader("USAGE")
	fmt.Printf("  %sPowerShell:%s\n", Bold, Reset)
	fmt.Printf("    %s$env:AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
	fmt.Printf("\n  %sBash/Zsh:%s\n", Bold, Reset)
	fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
	fmt.Println()
}

func runProfiles() {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	if len(config.Profiles) == 0 {
		printWarning("No AWS profiles found in ~/.aws/config")
		printInfo("Run 'awssso switch' to create one")
		return
	}

	printHeader(fmt.Sprintf("AWS PROFILES (%d)", len(config.Profiles)))

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
		status := "No SSO"
		remaining := ""
		session := p.SSOSession

		if p.SSOSession != "" || p.SSOStartURL != "" {
			mockProfile := &AWSProfile{}
			if p.SSOSession != "" {
				mockProfile.SSOSession = p.SSOSession
			} else {
				mockProfile.SSOStartURL = p.SSOStartURL
				session = "(inline)"
			}
			if tokenPath, err := getSSOTokenPath(mockProfile, config); err == nil {
				if token, err := readSSOToken(tokenPath); err == nil {
					if token.IsExpired() {
						status = "Expired"
						if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
							remaining = fmt.Sprintf("Expired %s ago", formatDuration(time.Since(parsed)))
						}
					} else {
						status = "Active"
						if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
							remaining = fmt.Sprintf("Expires in %s", formatDuration(time.Until(parsed)))
						}
					}
				} else {
					status = "Not Logged In"
				}
			}
		}

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

	scanner := bufio.NewScanner(os.Stdin)
	printPrompt(fmt.Sprintf("Select profile to activate %s(1-%d)%s, or press Enter to skip: ", Dim, len(flatRows), Reset))
	if !scanner.Scan() {
		return
	}
	text := strings.TrimSpace(scanner.Text())
	if text == "" {
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

	// Set AWS_PROFILE for the current process (useful if invoked from scripts)
	os.Setenv("AWS_PROFILE", selectedName)

	// Write a helper script that the user can source to activate the profile
	setProfileScript(selectedName)
}

// setProfileScript writes activation helpers and shows the user how to set the profile.
func setProfileScript(profileName string) {
	printSuccess(fmt.Sprintf("Selected profile: %s%s%s", Bold, profileName, Reset))
	fmt.Println()

	// Write a .cmd helper for quick activation
	home, err := homeDir()
	if err == nil {
		cmdPath := filepath.Join(home, ".aws", "activate.cmd")
		cmdContent := fmt.Sprintf("@echo off\nset AWS_PROFILE=%s\necho AWS_PROFILE set to %s\n", profileName, profileName)
		if err := os.WriteFile(cmdPath, []byte(cmdContent), 0644); err == nil {
			printInfo(fmt.Sprintf("Activation script written to: %s", cmdPath))
		}

		ps1Path := filepath.Join(home, ".aws", "activate.ps1")
		ps1Content := fmt.Sprintf("$env:AWS_PROFILE=\"%s\"\nWrite-Host \"AWS_PROFILE set to %s\" -ForegroundColor Green\n", profileName, profileName)
		if err := os.WriteFile(ps1Path, []byte(ps1Content), 0644); err == nil {
			// silently written
		}
	}

	printHeader("ACTIVATE PROFILE")
	fmt.Printf("  %sPowerShell:%s\n", Bold, Reset)
	fmt.Printf("    %s$env:AWS_PROFILE=\"%s\"%s\n", Dim, profileName, Reset)
	fmt.Printf("    %s# or run: . ~/.aws/activate.ps1%s\n\n", Dim, Reset)
	fmt.Printf("  %sCmd:%s\n", Bold, Reset)
	fmt.Printf("    %sset AWS_PROFILE=%s%s\n", Dim, profileName, Reset)
	fmt.Printf("    %s# or run: %%USERPROFILE%%\\.aws\\activate.cmd%s\n\n", Dim, Reset)
	fmt.Printf("  %sBash/Zsh:%s\n", Bold, Reset)
	fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n\n", Dim, profileName, Reset)
}

func runQuick() {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	recent, err := getRecentProfiles()
	if err != nil {
		printError(fmt.Sprintf("Failed to load recent profiles: %v", err))
		os.Exit(1)
	}

	if len(recent) == 0 {
		printInfo("No recent profiles found")
		printInfo("Use profiles with 'login', 'credential', 'switch', or 'console' commands to build history")
		return
	}

	printHeader("RECENT PROFILES")

	validProfiles := []RecentProfile{}
	for i, rp := range recent {
		if _, ok := config.Profiles[rp.Name]; !ok {
			continue
		}
		validProfiles = append(validProfiles, rp)

		env := detectEnvironment(config.Profiles[rp.Name])
		envColor := getEnvironmentColor(env)
		envSymbol := getEnvironmentSymbol(env)

		fmt.Printf("  %s[%d]%s %s %s%s%-20s%s %s%s%s %s(%s)%s\n",
			Cyan, i+1, Reset,
			envSymbol,
			Bold, envColor, rp.Name, Reset,
			Dim, rp.RoleName, Reset,
			Dim, formatTimeSince(rp.Timestamp), Reset)
	}

	if len(validProfiles) == 0 {
		printWarning("No valid recent profiles found")
		return
	}

	fmt.Println()

	scanner := bufio.NewScanner(os.Stdin)
	var choice int
	for {
		printPrompt(fmt.Sprintf("Select profile %s(1-%d)%s or %sq%s to quit: ", Dim, len(validProfiles), Reset, Bold, Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "q" || text == "quit" || text == "exit" {
			printInfo("Canceled")
			os.Exit(0)
		}
		val, err := strconv.Atoi(text)
		if err == nil && val >= 1 && val <= len(validProfiles) {
			choice = val
			break
		}
		printError(fmt.Sprintf("Invalid input %q. Enter a number between 1 and %d", text, len(validProfiles)))
	}

	selectedProfile := validProfiles[choice-1]
	profile := config.Profiles[selectedProfile.Name]

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Selected profile: %s", selectedProfile.Name))
	printHeader("SET PROFILE")
	fmt.Printf("  %sPowerShell:%s\n", Bold, Reset)
	fmt.Printf("    %s$env:AWS_PROFILE=\"%s\"%s\n\n", Dim, selectedProfile.Name, Reset)
	fmt.Printf("  %sBash/Zsh:%s\n", Bold, Reset)
	fmt.Printf("    %sexport AWS_PROFILE=\"%s\"%s\n\n", Dim, selectedProfile.Name, Reset)

	printInfo("Or run commands directly:")
	fmt.Printf("  %sawssso console --profile %s%s\n", Dim, selectedProfile.Name, Reset)
	fmt.Printf("  %sawssso export --profile %s%s\n", Dim, selectedProfile.Name, Reset)
}

func runExport(profileName string, format string) {
	profileName = getProfileName(profileName)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	profile, err := loadProfile(config, profileName)
	if err != nil {
		os.Exit(1)
	}

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	token, ssoRegion, err := resolveValidToken(ctx, profile, config)
	if err != nil {
		printError(err.Error())
		os.Exit(1)
	}

	creds, err := fetchRoleCredentials(ctx, ssoRegion, profile.SSOAccountID, profile.SSORoleName, token.AccessToken)
	if err != nil {
		printError(fmt.Sprintf("Failed to fetch credentials: %v", err))
		os.Exit(1)
	}

	_ = recordRecentProfile(profile)

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
	case "credential_process", "credential":
		exportFormat = FormatCredentialProcess
	default:
		printError(fmt.Sprintf("Unknown format %q. Supported: env, terraform, docker, json, yaml, credential_process", format))
		os.Exit(1)
	}

	output := exportCredentials(creds, exportFormat)

	env := detectEnvironment(profile)
	envSymbol := getEnvironmentSymbol(env)
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
		printInfo("Sessions are created automatically when you run 'awssso switch' or 'awssso login'")
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

	scanner := bufio.NewScanner(os.Stdin)
	for {
		printPrompt(fmt.Sprintf("Enter number(s) to delete %s(e.g. 1,3,5 or 1-3)%s, or %sq%s to cancel: ", Dim, Reset, Bold, Reset))
		if !scanner.Scan() {
			return
		}
		text := strings.TrimSpace(scanner.Text())
		if text == "" || text == "q" || text == "quit" || text == "exit" {
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
		printPrompt("Confirm deletion? (yes/no): ")
		if !scanner.Scan() {
			return
		}
		confirm := strings.ToLower(strings.TrimSpace(scanner.Text()))
		if confirm != "yes" && confirm != "y" {
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

// parseSelection parses a selection string like "1,3,5" or "1-3" or "1-3,5" into zero-based indices.
func parseSelection(input string, maxItems int) []int {
	seen := map[int]bool{}
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

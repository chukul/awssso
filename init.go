package main

import (
	"context"
	"fmt"
	"os"
	"runtime"
	"strings"
	"time"
)

func runInit() {
	printHeader("AWSSSO SETUP WIZARD")
	fmt.Printf("  %sThis wizard creates your initial ~/.aws/config with SSO configured.%s\n", Dim, Reset)
	fmt.Printf("  %sIf a config already exists, new entries will be appended.%s\n\n", Dim, Reset)

	// ── Step 1: SSO Start URL ─────────────────────────────────────────────────
	defaultURL, defaultRegion := "", ""
	config, _ := loadAWSConfig()
	if config != nil {
		defaultURL, defaultRegion = orgDefaults(config)
	}

	var startURL string
	for {
		prompt := fmt.Sprintf("%s?%s SSO Start URL %s(e.g. https://d-xxxxxxxxxx.awsapps.com/start/)%s: ",
			Yellow, Reset, Dim, Reset)
		if defaultURL != "" {
			prompt = fmt.Sprintf("%s?%s SSO Start URL %s(press Enter for %s)%s: ",
				Yellow, Reset, Dim, defaultURL, Reset)
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
			printWarning("URL should begin with https://")
			confirm, _ := readlineInput(fmt.Sprintf("%s?%s Continue anyway? (y/N): ", Yellow, Reset))
			if strings.ToLower(confirm) != "y" {
				continue
			}
		}
		break
	}

	// ── Step 2: SSO Region ────────────────────────────────────────────────────
	var ssoRegion string
	for {
		prompt := fmt.Sprintf("%s?%s SSO Region %s(e.g. us-east-1, eu-west-1)%s: ", Yellow, Reset, Dim, Reset)
		if defaultRegion != "" {
			prompt = fmt.Sprintf("%s?%s SSO Region %s(press Enter for %s)%s: ", Yellow, Reset, Dim, defaultRegion, Reset)
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

	// ── Step 3: Authenticate ──────────────────────────────────────────────────
	sessionName := sessionNameFromURL(startURL)
	if err := writeSSOSession(sessionName, startURL, ssoRegion); err != nil {
		printWarning(fmt.Sprintf("Could not write session: %v", err))
	}

	mockProfile := &AWSProfile{SSOSession: sessionName}
	cachePath, err := getSSOTokenPath(mockProfile, &AWSConfig{
		Profiles: map[string]*AWSProfile{},
		Sessions: map[string]*SSOSession{
			sessionName: {Name: sessionName, SSOStartURL: startURL, SSORegion: ssoRegion},
		},
	})
	if err != nil {
		printError(fmt.Sprintf("Cannot resolve token path: %v", err))
		os.Exit(1)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	fmt.Println()
	printInfo("Opening browser to authenticate with AWS SSO...")
	token, err := loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, sessionName, "", false)
	if err != nil {
		printError(fmt.Sprintf("Authentication failed: %v", err))
		os.Exit(1)
	}
	printSuccess("Authenticated successfully!")
	fmt.Println()

	// ── Step 4: Pick account ──────────────────────────────────────────────────
	spinner := NewSpinner("Fetching your AWS accounts")
	spinner.Start()
	accounts, err := fetchAccounts(ctx, ssoRegion, token.AccessToken)
	if err != nil {
		spinner.Stop(false, "Failed to fetch accounts")
		printError(fmt.Sprintf("Error: %v", err))
		os.Exit(1)
	}
	spinner.Stop(true, fmt.Sprintf("Found %d account(s)", len(accounts)))

	scanner := newStdinScanner()
	selectedAccount := selectAccount(scanner, accounts)
	acctID := *selectedAccount.AccountId
	acctName := *selectedAccount.AccountName

	// ── Step 5: Pick role ─────────────────────────────────────────────────────
	roleSpinner := NewSpinner("Fetching roles")
	roleSpinner.Start()
	roles, err := fetchAccountRoles(ctx, ssoRegion, acctID, token.AccessToken)
	if err != nil || len(roles) == 0 {
		roleSpinner.Stop(false, "Failed to fetch roles")
		os.Exit(1)
	}
	roleSpinner.Stop(true, fmt.Sprintf("%d role(s) available", len(roles)))

	printHeader("AVAILABLE ROLES")
	for i, r := range roles {
		fmt.Printf("  %s[%d]%s %s\n", Cyan, i+1, Reset, Bold+*r.RoleName+Reset)
	}
	fmt.Println()

	var roleName string
	for {
		printPrompt(fmt.Sprintf("Select a role %s(1-%d)%s: ", Dim, len(roles), Reset))
		if !scanner.Scan() {
			os.Exit(1)
		}
		text := strings.TrimSpace(scanner.Text())
		if val, convErr := parseInt(text); convErr == nil && val >= 1 && val <= len(roles) {
			roleName = *roles[val-1].RoleName
			break
		}
		printError(fmt.Sprintf("Enter a number between 1 and %d", len(roles)))
	}

	// ── Step 6: Profile name ──────────────────────────────────────────────────
	defaultProfileName := fmt.Sprintf("%s_%s", acctName, roleName)
	profileName, _ := readlineInput(fmt.Sprintf("%s?%s Profile name %s(press Enter for %q)%s: ",
		Yellow, Reset, Dim, defaultProfileName, Reset))
	if profileName == "" {
		profileName = defaultProfileName
		printInfo(fmt.Sprintf("Using: %s", profileName))
	}

	// ── Step 7: AWS default region ────────────────────────────────────────────
	awsRegion, _ := readlineInput(fmt.Sprintf("%s?%s AWS Default Region %s(press Enter for %s)%s: ",
		Yellow, Reset, Dim, ssoRegion, Reset))
	if awsRegion == "" {
		awsRegion = ssoRegion
	}

	// ── Step 8: Save ──────────────────────────────────────────────────────────
	newProfile := &AWSProfile{
		Name:         profileName,
		SSOSession:   sessionName,
		SSOAccountID: acctID,
		SSORoleName:  roleName,
		Region:       awsRegion,
	}
	if err := writeAWSProfile(profileName, newProfile); err != nil {
		printError(fmt.Sprintf("Failed to save profile: %v", err))
		os.Exit(1)
	}

	fmt.Println()
	printSuccess(fmt.Sprintf("Setup complete! Profile %q saved to ~/.aws/config", profileName))
	fmt.Println()
	printHeader("ACTIVATE")
	if isWindows() {
		fmt.Printf("  %s$env:AWS_PROFILE=\"%s\"%s\n\n", Dim, profileName, Reset)
	} else {
		fmt.Printf("  %sexport AWS_PROFILE=\"%s\"%s\n\n", Dim, profileName, Reset)
	}
}

// parseInt is a small helper to avoid importing strconv in callers.
func parseInt(s string) (int, error) {
	var n int
	_, err := fmt.Sscanf(s, "%d", &n)
	return n, err
}

// isWindows returns true on Windows.
func isWindows() bool {
	return runtime.GOOS == "windows"
}

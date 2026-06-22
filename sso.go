package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sso"
	"github.com/aws/aws-sdk-go-v2/service/sso/types"
	"github.com/aws/aws-sdk-go-v2/service/ssooidc"
	ssooidctypes "github.com/aws/aws-sdk-go-v2/service/ssooidc/types"
)

// newAWSConfig creates a regional AWS config. Centralizes SDK config creation
// so that shared settings (retry, HTTP client tuning, etc.) can be added in one place.
func newAWSConfig(region string) aws.Config {
	cfg := aws.NewConfig()
	cfg.Region = region
	return *cfg
}

// newSSOClient creates an SSO service client for the given region.
func newSSOClient(region string) *sso.Client {
	cfg := newAWSConfig(region)
	return sso.NewFromConfig(cfg)
}

// newOIDCClient creates an SSO OIDC service client for the given region.
func newOIDCClient(region string) *ssooidc.Client {
	cfg := newAWSConfig(region)
	return ssooidc.NewFromConfig(cfg)
}

func loginSSO(ctx context.Context, startURL string, ssoRegion string, cachePath string) (*SSOToken, error) {
	return loginSSOWithHint(ctx, startURL, ssoRegion, cachePath, "", "", false)
}

// loginSSOWithHint performs SSO login with optional session/email hints shown to the user.
// When private is true, the verification URL is opened in an incognito/InPrivate window,
// which is essential for multi-identity setups where you need a clean session per email.
func loginSSOWithHint(ctx context.Context, startURL string, ssoRegion string, cachePath string, sessionName string, emailHint string, private bool) (*SSOToken, error) {
	oidcClient := newOIDCClient(ssoRegion)

	spinner := NewSpinner("Registering client with AWS SSO OIDC")
	spinner.Start()
	regOutput, err := oidcClient.RegisterClient(ctx, &ssooidc.RegisterClientInput{
		ClientName: aws.String("awssso"),
		ClientType: aws.String("public"),
	})
	if err != nil {
		spinner.Stop(false, "Failed to register client")
		return nil, fmt.Errorf("failed to register client: %w", err)
	}
	spinner.Stop(true, "Registered client with AWS SSO OIDC")

	spinner = NewSpinner("Starting device authorization flow")
	spinner.Start()
	authOutput, err := oidcClient.StartDeviceAuthorization(ctx, &ssooidc.StartDeviceAuthorizationInput{
		ClientId:     regOutput.ClientId,
		ClientSecret: regOutput.ClientSecret,
		StartUrl:     aws.String(startURL),
	})
	if err != nil {
		spinner.Stop(false, "Failed to start device authorization")
		return nil, fmt.Errorf("failed to start device authorization: %w", err)
	}
	spinner.Stop(true, "Device authorization flow started")

	printHeader("AWS SSO DEVICE AUTHENTICATION")

	// Show session identity hint when multiple sessions share the same URL
	if sessionName != "" || emailHint != "" {
		fmt.Printf("%sSession:%s         %s\n", Bold+BrightBlack, Reset, sessionName)
		if emailHint != "" {
			fmt.Printf("%sExpected Email:%s  %s%s%s\n", Bold+BrightBlack, Reset, Bold+Magenta, emailHint, Reset)
			printWarning("Make sure you authenticate with the correct identity in your browser!")
		}
		fmt.Println()
	}

	fmt.Printf("%sVerification URI:%s %s\n", Bold+BrightBlack, Reset, *authOutput.VerificationUri)
	fmt.Printf("%sUser Code:%s        %s%s%s\n\n", Bold+BrightBlack, Reset, Bold+Yellow, *authOutput.UserCode, Reset)

	uriComplete := *authOutput.VerificationUriComplete
	if private {
		printInfo("Opening browser in private/incognito mode...")
		if emailHint != "" {
			printInfo(fmt.Sprintf("Authenticate as: %s%s%s", Bold+Magenta, emailHint, Reset))
		}
		_ = openBrowserPrivate(uriComplete)
	} else {
		printInfo("Opening default web browser to authorize credentials...")
		if emailHint != "" {
			printInfo(fmt.Sprintf("Tip: Use a browser profile signed in as %s%s%s, or use --private flag", Bold, emailHint, Reset))
		}
		_ = openBrowser(uriComplete)
	}

	interval := int(authOutput.Interval)
	if interval == 0 {
		interval = 5
	}

	pollSpinner := NewSpinner("Waiting for authentication in browser")
	pollSpinner.Start()
	ticker := time.NewTicker(time.Duration(interval) * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			pollSpinner.Stop(false, "Authentication timed out or canceled")
			return nil, ctx.Err()
		case <-ticker.C:
			tokenOut, err := oidcClient.CreateToken(ctx, &ssooidc.CreateTokenInput{
				ClientId:     regOutput.ClientId,
				ClientSecret: regOutput.ClientSecret,
				GrantType:    aws.String("urn:ietf:params:oauth:grant-type:device_code"),
				DeviceCode:   authOutput.DeviceCode,
				Code:         authOutput.UserCode,
			})

			if err != nil {
				var pendingErr *ssooidctypes.AuthorizationPendingException
				if errors.As(err, &pendingErr) {
					continue
				}
				pollSpinner.Stop(false, "Authentication failed")
				return nil, fmt.Errorf("create token failed: %w", err)
			}

			pollSpinner.Stop(true, "Successfully authenticated!")
			expiry := time.Now().Add(time.Duration(tokenOut.ExpiresIn) * time.Second)
			var regExpiry string
			if regOutput.ClientSecretExpiresAt > 0 {
				regExpiry = time.Unix(regOutput.ClientSecretExpiresAt, 0).Format(time.RFC3339)
			}

			token := &SSOToken{
				StartURL:              startURL,
				Region:                ssoRegion,
				AccessToken:           *tokenOut.AccessToken,
				ExpiresAt:             expiry.Format(time.RFC3339),
				ClientID:              *regOutput.ClientId,
				ClientSecret:          *regOutput.ClientSecret,
				RegistrationExpiresAt: regExpiry,
			}
			if tokenOut.RefreshToken != nil {
				token.RefreshToken = *tokenOut.RefreshToken
			}

			if err = writeSSOToken(cachePath, token); err != nil {
				return nil, fmt.Errorf("failed to save token: %w", err)
			}

			printSuccess("Cached AWS SSO token successfully!")
			return token, nil
		}
	}
}

func refreshToken(ctx context.Context, ssoRegion string, cachePath string, token *SSOToken) (*SSOToken, error) {
	if token.RefreshToken == "" || token.ClientID == "" || token.ClientSecret == "" {
		return nil, fmt.Errorf("refresh token or client metadata missing from cache")
	}

	oidcClient := newOIDCClient(ssoRegion)

	tokenOut, err := oidcClient.CreateToken(ctx, &ssooidc.CreateTokenInput{
		ClientId:     aws.String(token.ClientID),
		ClientSecret: aws.String(token.ClientSecret),
		GrantType:    aws.String("refresh_token"),
		RefreshToken: aws.String(token.RefreshToken),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to refresh token: %w", err)
	}

	expiry := time.Now().Add(time.Duration(tokenOut.ExpiresIn) * time.Second)
	token.AccessToken = *tokenOut.AccessToken
	token.ExpiresAt = expiry.Format(time.RFC3339)
	if tokenOut.RefreshToken != nil && *tokenOut.RefreshToken != "" {
		token.RefreshToken = *tokenOut.RefreshToken
	}

	if err := writeSSOToken(cachePath, token); err != nil {
		return nil, fmt.Errorf("failed to write refreshed token to cache: %w", err)
	}

	return token, nil
}

// CredentialResponse is the JSON format expected by AWS credential_process.
type CredentialResponse struct {
	Version         int    `json:"Version"`
	AccessKeyId     string `json:"AccessKeyId"`
	SecretAccessKey string `json:"SecretAccessKey"`
	SessionToken    string `json:"SessionToken"`
	Expiration      string `json:"Expiration"`
}

func fetchRoleCredentials(
	ctx context.Context,
	ssoRegion string,
	accountID string,
	roleName string,
	accessToken string,
) (*CredentialResponse, error) {
	ssoClient := newSSOClient(ssoRegion)

	creds, err := ssoClient.GetRoleCredentials(ctx, &sso.GetRoleCredentialsInput{
		AccountId:   aws.String(accountID),
		RoleName:    aws.String(roleName),
		AccessToken: aws.String(accessToken),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to get role credentials: %w", err)
	}

	exp := time.Unix(creds.RoleCredentials.Expiration/1000, 0).UTC().Format(time.RFC3339)

	return &CredentialResponse{
		Version:         1,
		AccessKeyId:     *creds.RoleCredentials.AccessKeyId,
		SecretAccessKey: *creds.RoleCredentials.SecretAccessKey,
		SessionToken:    *creds.RoleCredentials.SessionToken,
		Expiration:      exp,
	}, nil
}

func fetchAccounts(ctx context.Context, ssoRegion string, accessToken string) ([]types.AccountInfo, error) {
	ssoClient := newSSOClient(ssoRegion)

	accounts := []types.AccountInfo{}
	var nextToken *string

	for {
		out, err := ssoClient.ListAccounts(ctx, &sso.ListAccountsInput{
			AccessToken: aws.String(accessToken),
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list accounts: %w", err)
		}
		accounts = append(accounts, out.AccountList...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return accounts, nil
}

func fetchAccountRoles(ctx context.Context, ssoRegion string, accountID string, accessToken string) ([]types.RoleInfo, error) {
	ssoClient := newSSOClient(ssoRegion)

	roles := []types.RoleInfo{}
	var nextToken *string

	for {
		out, err := ssoClient.ListAccountRoles(ctx, &sso.ListAccountRolesInput{
			AccessToken: aws.String(accessToken),
			AccountId:   aws.String(accountID),
			NextToken:   nextToken,
		})
		if err != nil {
			return nil, fmt.Errorf("failed to list account roles: %w", err)
		}
		roles = append(roles, out.RoleList...)
		if out.NextToken == nil {
			break
		}
		nextToken = out.NextToken
	}

	return roles, nil
}

func openAWSConsole(ctx context.Context, region string, creds *CredentialResponse) error {
	type SessionData struct {
		SessionID    string `json:"sessionId"`
		SessionKey   string `json:"sessionKey"`
		SessionToken string `json:"sessionToken"`
	}

	sessionBytes, err := json.Marshal(SessionData{
		SessionID:    creds.AccessKeyId,
		SessionKey:   creds.SecretAccessKey,
		SessionToken: creds.SessionToken,
	})
	if err != nil {
		return fmt.Errorf("failed to marshal session: %w", err)
	}

	federationURL := "https://signin.aws.amazon.com/federation"
	q := url.Values{}
	q.Set("Action", "getSigninToken")
	q.Set("Session", string(sessionBytes))
	reqURL := fmt.Sprintf("%s?%s", federationURL, q.Encode())

	req, err := http.NewRequestWithContext(ctx, "GET", reqURL, nil)
	if err != nil {
		return err
	}

	client := &http.Client{Timeout: 15 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("failed to call federation endpoint: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("federation endpoint returned status %d", resp.StatusCode)
	}

	type FederationResponse struct {
		SigninToken string `json:"SigninToken"`
	}

	var fedResp FederationResponse
	if err := json.NewDecoder(resp.Body).Decode(&fedResp); err != nil {
		return fmt.Errorf("failed to decode federation response: %w", err)
	}

	if region == "" {
		region = "eu-west-1"
	}
	destination := fmt.Sprintf(
		"https://%s.console.aws.amazon.com/console/home?region=%s",
		region,
		region,
	)

	consoleURL := fmt.Sprintf(
		"%s?Action=login&Issuer=awssso&Destination=%s&SigninToken=%s",
		federationURL,
		url.QueryEscape(destination),
		url.QueryEscape(fedResp.SigninToken),
	)

	printSuccess("Successfully generated AWS federation sign-in token.")
	printInfo("Opening AWS Management Console in browser...")
	return openBrowser(consoleURL)
}

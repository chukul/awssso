package main

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/service/sts"
)

func runAssume(roleARN string, profileName string, sessionName string, format string) {
	if roleARN == "" {
		printError("Usage: awssso assume --role <arn> [--profile <profile>] [--session-name <name>] [--format env]")
		printInfo("Example: awssso assume --role arn:aws:iam::123456789:role/MyRole")
		os.Exit(1)
	}

	profileName = getProfileName(profileName)

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	// Get base credentials from the profile
	baseCreds, profile, err := resolveCredentials(ctx, profileName, config)
	if err != nil {
		os.Exit(1)
	}

	if !showProductionWarning(profile) {
		os.Exit(0)
	}

	// Build a static credentials provider from the base profile credentials
	stsClient := sts.New(sts.Options{
		Region: profile.Region,
		Credentials: aws.CredentialsProviderFunc(func(ctx context.Context) (aws.Credentials, error) {
			return aws.Credentials{
				AccessKeyID:     baseCreds.AccessKeyId,
				SecretAccessKey: baseCreds.SecretAccessKey,
				SessionToken:    baseCreds.SessionToken,
			}, nil
		}),
	})

	if sessionName == "" {
		// Derive a session name from the role ARN
		parts := strings.Split(roleARN, "/")
		sessionName = "awssso-" + parts[len(parts)-1]
	}

	spinner := NewSpinner(fmt.Sprintf("Assuming role %s", roleARN))
	spinner.Start()

	out, err := stsClient.AssumeRole(ctx, &sts.AssumeRoleInput{
		RoleArn:         aws.String(roleARN),
		RoleSessionName: aws.String(sessionName),
	})
	if err != nil {
		spinner.Stop(false, "AssumeRole failed")
		printError(fmt.Sprintf("Failed to assume role: %v", err))
		os.Exit(1)
	}
	spinner.Stop(true, fmt.Sprintf("Assumed role: %s", roleARN))

	creds := &CredentialResponse{
		Version:         1,
		AccessKeyId:     *out.Credentials.AccessKeyId,
		SecretAccessKey: *out.Credentials.SecretAccessKey,
		SessionToken:    *out.Credentials.SessionToken,
		Expiration:      out.Credentials.Expiration.UTC().Format(time.RFC3339),
	}

	var exportFormat ExportFormat
	switch strings.ToLower(format) {
	case "json", "credential_process":
		exportFormat = FormatJSON
	case "terraform", "tf":
		exportFormat = FormatTerraform
	case "docker":
		exportFormat = FormatDocker
	case "yaml", "yml":
		exportFormat = FormatYAML
	case "kyaml", "kubernetes":
		exportFormat = FormatKYAML
	default:
		exportFormat = FormatEnv
	}

	if exportFormat == FormatJSON {
		_ = json.NewEncoder(os.Stdout).Encode(creds)
		return
	}

	fmt.Fprintf(os.Stderr, "\n%s Assumed role: %s%s%s\n%s Session: %s\n%s\n",
		Dim, Bold, roleARN, Dim, Dim, sessionName, Reset)
	fmt.Println(exportCredentials(creds, exportFormat))
}

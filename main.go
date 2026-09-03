package main

import (
	"flag"
	"fmt"
	"os"
)

func main() {
	initTerminal()

	loginCmd := flag.NewFlagSet("login", flag.ExitOnError)
	loginProfile := loginCmd.String("profile", "", "AWS profile name")
	loginSession := loginCmd.String("session", "", "SSO session name (for multi-identity setups)")
	loginPrivate := loginCmd.Bool("private", false, "Open browser in incognito/InPrivate mode")
	loginGroup := loginCmd.String("group", "", "Login to all SSO sessions used by profiles in this group")

	// credential is intentionally hidden from help — only used by credential_process in ~/.aws/config
	credCmd := flag.NewFlagSet("credential", flag.ExitOnError)
	credProfile := credCmd.String("profile", "", "AWS profile name")

	createCmd := flag.NewFlagSet("create", flag.ExitOnError)
	createProfile := createCmd.String("profile", "", "AWS profile name")
	createSession := createCmd.String("session", "", "SSO session name")
	createPrivate := createCmd.Bool("private", false, "Open browser in incognito/InPrivate mode")

	consoleCmd := flag.NewFlagSet("console", flag.ExitOnError)
	consoleProfile := consoleCmd.String("profile", "", "AWS profile name")

	whoamiCmd := flag.NewFlagSet("whoami", flag.ExitOnError)
	whoamiProfile := whoamiCmd.String("profile", "", "AWS profile name")

	deleteCmd := flag.NewFlagSet("delete", flag.ExitOnError)
	deleteProfile := deleteCmd.String("profile", "", "Profile name to delete")

	refreshCmd := flag.NewFlagSet("refresh", flag.ExitOnError)
	refreshProfile := refreshCmd.String("profile", "", "AWS profile name (refresh all if omitted)")
	refreshSession := refreshCmd.String("session", "", "SSO session name (refresh specific session)")
	refreshForce := refreshCmd.Bool("force", false, "Refresh even valid tokens (proactive refresh)")
	refreshPrivate := refreshCmd.Bool("private", false, "Open browser in incognito/InPrivate mode")

	exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
	exportProfile := exportCmd.String("profile", "", "AWS profile name")
	exportFormat := exportCmd.String("format", "env", "Format: env, terraform, docker, json, yaml, kyaml, credential_process")
	exportClipboard := exportCmd.Bool("clipboard", false, "Copy credentials to clipboard instead of printing")

	completionCmd := flag.NewFlagSet("completion", flag.ExitOnError)
	completionShell := completionCmd.String("shell", "", "Shell type: zsh, bash, fish, powershell (auto-detected if omitted)")
	completionInstall := completionCmd.Bool("install", false, "Install the script for the current user")
	completionPrompt := completionCmd.Bool("prompt", false, "Output or install a shell prompt badge instead of tab completion")

	renameCmd := flag.NewFlagSet("rename", flag.ExitOnError)

	groupCmd := flag.NewFlagSet("group", flag.ExitOnError)
	groupAdd := groupCmd.Bool("add", false, "Add profile to a group tag")
	groupRemove := groupCmd.Bool("remove", false, "Remove profile from a group tag")
	groupProfile := groupCmd.String("profile", "", "Profile name (alternative to positional arg)")

	profilesCmd := flag.NewFlagSet("profiles", flag.ExitOnError)
	profilesGroup := profilesCmd.String("group", "", "Filter profiles by group tag")

	if len(os.Args) < 2 {
		runREPL()
		return
	}

	switch os.Args[1] {
	case "login":
		_ = loginCmd.Parse(os.Args[2:])
		if *loginGroup != "" {
			runLoginGroup(*loginGroup, *loginPrivate)
		} else {
			runLogin(*loginProfile, *loginSession, *loginPrivate)
		}
	case "credential":
		// Hidden — used by credential_process in ~/.aws/config, not advertised to users
		_ = credCmd.Parse(os.Args[2:])
		runCredential(*credProfile)
	case "create":
		_ = createCmd.Parse(os.Args[2:])
		runSwitch(*createProfile, *createSession, *createPrivate)
	case "console":
		_ = consoleCmd.Parse(os.Args[2:])
		runConsole(*consoleProfile)
	case "whoami":
		_ = whoamiCmd.Parse(os.Args[2:])
		runWhoami(*whoamiProfile)
	case "profiles":
		_ = profilesCmd.Parse(os.Args[2:])
		runProfilesFiltered(*profilesGroup)
	case "delete":
		_ = deleteCmd.Parse(os.Args[2:])
		args := deleteCmd.Args()
		if *deleteProfile != "" {
			args = append([]string{*deleteProfile}, args...)
		}
		runDelete(args)
	case "refresh":
		_ = refreshCmd.Parse(os.Args[2:])
		runRefresh(*refreshProfile, *refreshSession, refreshCmd.Args(), *refreshForce, *refreshPrivate)
	case "export":
		_ = exportCmd.Parse(os.Args[2:])
		runExport(*exportProfile, *exportFormat, *exportClipboard)
	case "doctor":
		runDoctor()
	case "init":
		runInit()
	case "rename":
		_ = renameCmd.Parse(os.Args[2:])
		args := renameCmd.Args()
		if len(args) != 2 {
			printError("Usage: awssso rename <old-name> <new-name>")
			os.Exit(1)
		}
		runRename(args[0], args[1])
	case "group":
		_ = groupCmd.Parse(os.Args[2:])
		runGroup(groupCmd.Args(), *groupAdd, *groupRemove, *groupProfile)
	case "completion":
		_ = completionCmd.Parse(os.Args[2:])
		if *completionPrompt {
			runPrompt(*completionInstall)
		} else {
			runCompletion(*completionShell, *completionInstall)
		}
	case "shell":
		runREPL()
	case "__list-profiles":
		runListProfiles()
	case "__list-sessions":
		runListSessions()
	case "version", "-v", "--version":
		printVersion()
		os.Exit(0)
	case "help", "-h", "--help":
		printUsage()
		os.Exit(0)
	default:
		printError(fmt.Sprintf("Unknown command %q", os.Args[1]))
		fmt.Fprintf(os.Stderr, "\n")
		printUsage()
		os.Exit(1)
	}
}

// Version is set at build time via: go build -ldflags "-X main.Version=v4.0.0"
// Update this whenever the CHANGELOG major version changes.
var Version = "v4.0.0" // overridden at build time by make install via -ldflags

func printVersion() {
	fmt.Printf("awssso %s\n", Version)
}

func printUsage() {
	fmt.Fprintf(os.Stderr, "%s%sawssso — AWS SSO Credential Process Helper%s\n", Bold, Cyan, Reset)
	fmt.Fprintf(os.Stderr, "%sA fast CLI tool for AWS SSO authentication and credential management%s\n\n", Dim, Reset)

	fmt.Fprintf(os.Stderr, "%sUSAGE:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  awssso <command> [options]\n")
	fmt.Fprintf(os.Stderr, "  awssso                     (no command → interactive shell)\n\n")

	fmt.Fprintf(os.Stderr, "%sCORE COMMANDS:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %slogin%s       Authenticate via AWS SSO and cache credentials\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %screate%s      Pick an account/role and create a profile\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sprofiles%s    List profiles and set one as active (AWS_PROFILE)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sexport%s      Get credentials (--format, --clipboard)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %srefresh%s     Refresh expired SSO tokens\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %swhoami%s      Show current profile, account, role, and token status\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sconsole%s     Open AWS Management Console in browser\n\n", Cyan, Reset)

	fmt.Fprintf(os.Stderr, "%sMANAGEMENT:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %sgroup%s       Manage profile groups (--add, --remove; profiles --group <tag>)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %srename%s      Rename a profile in ~/.aws/config\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sdelete%s      Delete one or more profiles\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sdoctor%s      Run a config and token health check\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sinit%s        First-time setup wizard\n\n", Cyan, Reset)

	fmt.Fprintf(os.Stderr, "%sSHELL INTEGRATION:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %scompletion%s  Tab completion (--install) · prompt badge (--prompt --install)\n\n", Cyan, Reset)

	fmt.Fprintf(os.Stderr, "%sKEY OPTIONS:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %s--profile%s <name>    AWS profile name\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--group%s <tag>       Target a profile group (login, profiles)\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--format%s <fmt>      env · terraform · docker · json · yaml · kyaml\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--clipboard%s         Copy credentials to clipboard (export)\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--private%s           Open browser in incognito/InPrivate mode\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--force%s             Refresh even valid tokens (refresh)\n\n", Yellow, Reset)

	fmt.Fprintf(os.Stderr, "%sEXAMPLES:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %sawssso login --profile my-profile%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  %sawssso export --format terraform --clipboard%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  %sawssso profiles --group eks%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  %sawssso completion --install%s\n\n", Dim, Reset)
}

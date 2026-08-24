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
	loginPrivate := loginCmd.Bool("private", false, "Open browser in incognito/InPrivate mode (useful for multi-identity)")

	credCmd := flag.NewFlagSet("credential", flag.ExitOnError)
	credProfile := credCmd.String("profile", "", "AWS profile name")

	switchCmd := flag.NewFlagSet("switch", flag.ExitOnError)
	switchProfile := switchCmd.String("profile", "", "AWS profile name")
	switchSession := switchCmd.String("session", "", "SSO session name (determines which identity fetches accounts)")
	switchPrivate := switchCmd.Bool("private", false, "Open browser in incognito/InPrivate mode (useful for multi-identity)")

	consoleCmd := flag.NewFlagSet("console", flag.ExitOnError)
	consoleProfile := consoleCmd.String("profile", "", "AWS profile name")

	dashboardCmd := flag.NewFlagSet("dashboard", flag.ExitOnError)

	whoamiCmd := flag.NewFlagSet("whoami", flag.ExitOnError)
	whoamiProfile := whoamiCmd.String("profile", "", "AWS profile name")

	quickCmd := flag.NewFlagSet("quick", flag.ExitOnError)

	_ = flag.NewFlagSet("profiles", flag.ExitOnError) // replaced by profilesCmd2

	deleteCmd := flag.NewFlagSet("delete", flag.ExitOnError)

	sessionsCmd := flag.NewFlagSet("sessions", flag.ExitOnError)

	refreshCmd := flag.NewFlagSet("refresh", flag.ExitOnError)
	refreshProfile := refreshCmd.String("profile", "", "AWS profile name (refresh all if omitted)")
	refreshSession := refreshCmd.String("session", "", "SSO session name (refresh specific session)")
	refreshForce := refreshCmd.Bool("force", false, "Refresh even valid tokens (proactive refresh)")
	refreshPrivate := refreshCmd.Bool("private", false, "Open browser in incognito/InPrivate mode (useful for multi-identity)")

	exportCmd := flag.NewFlagSet("export", flag.ExitOnError)
	exportProfile := exportCmd.String("profile", "", "AWS profile name")
	exportFormat := exportCmd.String("format", "env", "Export format: env, terraform, docker, json, yaml, credential_process")

	completionCmd := flag.NewFlagSet("completion", flag.ExitOnError)
	completionShell := completionCmd.String("shell", "", "Shell type: zsh, bash, or fish (auto-detected if omitted)")
	completionInstall := completionCmd.Bool("install", false, "Install the completion script for the current user")

	copyCmd := flag.NewFlagSet("copy", flag.ExitOnError)
	copyProfile := copyCmd.String("profile", "", "AWS profile name")
	copyFormat := copyCmd.String("format", "env", "Export format: env, terraform, docker, json, yaml, kyaml")

	renameCmd := flag.NewFlagSet("rename", flag.ExitOnError)

	pinCmd := flag.NewFlagSet("pin", flag.ExitOnError)
	unpinCmd := flag.NewFlagSet("unpin", flag.ExitOnError)

	groupCmd := flag.NewFlagSet("group", flag.ExitOnError)
	groupAdd := groupCmd.Bool("add", false, "Add profile to a group tag")
	groupRemove := groupCmd.Bool("remove", false, "Remove profile from a group tag")

	profilesCmd2 := flag.NewFlagSet("profiles", flag.ExitOnError)
	profilesGroup := profilesCmd2.String("group", "", "Filter profiles by group tag")

	if len(os.Args) < 2 {
		runREPL()
		return
	}

	switch os.Args[1] {
	case "login":
		_ = loginCmd.Parse(os.Args[2:])
		runLogin(*loginProfile, *loginSession, *loginPrivate)
	case "credential":
		_ = credCmd.Parse(os.Args[2:])
		runCredential(*credProfile)
	case "switch":
		_ = switchCmd.Parse(os.Args[2:])
		runSwitch(*switchProfile, *switchSession, *switchPrivate)
	case "console":
		_ = consoleCmd.Parse(os.Args[2:])
		runConsole(*consoleProfile)
	case "dashboard":
		_ = dashboardCmd.Parse(os.Args[2:])
		runDashboard()
	case "whoami":
		_ = whoamiCmd.Parse(os.Args[2:])
		runWhoami(*whoamiProfile)
	case "quick":
		_ = quickCmd.Parse(os.Args[2:])
		runQuick()
	// "profiles" is handled below with --group support
	case "delete":
		_ = deleteCmd.Parse(os.Args[2:])
		runDelete(deleteCmd.Args())
	case "sessions":
		_ = sessionsCmd.Parse(os.Args[2:])
		runSessions()
	case "refresh":
		_ = refreshCmd.Parse(os.Args[2:])
		runRefresh(*refreshProfile, *refreshSession, refreshCmd.Args(), *refreshForce, *refreshPrivate)
	case "export":
		_ = exportCmd.Parse(os.Args[2:])
		runExport(*exportProfile, *exportFormat)
	case "shell":
		runREPL()
	case "completion":
		_ = completionCmd.Parse(os.Args[2:])
		runCompletion(*completionShell, *completionInstall)
	case "copy":
		_ = copyCmd.Parse(os.Args[2:])
		runCopy(*copyProfile, *copyFormat)
	case "doctor":
		runDoctor()
	case "prompt":
		promptInstall := len(os.Args) > 2 && os.Args[2] == "--install"
		runPrompt(promptInstall)
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
	case "pin":
		_ = pinCmd.Parse(os.Args[2:])
		if len(pinCmd.Args()) == 0 {
			runPin("") // no arg → list pins
		} else {
			runPin(pinCmd.Args()[0])
		}
	case "unpin":
		_ = unpinCmd.Parse(os.Args[2:])
		if len(unpinCmd.Args()) == 0 {
			runUnpin("") // no arg → list pinned profiles
		} else {
			runUnpin(unpinCmd.Args()[0])
		}
	case "group":
		_ = groupCmd.Parse(os.Args[2:])
		runGroup(groupCmd.Args(), *groupAdd, *groupRemove)
	case "profiles":
		_ = profilesCmd2.Parse(os.Args[2:])
		runProfilesFiltered(*profilesGroup)
	case "__list-profiles":
		runListProfiles()
	case "__list-sessions":
		runListSessions()
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

func printUsage() {
	fmt.Fprintf(os.Stderr, "%s%sAWS SSO Credential Process Helper%s\n", Bold, Cyan, Reset)
	fmt.Fprintf(os.Stderr, "%sA fast CLI tool for AWS SSO authentication and credential management%s\n\n", Dim, Reset)

	fmt.Fprintf(os.Stderr, "%sUSAGE:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  awssso <command> [options]\n\n")

	fmt.Fprintf(os.Stderr, "%sCOMMANDS:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %slogin%s       Authenticate via AWS SSO and cache credentials\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %scredential%s  Output temporary AWS credentials in JSON format\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sswitch%s      Interactively select account/role and create profile\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sconsole%s     Open AWS Management Console in browser\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sdashboard%s   Interactive TUI session management dashboard\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sprofiles%s    List profiles and set one as active (AWS_PROFILE)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sdelete%s      Delete one or more profiles from ~/.aws/config\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %ssessions%s    List all SSO sessions with identity and status info\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %srefresh%s     Refresh expired SSO tokens (all sessions or by profile/session)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "\n%sCLOUD ENGINEER COMMANDS:%s\n", Bold+Green, Reset)
	fmt.Fprintf(os.Stderr, "  %swhoami%s      Show current profile, account, role, and SSO token status\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %squick%s       Quick switch between recently used profiles\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sexport%s      Export credentials for DevOps tools (Terraform, Docker, etc.)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sshell%s       Start an interactive session (also runs when no command is given)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %scompletion%s  Generate shell tab-completion script (zsh, bash, fish)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "\n%sNEW COMMANDS:%s\n", Bold+Green, Reset)
	fmt.Fprintf(os.Stderr, "  %scopy%s        Copy credentials to clipboard\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sdoctor%s      Run a config and token health check\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sprompt%s      Output profile badge for shell PS1 (--install to auto-configure)\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sinit%s        First-time setup wizard\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %srename%s      Rename a profile in ~/.aws/config\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %spin%s         Pin a profile to the top of all lists\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sunpin%s       Remove a profile pin\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "  %sgroup%s       Tag a profile into a group; filter profiles by group\n", Cyan, Reset)
	fmt.Fprintf(os.Stderr, "\n  %shelp%s        Show this help message\n\n", Cyan, Reset)

	fmt.Fprintf(os.Stderr, "%sOPTIONS:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %s--profile%s <name>   AWS profile name (defaults to $AWS_PROFILE or 'default')\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--session%s <name>   SSO session name (for multi-identity login/refresh)\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--private%s          Open browser in incognito/InPrivate mode [login, switch, refresh]\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--format%s <fmt>     Export format: env, terraform, docker, json, yaml, credential_process\n", Yellow, Reset)
	fmt.Fprintf(os.Stderr, "  %s--force%s            Refresh even valid tokens (proactive refresh) [refresh only]\n\n", Yellow, Reset)

	fmt.Fprintf(os.Stderr, "%sEXAMPLES:%s\n", Bold, Reset)
	fmt.Fprintf(os.Stderr, "  %s# Authenticate with SSO%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso login --profile my-profile\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Login to a specific session in private browser (multi-identity)%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso login --session team-alpha --private\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Show current identity%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso whoami\n\n")
	fmt.Fprintf(os.Stderr, "  %s# List profiles and set one as active%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso profiles\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Delete profiles (interactive)%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso delete\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Delete specific profiles by name%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso delete my-old-profile another-profile\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Proactively refresh all valid sessions%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso refresh --force\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Interactive session dashboard%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso dashboard\n\n")
	fmt.Fprintf(os.Stderr, "  %s# Export for Terraform%s\n", Dim, Reset)
	fmt.Fprintf(os.Stderr, "  awssso export --profile prod --format terraform\n\n")
}

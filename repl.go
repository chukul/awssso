package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"
)

// ── Command / flag metadata ───────────────────────────────────────────────────

func isKnownCommand(cmd string) bool {
	for _, c := range replCommands {
		if c == cmd {
			return true
		}
	}
	return false
}

var replCommands = []string{
	"login", "create", "profiles", "export", "refresh",
	"whoami", "console", "group", "pin", "unpin",
	"rename", "delete", "doctor", "init",
	"completion", "shell", "help", "exit", "quit",
}

var replCommandFlags = map[string][]string{
	"login":      {"--profile", "--session", "--private", "--group"},
	"create":     {"--profile", "--session", "--private"},
	"refresh":    {"--profile", "--session", "--private", "--force"},
	"console":    {"--profile"},
	"whoami":     {"--profile"},
	"delete":     {"--profile"},
	"export":     {"--profile", "--format", "--clipboard"},
	"group":      {"--add", "--remove", "--profile"},
	"profiles":   {"--group"},
	"completion": {"--shell", "--install", "--prompt"},
}

var formatValues = []string{"env", "terraform", "docker", "json", "yaml", "kyaml", "credential_process"}
var shellValues = []string{"zsh", "bash", "fish", "powershell"}

// ── Tab completer ─────────────────────────────────────────────────────────────

type replCompleter struct{}

func (c *replCompleter) Do(line []rune, pos int) ([][]rune, int) {
	str := string(line[:pos])
	tokens := parseArgs(str)
	endsWithSpace := len(str) > 0 && (str[len(str)-1] == ' ' || str[len(str)-1] == '\t')

	var prefix, prevToken string
	if endsWithSpace {
		prefix = ""
		if len(tokens) > 0 {
			prevToken = tokens[len(tokens)-1]
		}
	} else {
		if len(tokens) > 0 {
			prefix = tokens[len(tokens)-1]
		}
		if len(tokens) >= 2 {
			prevToken = tokens[len(tokens)-2]
		}
	}

	// 1. First word → complete command names
	if (!endsWithSpace && len(tokens) <= 1) || (endsWithSpace && len(tokens) == 0) {
		return filterComplete(replCommands, prefix)
	}

	cmd := tokens[0]

	// 2. Previous token is a flag → complete its value
	switch prevToken {
	case "--profile":
		return filterComplete(loadProfileNames(), prefix)
	case "--session":
		return filterComplete(loadSessionNames(), prefix)
	case "--format":
		return filterComplete(formatValues, prefix)
	case "--shell":
		return filterComplete(shellValues, prefix)
	case "--group":
		return filterComplete(allGroupTags(), prefix)
	}

	// 3. Current prefix starts with "-" → complete remaining flag names
	if strings.HasPrefix(prefix, "-") {
		return filterComplete(remainingFlagsFor(cmd, tokens, prefix), prefix)
	}

	// 4. Smart contextual: suggest the next logical argument as a full "--flag value" pair
	return smartNextArg(cmd, tokens, prefix)
}

// smartNextArg suggests the next argument based on what the command needs and what's already typed.
// It returns "insertable" completions like "--profile my-account" so selecting one fills both the
// flag name and its value in a single step.
func smartNextArg(cmd string, tokens []string, prefix string) ([][]rune, int) {
	typed := typedFlagSet(tokens)

	switch cmd {
	case "export":
		if !typed["--profile"] {
			return insertableCompletions(profileCandidates(), prefix)
		}
		if !typed["--format"] {
			return insertableCompletions(formatCandidates(), prefix)
		}

	case "login", "create":
		if !typed["--profile"] && !typed["--session"] {
			return insertableCompletions(append(profileCandidates(), sessionCandidates()...), prefix)
		}

	case "console", "whoami", "credential", "delete":
		if !typed["--profile"] {
			return insertableCompletions(profileCandidates(), prefix)
		}

	case "refresh":
		if !typed["--profile"] && !typed["--session"] {
			return insertableCompletions(append(profileCandidates(), sessionCandidates()...), prefix)
		}

	// Positional-argument commands — complete bare profile names, not --flag pairs

	case "rename":
		return filterComplete(loadProfileNames(), prefix)

	case "group":
		if len(tokens) == 1 || (len(tokens) == 2 && !strings.HasPrefix(prefix, "-")) {
			// First arg: "create", "delete", or a profile name / tag name
			groupSubcmds := append([]string{"create", "delete"}, append(loadProfileNames(), allGroupTags()...)...)
			return filterComplete(groupSubcmds, prefix)
		}
		if len(tokens) == 2 && tokens[1] != "create" && tokens[1] != "delete" {
			// Second arg after a profile name: suggest existing tags
			return filterComplete(allGroupTags(), prefix)
		}
		if len(tokens) == 2 && (tokens[1] == "create" || tokens[1] == "delete") {
			// Second arg after create/delete: suggest existing tags
			return filterComplete(allGroupTags(), prefix)
		}

	case "pin":
		// Only show profiles that are not yet pinned
		pinned := loadPins()
		pinnedSet := make(map[string]bool, len(pinned))
		for _, p := range pinned {
			pinnedSet[p] = true
		}
		var unpinned []string
		for _, n := range loadProfileNames() {
			if !pinnedSet[n] {
				unpinned = append(unpinned, n)
			}
		}
		return filterComplete(unpinned, prefix)

	case "unpin":
		// Only show profiles that are currently pinned
		return filterComplete(loadPins(), prefix)
	}

	return filterComplete(remainingFlagsFor(cmd, tokens, prefix), prefix)
}

// profileCandidates returns "--profile <name>" strings for every configured profile.
func profileCandidates() []string {
	names := loadProfileNames()
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "--profile " + n
	}
	return out
}

// sessionCandidates returns "--session <name>" strings for every configured session.
func sessionCandidates() []string {
	names := loadSessionNames()
	out := make([]string, len(names))
	for i, n := range names {
		out[i] = "--session " + n
	}
	return out
}

// formatCandidates returns "--format <value>" strings for every supported export format.
func formatCandidates() []string {
	out := make([]string, len(formatValues))
	for i, f := range formatValues {
		out[i] = "--format " + f
	}
	return out
}

// insertableCompletions handles three prefix shapes for candidates like "--profile myaccount":
//
//	""         → append full candidate          "export " + "--profile myaccount"
//	"--pro"    → complete the flag name part    "export --pro" + "file myaccount"
//	"myacc"    → match on value, replace prefix "export myacc" → "export --profile myaccount"
func insertableCompletions(candidates []string, prefix string) ([][]rune, int) {
	var matches [][]rune
	for _, c := range candidates {
		// Standard: candidate starts with the prefix (covers "" and "--flag..." cases)
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, []rune(c[len(prefix):]))
			continue
		}
		// Bare-word prefix: match against the value portion (after the last space)
		if !strings.HasPrefix(prefix, "-") {
			if idx := strings.LastIndex(c, " "); idx >= 0 {
				if strings.HasPrefix(c[idx+1:], prefix) {
					// Return the full candidate; readline removes `prefix` chars
					// and appends this, effectively replacing the bare word with
					// the full "--flag value" string.
					matches = append(matches, []rune(c))
				}
			}
		}
	}
	return matches, len([]rune(prefix))
}

// filterComplete is the standard completer: candidates that start with prefix,
// returning only the suffix that readline appends.
func filterComplete(candidates []string, prefix string) ([][]rune, int) {
	var matches [][]rune
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, []rune(c[len(prefix):]))
		}
	}
	return matches, len([]rune(prefix))
}

// typedFlagSet returns the set of --flags already present in the token list.
func typedFlagSet(tokens []string) map[string]bool {
	set := make(map[string]bool)
	for _, t := range tokens[1:] { // skip command name
		if strings.HasPrefix(t, "--") {
			set[t] = true
		}
	}
	return set
}

// remainingFlagsFor returns the flags defined for cmd that have not yet been typed.
func remainingFlagsFor(cmd string, tokens []string, prefix string) []string {
	typed := typedFlagSet(tokens)
	var out []string
	for _, f := range replCommandFlags[cmd] {
		if !typed[f] {
			out = append(out, f)
		}
	}
	return out
}

func loadProfileNames() []string {
	config, err := loadAWSConfig()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(config.Profiles))
	for name := range config.Profiles {
		names = append(names, name)
	}
	return names
}

func loadSessionNames() []string {
	config, err := loadAWSConfig()
	if err != nil {
		return nil
	}
	names := make([]string, 0, len(config.Sessions))
	for name := range config.Sessions {
		names = append(names, name)
	}
	return names
}

// cachedReplConfig is loaded once at REPL startup and reused for prompts.
// Avoids parsing ~/.aws/config on every prompt redraw.
var cachedReplConfig *AWSConfig

// replPrompt builds the readline prompt, embedding the active AWS_PROFILE with
// its environment colour. When < 15 min remain on the token it shows a warning.
func replPrompt() string {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		return "\033[1;36mawssso\033[0m › "
	}
	mock := &AWSProfile{Name: profile}
	color := getEnvironmentColor(detectEnvironment(mock))
	badge := tokenBadge(profile, color)
	return fmt.Sprintf("\033[1;36mawssso\033[0m [%s] › ", badge)
}

// tokenBadge returns the coloured profile badge, appending a time warning when
// the SSO token has less than 15 minutes remaining.
// Uses cachedReplConfig to avoid re-parsing ~/.aws/config on every prompt.
func tokenBadge(profile, color string) string {
	suffix := ""
	if cachedReplConfig == nil {
		cachedReplConfig, _ = loadAWSConfig()
	}
	config := cachedReplConfig
	if config != nil {
		if p, ok := config.Profiles[profile]; ok {
			if tokenPath, pathErr := getSSOTokenPath(p, config); pathErr == nil {
				if token, readErr := readSSOToken(tokenPath); readErr == nil {
					if token.IsExpired() {
						suffix = " \033[33m⚠\033[0m"
					} else if expiry, parseErr := time.Parse(time.RFC3339, token.ExpiresAt); parseErr == nil {
						remaining := time.Until(expiry)
						if remaining < 15*time.Minute {
							mins := int(remaining.Minutes()) + 1
							suffix = fmt.Sprintf(" \033[33m~%dm\033[0m", mins)
						}
					}
				}
			}
		}
	}
	return fmt.Sprintf("%s%s\033[0m%s", color, profile, suffix)
}

// ── REPL entry point ──────────────────────────────────────────────────────────

func runREPL() {
	binaryPath, err := os.Executable()
	if err != nil {
		printError("Could not determine binary path")
		os.Exit(1)
	}

	// Use the basic REPL — readline's raw-mode terminal control was being
	// killed by macOS on some systems. The basic loop is stable on all platforms.
	// Tab completion works through the shell's installed completion scripts.
	runBasicREPL(binaryPath)
}

// runBasicREPL is the stable cross-platform REPL loop.
func runBasicREPL(binaryPath string) {
	printHeader("AWSSSO INTERACTIVE SHELL")
	fmt.Printf("  %sType commands without the %sawssso%s prefix. Type exit to quit.%s\n\n",
		Dim, Reset+Bold, Reset+Dim, Reset)

	for {
		fmt.Printf("%sawssso%s › ", Cyan+Bold, Reset)

		line, ok := readLine()
		if !ok {
			fmt.Println()
			printInfo("Goodbye!")
			return
		}

		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" || line == "q" {
			printInfo("Goodbye!")
			return
		}

		args := parseArgs(line)
		if len(args) == 0 {
			continue
		}

		cmd := exec.Command(binaryPath, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()

		fmt.Println()
	}
}

// readLine reads one byte at a time so nothing is pre-buffered and the next
// subprocess always gets a clean stdin. Used only in the basic fallback REPL.
func readLine() (string, bool) {
	var line strings.Builder
	buf := make([]byte, 1)
	for {
		n, err := os.Stdin.Read(buf)
		if n > 0 {
			if buf[0] == '\n' {
				// Strip trailing \r for Windows CRLF line endings
				return strings.TrimRight(line.String(), "\r"), true
			}
			line.WriteByte(buf[0])
		}
		if err != nil {
			return strings.TrimRight(line.String(), "\r"), line.Len() > 0
		}
	}
}

// parseArgs splits a line into tokens, respecting single/double quotes and
// backslash escaping — mirrors how a shell tokenises input.
func parseArgs(line string) []string {
	var args []string
	var current strings.Builder
	inSingle := false
	inDouble := false

	for i := 0; i < len(line); i++ {
		ch := line[i]
		switch {
		case ch == '\'' && !inDouble:
			inSingle = !inSingle
		case ch == '"' && !inSingle:
			inDouble = !inDouble
		case ch == '\\' && !inSingle && i+1 < len(line):
			i++
			current.WriteByte(line[i])
		case (ch == ' ' || ch == '\t') && !inSingle && !inDouble:
			if current.Len() > 0 {
				args = append(args, current.String())
				current.Reset()
			}
		default:
			current.WriteByte(ch)
		}
	}

	if current.Len() > 0 {
		args = append(args, current.String())
	}
	return args
}

package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/chzyer/readline"
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
	"login", "credential", "switch", "console", "dashboard",
	"whoami", "quick", "profiles", "delete", "sessions",
	"refresh", "export", "copy", "doctor", "prompt",
	"init", "rename", "pin", "unpin", "shell",
	"completion", "help", "exit", "quit",
}

var replCommandFlags = map[string][]string{
	"login":      {"--profile", "--session", "--private"},
	"switch":     {"--profile", "--session", "--private"},
	"refresh":    {"--profile", "--session", "--private", "--force"},
	"credential": {"--profile"},
	"console":    {"--profile"},
	"whoami":     {"--profile"},
	"delete":     {"--profile"},
	"export":     {"--profile", "--format"},
	"copy":       {"--profile", "--format"},
	"prompt":     {"--install"},
	"completion": {"--shell", "--install"},
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

	case "login", "switch":
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
		// Both arguments are profile names; always complete from the full list
		return filterComplete(loadProfileNames(), prefix)

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

// replPrompt builds the readline prompt, embedding the active AWS_PROFILE with
// its environment colour so the user always knows which account they are in.
func replPrompt() string {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		return "\033[1;36mawssso\033[0m › "
	}
	// Reuse cloudeng environment detection
	mock := &AWSProfile{Name: profile}
	color := getEnvironmentColor(detectEnvironment(mock))
	return fmt.Sprintf("\033[1;36mawssso\033[0m [%s%s\033[0m] › ", color, profile)
}

// ── REPL entry point ──────────────────────────────────────────────────────────

func runREPL() {
	binaryPath, err := os.Executable()
	if err != nil {
		printError("Could not determine binary path")
		os.Exit(1)
	}

	home, _ := homeDir()
	historyFile := filepath.Join(home, ".aws", "awssso_history")

	rl, err := readline.NewEx(&readline.Config{
		Prompt:            replPrompt(),
		HistoryFile:       historyFile,
		AutoComplete:      &replCompleter{},
		InterruptPrompt:   "^C",
		EOFPrompt:         "exit",
		HistorySearchFold: true,
	})
	if err != nil {
		// readline unavailable (e.g. non-interactive pipe) — fall back to basic loop
		runBasicREPL(binaryPath)
		return
	}
	defer rl.Close()

	printHeader("AWSSSO INTERACTIVE SHELL")
	fmt.Printf("  %sType commands without the %sawssso%s prefix.%s\n",
		Dim, Reset+Bold, Reset+Dim, Reset)
	fmt.Printf("  %s↑↓  history   Tab  complete   Ctrl+R  search   Ctrl+D / exit  quit%s\n\n",
		Dim, Reset)

	for {
		rl.SetPrompt(replPrompt())
		line, err := rl.Readline()

		if err == readline.ErrInterrupt {
			// Ctrl+C on non-empty line: clear and re-prompt
			continue
		}
		if err == io.EOF {
			// Ctrl+D: exit
			fmt.Println()
			printInfo("Goodbye!")
			return
		}
		if err != nil {
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

		if !isKnownCommand(args[0]) {
			printError(fmt.Sprintf("Unknown command %q", args[0]))
			printInfo(fmt.Sprintf("Available: %s", strings.Join(replCommands[:len(replCommands)-2], ", ")))
			fmt.Println()
			continue
		}

		cmd := exec.Command(binaryPath, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()

		// Sync active profile set by subprocess back into our own environment
		// so the REPL prompt reflects the new selection immediately.
		if active := readActiveProfile(); active != "" && active != os.Getenv("AWS_PROFILE") {
			os.Setenv("AWS_PROFILE", active)
		}

		fmt.Println()
	}
}

// runBasicREPL is used when readline is unavailable (non-interactive stdin).
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

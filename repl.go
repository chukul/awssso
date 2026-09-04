package main

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"golang.org/x/term"
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
	"whoami", "console", "group",
	"recreate", "rename", "delete", "doctor", "init",
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
	"shell":      {"--profile"},
	"recreate":   {"--role", "--session"},
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
		if !typed["--profile"] && !typed["--format"] && !typed["--clipboard"] && prefix == "" {
			// No flags typed yet — show the flags, not 22 profiles
			return filterComplete(replCommandFlags["export"], "")
		}
		if !typed["--profile"] && (typed["--format"] || typed["--clipboard"] || prefix != "") {
			return insertableCompletions(profileCandidates(), prefix)
		}
		if !typed["--format"] {
			return insertableCompletions(formatCandidates(), prefix)
		}

	case "login", "create":
		if !typed["--profile"] && !typed["--session"] && prefix == "" {
			return filterComplete(replCommandFlags[cmd], "")
		}
		if !typed["--profile"] && !typed["--session"] {
			return insertableCompletions(append(profileCandidates(), sessionCandidates()...), prefix)
		}

	case "console", "whoami", "credential", "delete":
		if !typed["--profile"] && prefix == "" {
			return filterComplete([]string{"--profile"}, "")
		}
		if !typed["--profile"] {
			return insertableCompletions(profileCandidates(), prefix)
		}

	case "refresh":
		if !typed["--profile"] && !typed["--session"] && prefix == "" {
			return filterComplete(replCommandFlags["refresh"], "")
		}
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
	runBasicREPL(binaryPath)
}

// runBasicREPL runs the REPL. If the terminal supports raw mode it enables Tab
// completion and arrow-key history. Falls back to plain line reading otherwise.
func runBasicREPL(binaryPath string) {
	printHeader("AWSSSO INTERACTIVE SHELL")
	fmt.Printf("  %sType commands without the %sawssso%s prefix.%s\n",
		Dim, Reset+Bold, Reset+Dim, Reset)
	fmt.Printf("  %sTab: complete + select   ↑↓: history / menu   Enter: pick   Ctrl+D / exit: quit%s\n\n",
		Dim, Reset)

	fd := int(os.Stdin.Fd())
	isRaw := term.IsTerminal(fd)

	// History buffer (in-memory for this session)
	var history []string
	historyIdx := -1

	readInput := func(prompt string) (string, bool) {
		fmt.Print(prompt)
		if !isRaw {
			line, ok := readLine()
			return strings.TrimSpace(line), ok
		}

		oldState, err := term.MakeRaw(fd)
		if err != nil {
			isRaw = false
			line, ok := readLine()
			return strings.TrimSpace(line), ok
		}

		var buf []byte   // line content
		cursor := 0      // insert position within buf
		historyIdx = len(history)

		// redraw redraws from cursor to end of line and repositions cursor.
		redraw := func() {
			// Erase from cursor to end, print tail, move cursor back
			tail := buf[cursor:]
			os.Stdout.Write([]byte("\033[K")) // clear to EOL
			os.Stdout.Write(tail)
			// Move cursor left by len(tail)
			if len(tail) > 0 {
				fmt.Printf("\033[%dD", len(tail))
			}
		}

		// replaceContent replaces the whole line with new content.
		replaceContent := func(newBuf []byte) {
			// Move to start, clear line, print new content
			if cursor > 0 {
				fmt.Printf("\033[%dD", cursor)
			}
			fmt.Print("\033[K")
			os.Stdout.Write(newBuf)
			buf = newBuf
			cursor = len(buf)
		}

		b := make([]byte, 32) // larger buffer to catch paste + escape seqs

		for {
			n, err := os.Stdin.Read(b)
			if err != nil || n == 0 {
				term.Restore(fd, oldState)
				fmt.Println()
				return "", false
			}

			i := 0
			for i < n {
				ch := b[i]

				switch {
				case ch == '\r' || ch == '\n': // Enter
					term.Restore(fd, oldState)
					fmt.Println()
					return strings.TrimSpace(string(buf)), true

				case ch == 4: // Ctrl+D
					term.Restore(fd, oldState)
					fmt.Println()
					if len(buf) == 0 {
						return "", false
					}
					return strings.TrimSpace(string(buf)), true

				case ch == 3: // Ctrl+C
					term.Restore(fd, oldState)
					fmt.Println("^C")
					return "", true

				case ch == 1: // Ctrl+A — go to start
					if cursor > 0 {
						fmt.Printf("\033[%dD", cursor)
						cursor = 0
					}

				case ch == 5: // Ctrl+E — go to end
					if cursor < len(buf) {
						fmt.Printf("\033[%dC", len(buf)-cursor)
						cursor = len(buf)
					}

				case ch == 127 || ch == 8: // Backspace
					if cursor > 0 {
						buf = append(buf[:cursor-1], buf[cursor:]...)
						cursor--
						fmt.Print("\b")
						redraw()
					}

				case ch == 27 && i+1 < n && b[i+1] == 27: // double-ESC: ignore second ESC
					i++ // skip the second ESC byte

				case ch == 27 && (i+2 >= n || (b[i+1] != '[' && b[i+1] != 'O')): // bare ESC — clear line
					if len(buf) > 0 {
						if cursor > 0 {
							fmt.Printf("\033[%dD", cursor)
						}
						fmt.Print("\033[K")
						buf = buf[:0]
						cursor = 0
					}

				case ch == 27 && i+2 < n && (b[i+1] == '[' || b[i+1] == 'O'): // Escape sequence
					// Handles both ANSI mode (\x1b[A) and application cursor mode (\x1bOA).
					prefix := b[i+1]
					code := b[i+2]
					i += 2
					switch code {
					case 'D': // Left arrow
						if cursor > 0 {
							cursor--
							fmt.Print("\033[D")
						}
					case 'C': // Right arrow
						if cursor < len(buf) {
							cursor++
							fmt.Print("\033[C")
						}
					case 'A': // Up arrow — history previous
						if historyIdx > 0 {
							historyIdx--
							replaceContent([]byte(history[historyIdx]))
						}
					case 'B': // Down arrow — history next
						if historyIdx < len(history)-1 {
							historyIdx++
							replaceContent([]byte(history[historyIdx]))
						} else {
							historyIdx = len(history)
							replaceContent(nil)
						}
					case '1', '7': // Home (^[[1~ or ^[[7~)
						if prefix == '[' && cursor > 0 {
							fmt.Printf("\033[%dD", cursor)
							cursor = 0
						}
					case '4', '8': // End (^[[4~ or ^[[8~)
						if prefix == '[' && cursor < len(buf) {
							fmt.Printf("\033[%dC", len(buf)-cursor)
							cursor = len(buf)
						}
					case '3': // Delete key (^[[3~)
						if prefix == '[' && cursor < len(buf) {
							buf = append(buf[:cursor], buf[cursor+1:]...)
							redraw()
						}
					}

				case ch == 9: // Tab
					current := string(buf[:cursor])
					tc := tabComplete(current)

					switch {
					case len(tc.completions) == 0:
						// No completions — do nothing

					case len(tc.completions) == 1:
						// Exact match — auto-complete the word
						newWord := tc.completions[0]
						newCurrent := tc.base + newWord
						suffix := newCurrent[len(current):]
						if len(suffix) > 0 {
							newBuf := append([]byte(newCurrent), buf[cursor:]...)
							os.Stdout.Write([]byte(suffix))
							buf = newBuf
							cursor = len(newCurrent)
							redraw()
						}

					default:
						// Multiple matches — complete to the common prefix first,
						// then open an interactive, navigable selection menu.
						if len(tc.common) > len(tc.word) {
							newCurrent := tc.base + tc.common
							suffix := newCurrent[len(current):]
							newBuf := append([]byte(newCurrent), buf[cursor:]...)
							os.Stdout.Write([]byte(suffix))
							buf = newBuf
							cursor = len(newCurrent)
							redraw()
							current = string(buf[:cursor])
						}

						// Open the selectable popup beneath the prompt. The menu
						// runs its own key loop while we stay in raw mode.
						promptLine := prompt + string(buf)
						choice, ok := selectFromMenu(tc.completions, promptLine)
						if ok {
							// Replace the word being completed with the chosen option.
							newCurrent := tc.base + choice
							// Rebuild the line: chosen completion + any text that was
							// to the right of the cursor.
							newBuf := append([]byte(newCurrent), buf[cursor:]...)
							// Redraw the whole line cleanly: return to prompt start,
							// clear, and reprint prompt + new content.
							fmt.Print("\r\033[K")
							fmt.Print(prompt)
							os.Stdout.Write(newBuf)
							buf = newBuf
							cursor = len(newCurrent)
							if cursor < len(buf) {
								fmt.Printf("\033[%dD", len(buf)-cursor)
							}
						} else {
							// Cancelled — repaint the prompt line as it was.
							fmt.Print("\r\033[K")
							fmt.Print(prompt)
							os.Stdout.Write(buf)
							if cursor < len(buf) {
								fmt.Printf("\033[%dD", len(buf)-cursor)
							}
						}
					}

				default:
					if ch >= 32 { // Printable — handles paste too
						// Insert at cursor
						newBuf := make([]byte, len(buf)+1)
						copy(newBuf, buf[:cursor])
						newBuf[cursor] = ch
						copy(newBuf[cursor+1:], buf[cursor:])
						buf = newBuf
						cursor++
						os.Stdout.Write([]byte{ch})
						redraw()
					}
				}
				i++
			}
		}
	}

	for {
		line, ok := readInput(replPrompt())
		if !ok {
			printInfo("Goodbye!")
			return
		}
		if line == "" {
			continue
		}
		if line == "exit" || line == "quit" || line == "q" {
			printInfo("Goodbye!")
			return
		}

		history = append(history, line)
		historyIdx = len(history)

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

		if active := readActiveProfile(); active != "" && active != os.Getenv("AWS_PROFILE") {
			os.Setenv("AWS_PROFILE", active)
		}
		cachedReplConfig = nil
		fmt.Println()
	}
}

// clearLine erases the current prompt+input and moves the cursor to the start.
func clearLine(prompt, current string) {
	total := len([]rune(stripANSI(prompt))) + len([]rune(current))
	fmt.Print("\r" + strings.Repeat(" ", total+2) + "\r")
	fmt.Print(prompt)
}

// stripANSI removes ANSI escape codes for length calculation.
func stripANSI(s string) string {
	var out strings.Builder
	skip := false
	for _, r := range s {
		if r == 0x1b {
			skip = true
		} else if skip && r == 'm' {
			skip = false
		} else if !skip {
			out.WriteRune(r)
		}
	}
	return out.String()
}

// selectFromMenu renders an interactive, navigable list of completion options
// below the current prompt line and lets the user pick one.
//
// The terminal is assumed to be in raw mode on entry and is left in raw mode on
// exit (the caller manages MakeRaw/Restore around the whole read loop).
//
// Controls:
//
//	Tab / ↓ / Ctrl+N   → move selection down (wraps)
//	Shift+Tab / ↑ / Ctrl+P → move selection up (wraps)
//	Enter              → accept the highlighted option
//	Esc / Ctrl+C       → cancel, return ("", false)
//
// It returns the chosen option string and true, or ("", false) if cancelled.
// promptLine is the full text currently on the prompt line (prompt+buf) so the
// menu can be drawn beneath it and cleaned up afterwards.
func selectFromMenu(options []string, promptLine string) (string, bool) {
	if len(options) == 0 {
		return "", false
	}

	sel := 0
	drawn := 0 // number of menu lines currently drawn (for cleanup)

	// draw renders the menu below the prompt line, then returns the cursor to
	// the prompt line so the caller's redraw stays correct.
	draw := func() {
		// Move to a fresh line under the prompt and clear everything below.
		fmt.Print("\r\n\033[J")
		for i, opt := range options {
			pointer := "  "
			style := Dim
			if i == sel {
				pointer = Cyan + "❯ " + Reset
				style = Bold + Cyan
			}
			fmt.Printf("%s%s%s%s\r\n", pointer, style, opt, Reset)
		}
		// Move cursor back up to the prompt line and restore it.
		fmt.Printf("\033[%dA", len(options)+1)
		fmt.Print("\r")
		fmt.Print(promptLine)
		drawn = len(options)
	}

	// clear erases the drawn menu lines below the prompt.
	clear := func() {
		if drawn == 0 {
			return
		}
		fmt.Print("\r\n\033[J") // go below prompt, clear to end of screen
		fmt.Print("\033[1A")    // back up to the prompt line
		fmt.Print("\r")
		fmt.Print(promptLine)
		drawn = 0
	}

	draw()

	b := make([]byte, 8)
	for {
		n, err := os.Stdin.Read(b)
		if err != nil || n == 0 {
			clear()
			return "", false
		}

		// Handle escape / arrow sequences.
		// Both ANSI mode (\x1b[A) and application cursor mode (\x1bOA) are handled.
		if b[0] == 27 { // ESC
			if n == 1 {
				clear()
				return "", false // bare Esc cancels
			}
			if n >= 3 && (b[1] == '[' || b[1] == 'O') {
				switch b[2] {
				case 'A': // Up
					sel = (sel - 1 + len(options)) % len(options)
					draw()
					continue
				case 'B': // Down
					sel = (sel + 1) % len(options)
					draw()
					continue
				case 'Z': // Shift+Tab (only with [ prefix)
					if b[1] == '[' {
						sel = (sel - 1 + len(options)) % len(options)
						draw()
						continue
					}
				case 'C', 'D': // Left / Right — cancel menu, let the key act on the line
					clear()
					return "", false
				}
			}
			continue
		}

		switch b[0] {
		case '\t', 14: // Tab or Ctrl+N → next
			sel = (sel + 1) % len(options)
			draw()
		case 16: // Ctrl+P → previous
			sel = (sel - 1 + len(options)) % len(options)
			draw()
		case '\r', '\n': // Enter → accept
			clear()
			return options[sel], true
		case 3: // Ctrl+C → cancel
			clear()
			return "", false
		default:
			// Any other key cancels the menu without consuming the key's
			// intent; simplest predictable behaviour is to cancel.
			clear()
			return "", false
		}
	}
}

// tabCompleteResult holds the parts of a tab-completion result.
type tabCompleteResult struct {
	base        string   // unchanged part before the word being completed
	word        string   // the word being completed (will be replaced)
	completions []string // possible completions for word
	common      string   // longest common prefix of all completions
}

// tabComplete splits current input into a base + word being completed,
// returns all matching completions and their common prefix.
func tabComplete(current string) tabCompleteResult {
	c := &replCompleter{}
	runes := []rune(current)
	results, length := c.Do(runes, len(runes))

	word := string(runes[len(runes)-length:]) // the word being replaced
	base := string(runes[:len(runes)-length]) // unchanged prefix

	var completions []string
	for _, r := range results {
		completions = append(completions, word+string(r))
	}

	// Find the longest common prefix of all completions
	common := ""
	if len(completions) > 0 {
		common = completions[0]
		for _, c := range completions[1:] {
			for len(common) > 0 && !strings.HasPrefix(c, common) {
				common = common[:len(common)-1]
			}
		}
	}

	return tabCompleteResult{base: base, word: word, completions: completions, common: common}
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

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

var replCommands = []string{
	"login", "credential", "switch", "console", "dashboard",
	"whoami", "quick", "profiles", "delete", "sessions",
	"refresh", "export", "shell", "daemon", "service",
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
	"daemon":     {"--interval"},
	"service":    {"--install", "--uninstall", "--on", "--off", "--status", "--interval"},
	"completion": {"--shell", "--install"},
}

var formatValues = []string{"env", "terraform", "docker", "json", "yaml", "credential_process"}
var shellValues = []string{"zsh", "bash", "fish", "powershell"}

// ── Tab completer ─────────────────────────────────────────────────────────────

type replCompleter struct{}

func (c *replCompleter) Do(line []rune, pos int) ([][]rune, int) {
	str := string(line[:pos])
	tokens := parseArgs(str)
	endsWithSpace := len(str) > 0 && (str[len(str)-1] == ' ' || str[len(str)-1] == '\t')

	// Current word being typed and the word before it
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

	// First word: complete command names
	isFirstWord := (!endsWithSpace && len(tokens) <= 1) || (endsWithSpace && len(tokens) == 0)
	if isFirstWord {
		return filterComplete(replCommands, prefix)
	}

	cmd := tokens[0]

	// Completing a value for a known flag
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

	// Completing a flag for the current command
	return filterComplete(replCommandFlags[cmd], prefix)
}

func filterComplete(candidates []string, prefix string) ([][]rune, int) {
	var matches [][]rune
	for _, c := range candidates {
		if strings.HasPrefix(c, prefix) {
			matches = append(matches, []rune(c[len(prefix):]))
		}
	}
	return matches, len([]rune(prefix))
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
		Prompt:            "\033[1;36mawssso\033[0m › ",
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

		// Restore terminal to cooked mode before running subprocess, so
		// interactive commands (switch, profiles, delete) can read stdin normally.
		rl.Clean()

		cmd := exec.Command(binaryPath, args...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		cmd.Run()

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

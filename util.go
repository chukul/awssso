package main

import (
	"bufio"
	"fmt"
	"os"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso/types"
	"golang.org/x/term"
)

// formatDuration formats a duration into a human-readable string like "2h 30m" or "1d 5h 10m".
func formatDuration(d time.Duration) string {
	d = d.Round(time.Minute)
	h := d / time.Hour
	d -= h * time.Hour
	m := d / time.Minute

	switch {
	case h > 24:
		days := h / 24
		h = h % 24
		return fmt.Sprintf("%dd %dh %dm", days, h, m)
	case h > 0:
		return fmt.Sprintf("%dh %dm", h, m)
	default:
		return fmt.Sprintf("%dm", m)
	}
}

// levenshteinDistance computes the edit distance between two strings (case-insensitive).
func levenshteinDistance(s1, s2 string) int {
	s1Lower := strings.ToLower(s1)
	s2Lower := strings.ToLower(s2)

	if s1Lower == s2Lower {
		return 0
	}

	m, n := len(s1Lower), len(s2Lower)
	if m == 0 {
		return n
	}
	if n == 0 {
		return m
	}

	prev := make([]int, n+1)
	curr := make([]int, n+1)

	for j := range n + 1 {
		prev[j] = j
	}

	for i := 1; i <= m; i++ {
		curr[0] = i
		for j := 1; j <= n; j++ {
			cost := 0
			if s1Lower[i-1] != s2Lower[j-1] {
				cost = 1
			}
			curr[j] = min(min(curr[j-1]+1, prev[j]+1), prev[j-1]+cost)
		}
		prev, curr = curr, prev
	}

	return prev[n]
}

// filterAccounts returns accounts whose name or ID contains the search string.
func filterAccounts(accounts []types.AccountInfo, search string) []types.AccountInfo {
	if search == "" {
		return accounts
	}
	searchLower := strings.ToLower(search)
	result := []types.AccountInfo{}
	for _, acct := range accounts {
		name := ""
		if acct.AccountName != nil {
			name = *acct.AccountName
		}
		id := ""
		if acct.AccountId != nil {
			id = *acct.AccountId
		}
		if strings.Contains(strings.ToLower(name), searchLower) || strings.Contains(id, search) {
			result = append(result, acct)
		}
	}
	return result
}

// suggestProfiles returns profile names similar to the target using Levenshtein distance.
func suggestProfiles(target string, config *AWSConfig, maxSuggestions int) []string {
	type profileScore struct {
		name  string
		score int
	}

	scores := []profileScore{}
	for name := range config.Profiles {
		distance := levenshteinDistance(target, name)
		if distance <= len(target)*4/10+2 {
			scores = append(scores, profileScore{name: name, score: distance})
		}
	}

	// Sort by distance using slices.SortFunc from stdlib
	slices.SortFunc(scores, func(a, b profileScore) int {
		return a.score - b.score
	})

	suggestions := []string{}
	limit := min(maxSuggestions, len(scores))
	for i := range limit {
		suggestions = append(suggestions, scores[i].name)
	}

	return suggestions
}

// readlineInput displays a prompt and reads one line in raw mode with basic
// line editing. Esc, Ctrl+C, and Ctrl+D all cancel and return ("", false).
// Falls back to a plain bufio read when stdin is not a terminal.
func readlineInput(prompt string) (string, bool) {
	fmt.Print(prompt)

	fd := int(os.Stdin.Fd())
	if !term.IsTerminal(fd) {
		// Non-interactive stdin (piped) — plain line read
		reader := bufio.NewReader(os.Stdin)
		line, err := reader.ReadString('\n')
		if err != nil {
			return "", false
		}
		return strings.TrimSpace(line), true
	}

	oldState, err := term.MakeRaw(fd)
	if err != nil {
		// Can't enter raw mode — fall back
		reader := bufio.NewReader(os.Stdin)
		line, readErr := reader.ReadString('\n')
		if readErr != nil {
			return "", false
		}
		return strings.TrimSpace(line), true
	}

	var buf []byte
	cursor := 0
	b := make([]byte, 32)

	redraw := func() {
		tail := buf[cursor:]
		os.Stdout.Write([]byte("\033[K"))
		os.Stdout.Write(tail)
		if len(tail) > 0 {
			fmt.Printf("\033[%dD", len(tail))
		}
	}

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

			case ch == 27 && i+2 < n && (b[i+1] == '[' || b[i+1] == 'O'): // Arrow keys
				code := b[i+2]
				i += 2
				switch code {
				case 'D': // Left
					if cursor > 0 {
						cursor--
						fmt.Print("\033[D")
					}
				case 'C': // Right
					if cursor < len(buf) {
						cursor++
						fmt.Print("\033[C")
					}
				case 'H', '1', '7': // Home
					if cursor > 0 {
						fmt.Printf("\033[%dD", cursor)
						cursor = 0
					}
				case 'F', '4', '8': // End
					if cursor < len(buf) {
						fmt.Printf("\033[%dC", len(buf)-cursor)
						cursor = len(buf)
					}
				}

			case ch == 27: // Esc — cancel
				term.Restore(fd, oldState)
				fmt.Println()
				return "", false

			case ch == 3 || ch == 4: // Ctrl+C / Ctrl+D — cancel
				term.Restore(fd, oldState)
				fmt.Println()
				return "", false

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

			default:
				if ch >= 32 { // Printable
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

// newStdinScanner returns a bufio.Scanner reading from os.Stdin.
func newStdinScanner() *bufio.Scanner {
	return bufio.NewScanner(os.Stdin)
}


// shortName extracts a short identifier from an SSO session name or URL.
func shortName(s string) string {
	s = strings.TrimPrefix(s, "https://")
	if idx := strings.Index(s, "."); idx > 0 {
		s = s[:idx]
	}
	return s
}

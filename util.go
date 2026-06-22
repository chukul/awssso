package main

import (
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/service/sso/types"
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

// shortName extracts a short identifier from an SSO session name or URL.
func shortName(s string) string {
	s = strings.TrimPrefix(s, "https://")
	if idx := strings.Index(s, "."); idx > 0 {
		s = s[:idx]
	}
	return s
}

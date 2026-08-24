package main

import (
	"fmt"
	"os"
)

// runPrompt outputs a compact profile badge for embedding in a shell prompt.
//
// macOS/Linux — add to ~/.zshrc or ~/.bashrc:
//
//	PROMPT='$(awssso prompt) '$PROMPT        # zsh
//	PS1='$(awssso prompt) '$PS1              # bash
//
// Windows (PowerShell) — add to $PROFILE:
//
//	function prompt { "$(awssso prompt) PS $($executionContext.SessionState.Path.CurrentLocation)$('>' * ($nestedPromptLevel + 1)) " }
func runPrompt() {
	profile := os.Getenv("AWS_PROFILE")
	if profile == "" {
		return // print nothing so the shell prompt is unchanged when no profile is set
	}

	mock := &AWSProfile{Name: profile}
	env := detectEnvironment(mock)
	symbol := getEnvironmentSymbol(env)

	// Output is intentionally plain — no trailing newline so it embeds cleanly in PS1
	fmt.Printf("[%s %s]", symbol, profile)
}

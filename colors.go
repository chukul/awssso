package main

import (
	"os"

	"golang.org/x/term"
)

// Color / style escape sequences.
//
// These are runtime variables (not constants) so they can be emptied when color
// output is not appropriate — e.g. output is piped/redirected, NO_COLOR is set,
// or the terminal is "dumb". Every call site references them as bare identifiers,
// so disabling color is simply a matter of setting them all to "".
//
// Defaults hold the ANSI sequences; initColors() decides whether to keep or clear
// them and is invoked once from initTerminal() at startup.
var (
	Reset     = "\033[0m"
	Bold      = "\033[1m"
	Dim       = "\033[2m"
	Underline = "\033[4m"

	Red     = "\033[31m"
	Green   = "\033[32m"
	Yellow  = "\033[33m"
	Blue    = "\033[34m"
	Magenta = "\033[35m"
	Cyan    = "\033[36m"
	White   = "\033[37m"

	BrightBlack = "\033[90m"
)

// colorEnabled reports whether the current environment reflects the resolved
// color state. It is set by initColors() and can be read by callers that want
// to branch on styling (e.g. choosing a plain glyph over a colored one).
var colorEnabled = true

// initColors resolves whether ANSI color/styling should be emitted, following
// widely adopted CLI conventions:
//
//   - NO_COLOR (any value)            → disable  (https://no-color.org)
//   - TERM=dumb                       → disable
//   - stdout is not a terminal        → disable  (piped/redirected output)
//   - FORCE_COLOR / CLICOLOR_FORCE    → force enable, overriding the above
//
// When disabled, every style variable is set to the empty string so all existing
// fmt.Printf("%s...%s", Color, ..., Reset) call sites emit clean, uncolored text.
func initColors() {
	force := os.Getenv("FORCE_COLOR") != "" || os.Getenv("CLICOLOR_FORCE") != ""

	disabled := false
	switch {
	case force:
		disabled = false
	case os.Getenv("NO_COLOR") != "":
		disabled = true
	case os.Getenv("TERM") == "dumb":
		disabled = true
	case !term.IsTerminal(int(os.Stdout.Fd())):
		disabled = true
	}

	if disabled {
		disableColors()
	}
}

// disableColors blanks every style variable so output is plain text.
func disableColors() {
	colorEnabled = false
	Reset, Bold, Dim, Underline = "", "", "", ""
	Red, Green, Yellow, Blue, Magenta, Cyan, White = "", "", "", "", "", "", ""
	BrightBlack = ""
}

//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func initTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getStdHandle := kernel32.NewProc("GetStdHandle")
	getConsoleMode := kernel32.NewProc("GetConsoleMode")

	// Enable ANSI on stdout: ENABLE_PROCESSED_OUTPUT | ENABLE_WRAP_AT_EOL_OUTPUT | ENABLE_VIRTUAL_TERMINAL_PROCESSING
	outHandle, _, _ := getStdHandle.Call(uintptr(0xfffffff5)) // STD_OUTPUT_HANDLE = -11
	if outHandle != 0 {
		setConsoleMode.Call(outHandle, uintptr(0x0001|0x0002|0x0004))
	}

	// Enable VT input on stdin so arrow keys produce ESC sequences that
	// golang.org/x/term raw mode can read. Without this, arrow keys produce
	// Windows extended codes (0xE0 prefix) which the REPL key handler never matches.
	inHandle, _, _ := getStdHandle.Call(uintptr(0xfffffff6)) // STD_INPUT_HANDLE = -10
	if inHandle != 0 {
		var mode uint32
		getConsoleMode.Call(inHandle, uintptr(unsafe.Pointer(&mode)))
		setConsoleMode.Call(inHandle, uintptr(mode)|0x0200) // add ENABLE_VIRTUAL_TERMINAL_INPUT
	}

	// Resolve color output (NO_COLOR / non-TTY / dumb terminal) after enabling
	// the Windows VT processing above.
	initColors()
}

// Spinner provides animated terminal feedback during long-running operations.
type Spinner struct {
	message string
	stop    chan struct{}
	done    chan struct{}
}

func NewSpinner(msg string) *Spinner {
	return &Spinner{
		message: msg,
		stop:    make(chan struct{}),
		done:    make(chan struct{}),
	}
}

func (s *Spinner) Start() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
		defer close(s.done)
		i := 0
		for {
			select {
			case <-s.stop:
				fmt.Print("\r\033[K")
				return
			default:
				fmt.Printf("\r%s%s%s %s...", Cyan, frames[i%len(frames)], Reset, s.message)
				i++
				time.Sleep(80 * time.Millisecond)
			}
		}
	}()
}

func (s *Spinner) Stop(success bool, completionMsg string) {
	close(s.stop)
	<-s.done // wait for goroutine to clear the line before we print
	if success {
		fmt.Printf("\r%s✓%s %s\n", Green, Reset, completionMsg)
	} else {
		fmt.Printf("\r%s✗%s %s\n", Red, Reset, completionMsg)
	}
}

func printHeader(title string) {
	// Modern, understated header: a colored accent bar precedes a bold title,
	// followed by a thin dim rule sized to the title. Avoids shouty ALL-CAPS blocks.
	fmt.Printf("\n%s▎%s%s%s %s\n", Cyan, Bold, title, Reset, Reset)
	fmt.Printf("%s%s%s\n\n", Dim, strings.Repeat("─", len([]rune(title))+2), Reset)
}

// Diagnostic output (success/info/warning) goes to stderr so stdout stays a
// clean, pipeable data stream (e.g. `awssso export --format kyaml | kubectl apply -f -`
// or `awssso completion --shell zsh > _awssso`). Errors already go to stderr.
func printSuccess(msg string) {
	fmt.Fprintf(os.Stderr, "%s✓%s %s\n", Green, Reset, msg)
}

func printInfo(msg string) {
	fmt.Fprintf(os.Stderr, "%s→%s %s\n", Cyan, Reset, msg)
}

func printWarning(msg string) {
	fmt.Fprintf(os.Stderr, "%s%s!%s %s\n", Bold, Yellow, Reset, msg)
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%s%s✗%s %s\n", Bold, Red, Reset, msg)
}

func printPrompt(msg string) {
	fmt.Printf("%s?%s %s", Magenta, Reset, msg)
}

//go:build windows

package main

import (
	"fmt"
	"os"
	"strings"
	"syscall"
	"time"
)

const (
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

func initTerminal() {
	kernel32 := syscall.NewLazyDLL("kernel32.dll")
	setConsoleMode := kernel32.NewProc("SetConsoleMode")
	getStdHandle := kernel32.NewProc("GetStdHandle")

	handle, _, _ := getStdHandle.Call(uintptr(0xfffffff5))
	if handle != 0 {
		setConsoleMode.Call(handle, uintptr(0x0001|0x0002|0x0004))
	}
}

// Spinner provides animated terminal feedback during long-running operations.
type Spinner struct {
	message string
	stop    chan struct{}
}

func NewSpinner(msg string) *Spinner {
	return &Spinner{
		message: msg,
		stop:    make(chan struct{}),
	}
}

func (s *Spinner) Start() {
	frames := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	go func() {
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
	time.Sleep(40 * time.Millisecond)
	if success {
		fmt.Printf("\r%s✔%s %s\n", Green, Reset, completionMsg)
	} else {
		fmt.Printf("\r%s✘%s %s\n", Red, Reset, completionMsg)
	}
}

func printHeader(title string) {
	fmt.Printf("\n%s%s%s%s\n", Bold, Cyan, title, Reset)
	fmt.Printf("%s%s%s\n\n", Dim, strings.Repeat("━", len(title)), Reset)
}

func printSuccess(msg string) {
	fmt.Printf("%s✔%s %s\n", Green, Reset, msg)
}

func printInfo(msg string) {
	fmt.Printf("%sℹ%s %s\n", Blue, Reset, msg)
}

func printWarning(msg string) {
	fmt.Printf("%s⚠%s %s\n", Yellow, Reset, msg)
}

func printError(msg string) {
	fmt.Fprintf(os.Stderr, "%s✘%s %s\n", Red, Reset, msg)
}

func printPrompt(msg string) {
	fmt.Printf("%s?%s %s", Yellow, Reset, msg)
}

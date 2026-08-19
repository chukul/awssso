# Changelog

Versions use the format `vMAJOR.MINOR.PATCH`. The major version follows the Fibonacci sequence: 1, 2, 3, 5, 8, 13, 21, 34, 55 …
Hotfixes and small follow-ups increment the minor or patch number within the current major.
One version entry is added per merge to `main`, written only when explicitly requested.

---

## v1.0.0 — 2026-08-19

### Features

**macOS support**
- Platform-aware activation output — shows `export AWS_PROFILE=` (Bash/Zsh) on macOS/Linux and `$env:AWS_PROFILE =` (PowerShell) on Windows
- `~/.aws/activate.sh` generated on macOS/Linux; `activate.cmd` and `activate.ps1` on Windows
- Improved private browser detection on macOS — checks both `/Applications/` and `~/Applications/` for Chrome, Firefox, and Brave; falls back via `open -a` before using the system default

**Environment colour coding**
- New `integration` tier — profiles matching `int` or `integration` shown in Magenta (🟣)
- New `sandbox` tier — profiles matching `sandbox` or `sbx` shown in White (⚪), no longer grouped under development
- OAT colour changed from Magenta to Yellow (🟡) to match team convention

**Tab completion**
- New `completion` command generates shell scripts for zsh, bash, fish, and PowerShell
- `--install` flag auto-installs and configures the script for the current user
- Profile and session names complete dynamically from `~/.aws/config`
- PowerShell: uses `Register-ArgumentCompleter`; auto-detects PowerShell on Windows via `PSModulePath`

**Interactive shell**
- Running `awssso` with no arguments (or `awssso shell`) enters a REPL session
- Built on `github.com/chzyer/readline`: up/down arrow history, left/right cursor movement, `Ctrl+R` reverse search, `Tab` completion for commands, flags, profiles, and sessions
- History persisted to `~/.aws/awssso_history` across sessions
- Falls back to a basic line-by-line loop when readline is unavailable (e.g. piped input)

**Auto-refresh**
- New `daemon` command runs a foreground refresh loop (default every 60 minutes); silently renews sessions with a cached OIDC refresh token and reports sessions that need manual login without blocking
- New `service` command installs auto-refresh as a persistent background service — launchd on macOS, Task Scheduler on Windows, cron on Linux
- `--on` / `--off` pause and resume the service without removing its configuration; `--status` shows current state
- Logs written to `~/Library/Logs/awssso-refresh.log` (macOS) and `~/.local/share/awssso/refresh.log` (Linux)

**Multi-session refresh**
- `refresh` with no arguments shows an interactive numbered picker
- Accepts single (`2`), space-separated (`1 2 3`), comma-separated (`1,3,5`), range (`1-4`), mixed (`1-3 5`), and `all`
- Session names can also be passed as positional arguments: `awssso refresh session-a session-b`
- Selected sessions refreshed in parallel

**Windows cross-platform fixes**
- `--format env` outputs `$env:` syntax on Windows; Bash `export` syntax on macOS/Linux
- `--format docker` uses backtick `` ` `` line continuation on Windows; backslash `\` on macOS/Linux
- `browser_windows.go` — switched from `exec.LookPath` to `os.Stat` for absolute paths; added `%LOCALAPPDATA%` Edge install path
- `autorefresh.go` — daemon uses `os.Interrupt` only on Windows (SIGTERM is never delivered on Windows); schtasks `/TR` value correctly quoted for paths with spaces
- `repl.go` — basic REPL fallback strips trailing `\r` from Windows CRLF line endings

**Documentation**
- `README.md` fully rewritten — every section clearly separated into macOS/Linux and Windows subsections
- `CLAUDE.md` added — mandatory rules for AI agents covering README updates, branch policy, changelog, tests, and build verification

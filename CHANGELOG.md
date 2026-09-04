# Changelog

Versions use the format `vMAJOR.MINOR.PATCH`. The major version follows the Fibonacci sequence: 1, 2, 3, 5, 8, 13, 21, 34, 55 …
Hotfixes and small follow-ups increment the minor or patch number within the current major.
One version entry is added per merge to `main`, written only when explicitly requested.

---

## v4.1.0 — 2026-09-03

### Features
- **`awssso shell` — credentialed system shell** — typing `shell` inside the REPL (or `awssso shell` directly) spawns a new bash/zsh/PowerShell session with `AWS_PROFILE` and temporary credentials already injected; `--profile <name>` targets a specific profile
- **Interactive Tab-completion menu** — in the REPL, Tab with multiple matches opens a navigable popup: `↑`/`↓` (or `Tab`/`Shift+Tab`, `Ctrl+N`/`Ctrl+P`) move the `❯` selection, `Enter` picks, `Esc`/`Ctrl+C` cancels; single unique matches still auto-complete instantly
- **`awssso recreate` — bulk profile creation** — `recreate <p1> <p2> <p3> ...` auto-matches each name to an AWS account, picks the role automatically (single role → silent; `--role <name>` pins a preferred role across all), and saves each profile using exactly the name given; `--session` pins a specific SSO identity
- **`export --format profile`** — outputs `export AWS_PROFILE="<name>"` (macOS/Linux) or `$env:AWS_PROFILE="<name>"` (Windows) with no credential fetch; works for any profile including unconfigured ones; supports `--clipboard`
- **`version` inside the REPL** — typing `version` in the interactive shell now works instead of returning `Unknown command`
- **Modern output styling** — `printHeader` uses a subtle accent bar (`▎`) + bold title + thin `─` rule; status glyphs refreshed (`✓` success, `→` info, `!` warning, `✗` error, `?` prompt)

### Fixes
- **Arrow keys in REPL** — the key handler now accepts both ANSI mode (`\x1b[A/B/C/D`) and application cursor mode (`\x1bOA/B/C/D`); previously terminals in application cursor mode silently swallowed all four arrow keys
- **Esc in REPL** — pressing Esc at the main prompt clears the current input line; pressing Esc at any sub-prompt (profile picker, role selector, group confirmation, etc.) cancels and returns to the REPL — previously Esc did nothing in most places; `chzyer/readline` dependency removed
- **`group <tag> --add p1 p2 ...`** — `--add`/`--remove` flags that appear after a positional argument were silently ignored because Go's flag parser stops at the first non-flag arg; the handler now strips them from the positional list correctly so `group favourites --add p1 p2 p3` works as expected
- **Export picker shows active profiles only** — the interactive picker for `export` and `export --clipboard` now lists only profiles with an active SSO token; expired and unconfigured profiles are hidden so every listed option can be used immediately (`--format profile` is exempt since it needs no credentials)
- **Color output honours conventions** — ANSI codes suppressed when stdout is piped/redirected, `NO_COLOR` is set, or `TERM=dumb`; `FORCE_COLOR`/`CLICOLOR_FORCE` force colour on; previously raw escapes leaked into pipes and files
- **stdout/stderr discipline** — `printSuccess`/`printInfo`/`printWarning` now write to stderr, keeping stdout a clean pipeable stream (e.g. `export --format kyaml | kubectl apply -f -` works correctly)
- **Windows REPL arrow keys** — `initTerminal` enables `ENABLE_VIRTUAL_TERMINAL_INPUT` on stdin so arrow keys produce ESC sequences compatible with `golang.org/x/term` raw mode
- **Windows `runShell` PowerShell fallback** — tries `powershell.exe` first, then `pwsh`, so both Windows PowerShell 5 and PowerShell 7 installs work
- **Production warning prompt** — replaced `fmt.Scanln` with `readlineInput` so Esc and arrow keys work at the `Continue? (y/N)` prompt
- **`init` role picker** — replaced `bufio.Scanner` with `readlineInput` so Esc cancels and arrow keys work during first-time setup
- **Exit codes** — unknown command exits `2` (usage error) per Unix convention instead of `1`

---

## v4.0.0 — 2026-09-02

### Removed
- `dashboard`, `quick`, `sessions` — removed; covered by `profiles` and the REPL
- `copy` — merged into `export --clipboard`
- `prompt` — merged into `completion --prompt` and `completion --prompt --install`
- `pin` / `unpin` — removed; use `awssso group favourites --add/--remove <profile>` instead; existing pinned data preserved in `profile_groups.json` under the `favourites` tag
- `credential` — hidden from help; still works for `credential_process` entries in `~/.aws/config`

### Changes
- `switch` renamed to `create`
- `export --clipboard` replaces `awssso copy`
- `completion --prompt` replaces `awssso prompt`; use `$(awssso completion --prompt)` in PS1
- Help text reorganised into Core / Management / Shell Integration sections
- `version` / `-v` / `--version` flag added

### Refactor
- Token status check (`profileTokenStatus`) used consistently — removed ~75 lines of duplicated inline token-read code
- AWS config cached for REPL prompt redraws

### Fixes (cross-platform)
- `pins.go` — hardcoded `/` path separator replaced with `filepath.Join` (Windows fix)
- `init.go` — `isWindows()` now uses `runtime.GOOS` instead of env-var heuristic (was unreliable under Git Bash)
- `prompt.go` — install snippets updated from `awssso prompt` to `awssso completion --prompt`
- `clipboard.go` — format auto-detection now case-insensitive (`dockerfile`, `DOCKERFILE` both match on Linux)
- REPL full line editing: ←→ cursor, Home/End/Del, copy/paste, ↑↓ history, Tab completion all work with no setup; Tab now shows only the word being completed (profile names, not full commands), auto-completes to common prefix across multiple matches, and shows flags before values; implemented with `golang.org/x/term` raw-mode (no goroutines)
- **Windows REPL arrow keys** — `initTerminal` now enables `ENABLE_VIRTUAL_TERMINAL_INPUT` on stdin so arrow keys produce ESC sequences; without this they produced Windows extended codes (`0xE0` prefix) that the key handler never matched
- **Fish completion** — subcommand list corrected to v4 command surface: removed stale `switch`, `dashboard`, `quick`, `sessions`; added `create`, `group`, `rename`, `doctor`, `init`, `completion`; added missing `--clipboard`, `--shell`, `--install`, `--prompt`, `--group` flag completions
- **PowerShell completion** — `export` was missing `--clipboard`; `completion` was missing `--prompt`
- **Zsh completion** — two spurious blank entries removed from the command list

---

## v3.0.4 — 2026-08-24

### Features
- `Makefile` added — `make build`, `make install`, `make test`, `make patch/minor/major` for version bumps
- `post-commit` git hook — automatically builds and installs `/usr/local/bin/awssso` after every commit
- `pre-push` git hook — blocks push if `README.md` or `CHANGELOG.md` were not updated in the commits being pushed; README and changelog updates are now enforced, not just requested

---

## v3.0.3 — 2026-08-24

### Features
- `awssso login --group <tag>` logs in to every unique SSO session used by profiles in the group — runs each login in sequence, opening the browser once per distinct session; supports `--private` for incognito mode

---

## v3.0.2 — 2026-08-24

### Features
- `group` command accepts multiple profiles in one command: `awssso group eks --add prof1 prof2 prof3`
- `group eks --add --profile <name>` syntax supported alongside the positional form
- Argument order is flexible — `group eks --add <profile>` and `group <profile> eks --add` both work; the tool auto-detects which arg is the group tag and which is the profile name
- Adding a profile to a non-existent group creates the group automatically — `group create` is now optional

---

## v3.0.1 — 2026-08-24

### Fixes
- `group create <tag>` now persists — data structure changed from `profile → []tags` to `tag → []profiles` so empty groups can be stored; previously `create` printed a success message but saved nothing
- `group` listing now shows empty groups with `(empty)` instead of "No groups defined"
- `group create` with no tag argument now shows a usage error instead of listing profiles in a group named "create"
- `group --add <tag>` and `group <tag> --add` (one positional arg with `--add` or `--remove`) now use `$AWS_PROFILE` as the profile name — shorthand for adding the currently active profile to a group

---

## v3.0.0 — 2026-08-24

### Features
- `awssso group` — tag profiles into named groups with `group create <tag>` / `group delete <tag>` / `group <profile> <tag> --add` / `group <profile> <tag> --remove`; `awssso profiles --group <tag>` filters any profile list to show only members of that group
- Token expiry warning in the REPL prompt and `awssso prompt` output — shows `[🔴 prod ~9m]` when less than 15 minutes remain and `[🔴 prod ⚠]` when expired
- `awssso copy` auto-detects export format from the working directory — defaults to `terraform` when `*.tf` files are present, `docker` when a `Dockerfile` is found, otherwise `env`
- `awssso whoami` silently refreshes an expired OIDC token before displaying status
- `awssso pin` with no arguments prompts to activate one of the pinned profiles directly
- `awssso unpin` with no arguments lists pinned profiles for reference before removing
- `[No SSO]` profiles are now selectable in `export`, `copy`, and `console` pickers — selecting one triggers automatic account/role configuration
- Confirmation prompt before auto-reconfiguring a profile — asks `Configure it now? (Y/n)` instead of silently modifying `~/.aws/config`
- `PRODUCT.md` added — vision, delivered feature registry, prioritised backlog, acceptance criteria, and QA checklist for macOS and Windows
- Agent roles defined in `CLAUDE.md` — Stakeholder, Product Owner, QA Engineer, and Developer with explicit responsibilities

### Fixes
- All interactive prompts now use readline — arrow keys, history, and Ctrl+A/E work everywhere inside the REPL; prompts no longer silently exit when run as subprocesses inside the shell
- Pre-commit hook blocks direct commits to `main` and prints a clear error with the correct branch command
- `profiles` command showed nothing inside the REPL — `bufio.Scanner` was receiving an empty read from readline-managed stdin; fixed by switching to `readlineInput`

---

## v2.0.3 — 2026-08-24

### Features
- `awssso pin` / `awssso unpin` with no arguments list all currently pinned profiles, showing each one with its environment symbol and colour
- `awssso export` and `awssso copy` with no `--profile` flag show an interactive profile picker — all profiles are listed including `[No SSO]` ones, so any profile can be targeted directly
- `export` / `copy` / `console` picker now includes unconfigured `[No SSO]` profiles; selecting one auto-configures the account and role before proceeding

### Fixes
- Selecting a `[No SSO]` profile in `awssso profiles` no longer activates a broken `AWS_PROFILE` — it auto-detects the matching AWS account by name, prompts for a role, saves the configuration, and only then activates the profile so credentials are valid immediately

---

## v2.0.2 — 2026-08-24

### Fixes
- **Multiple REPL sessions no longer interfere** — each session now writes its active profile to a session-specific file (`active_profile_<pid>`) instead of a shared one; switching profiles in one terminal window no longer changes the prompt badge in another
- **Sync files are cleaned up automatically** — the file is removed when the REPL exits normally (via `exit` or Ctrl+D); stale files from crashed or killed sessions are deleted on the next REPL startup
- Cross-platform dead-process detection: Unix uses signal 0, Windows uses `FindProcess`

---

## v2.0.1 — 2026-08-24

### Features
- `awssso prompt --install` — automatically patches the shell config file (zsh, bash, fish, PowerShell) so the profile badge appears without any manual editing; idempotent and safe to re-run
- `awssso prompt` now outputs a coloured badge (`[🔴 prod]`, `[🟢 dev]`, etc.) using the same ANSI colour constants used everywhere else in the tool; previously only the emoji was shown

### Fixes
- REPL prompt badge now updates immediately after switching profiles — previously the profile name was inherited at startup and never changed because child processes cannot propagate environment variables back to the parent; fixed by writing the active profile to `~/.aws/sso/active_profile` on every selection and reading it back after each subprocess exits
- `awssso prompt --install` completion: `--install` flag is now included in tab completion for the `prompt` command

---

## v2.0.0 — 2026-08-24

### Features
- `awssso init` — first-time setup wizard: prompts for SSO URL and region, opens a browser, then guides through account and role selection, saving everything to `~/.aws/config`
- `awssso copy` — fetches credentials and copies them to the system clipboard in any export format (macOS: `pbcopy`; Windows: PowerShell `Set-Clipboard`; Linux: `xclip` / `xsel` / `wl-copy`)
- `awssso assume` — assumes an IAM role via STS on top of the credentials from any existing profile; supports all export formats
- `awssso doctor` — health check that validates `~/.aws/config`, checks token status per session, detects orphaned profiles, and verifies the binary is on PATH
- `awssso prompt` — outputs a compact profile badge (`[🔴 prod]`) for embedding in shell PS1/PROMPT on macOS, Linux, and Windows PowerShell
- `awssso rename <old> <new>` — renames a profile in `~/.aws/config`, updating `credential_process` references automatically
- `awssso pin <name>` / `awssso unpin <name>` — pins profiles to the top of every list and picker; stored in `~/.aws/sso/pinned_profiles.json`
- `--format kyaml` — new export format that outputs a Kubernetes `Secret` YAML for direct `kubectl apply` piping
- REPL prompt now shows the active `$AWS_PROFILE` with its environment colour (`awssso [🔴 prod] ›`) and updates live as the profile changes
- Desktop notifications — daemon sends OS-native alerts (macOS: `osascript`, Windows: PowerShell balloon, Linux: `notify-send`) when sessions expire and cannot be auto-refreshed

### Removed
- `daemon` and `service` commands removed — auto-refresh functionality has been disabled
- `assume` command removed — STS role chaining is not compatible with this organisation's SSO setup

### Fixes
- **Spinner race condition** — `Stop()` now waits for the spinner goroutine to clear the line before printing the result; previously a 40 ms sleep was too short, causing garbled output like `tokenntials...`
- **Duplicate credentials in WHAT'S NEXT** — `runSwitch` called from auto-recovery (`resolveCredentials`) now skips the WHAT'S NEXT menu; credentials were being shown twice when `export` triggered a switch
- **Duplicate error messages** — `resolveCredentials` now checks profile completeness directly instead of through `loadProfile`, eliminating the duplicate "missing account/role" messages that appeared side by side
- **Arrow keys in interactive prompts** — free-text prompts (`switch` profile name, account search, SSO URL/region) now use readline in raw mode so left/right arrows, backspace, and Ctrl+A/E work correctly; previously showed `^[[D^[[C` literal escape sequences
- **Unknown command "1"/"2" in REPL** — WHAT'S NEXT numbered items (`1.`, `2.`, `3.`) changed to arrow bullets (`→`) to prevent users from typing them as commands; REPL also now validates commands before spawning a subprocess
- **fetchAccounts 401 after reconfiguration** — when an existing SSO token is cached but rejected by AWS (e.g. after a forced sign-out), `runSwitch` now detects the 401 `UnauthorizedException`, invalidates the token, and re-authenticates automatically instead of exiting with an error
- **Auto account matching** — `export` and `credential` on an unconfigured profile now search for an AWS account whose name matches the profile name, selecting it automatically and skipping the 587-account search in the common case
- **SSO URL and region pre-fill** — profile configuration wizard now reads the URL and region from any existing session as defaults, so users can press Enter instead of re-typing the same values
- **`resolveCredentials` auto-recovery** — expired token and missing account/role are now recovered automatically without requiring the user to run a separate command
- **`rl.Clean()` cursor corruption** — removed the `rl.Clean()` call between readline commands; calling it after `Readline()` had already exited raw mode was writing ANSI sequences that corrupted the terminal's cursor reference point, breaking left/right arrow movement in the next prompt
- **zsh completion missing `kyaml`** — `kyaml` was present in bash, fish, and PowerShell completion scripts but missing from the zsh `--format` list
- **PowerShell completion install on macOS** — `awssso completion --shell powershell --install` now checks that `powershell` or `pwsh` is on PATH before attempting to auto-install; on macOS without PowerShell, a clear message is shown instead of a confusing warning
- **Cron entry path quoting** — binary and log paths in the Linux cron entry are now quoted, preventing failures when either path contains spaces
- **`schtasks /TR` quoting** — Windows Task Scheduler entry now correctly quotes the binary path when it contains spaces

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

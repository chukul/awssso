# AWS SSO Credential Process Helper (Improved)

A fast CLI tool for AWS SSO authentication and credential management. Provides interactive workflows for logging in, switching accounts/roles, managing profiles, and exporting credentials for DevOps tools.

## Improvements Over Original

This is a refactored version with the following improvements applied:

### 1. Code Structure & Organization
- **Split `main.go`** into focused files: `commands.go` (command handlers), `util.go` (utility functions), keeping `main.go` slim (CLI setup + usage only)
- Each file has a clear, single responsibility
- Shared helper `resolveValidToken` eliminates repeated token-load-refresh boilerplate across commands
- Centralized AWS SDK client factories (`newSSOClient`, `newOIDCClient`, `newAWSConfig`) in `sso.go`

### 2. Consistent Error Handling
- All error output now uses `printError()` / `printInfo()` / `printWarning()` consistently
- No more mixing raw `fmt.Fprintf(os.Stderr, ...)` with pretty-printed helpers

### 3. Removed Redundant `min()` Function
- Go 1.21+ has a built-in `min()` — the custom shadow is removed
- Manual insertion sorts replaced with stdlib `slices.Sort` / `slices.SortFunc`

### 4. Platform Portability (Build Tags)
- `ui.go` uses `//go:build windows` for Windows-specific terminal init
- `ui_other.go` provides a no-op `initTerminal()` for macOS/Linux
- `browser_windows.go` / `browser_other.go` abstract browser opening per platform
- Both `openBrowser(url)` and `openBrowserPrivate(url)` are platform-specific
- Private mode tries Edge InPrivate → Chrome Incognito → Firefox Private → fallback
- The tool now compiles and runs on any OS

### 5. Improved Test Coverage
- `loadAWSConfigFromPath(path)` is testable without touching real `~/.aws/config`
- Tests for: config parsing, token expiry, hash path computation, environment detection, export formatting, Levenshtein distance, account filtering, profile suggestions, selection parsing
- Fixed the original test which had dead code and an incorrect expected hash

### 6. Performance: Cached `os.UserHomeDir()`
- A `homeDir()` function caches the user home directory to avoid repeated syscalls
- All file-path resolution functions use the cached version

### 6. Race Condition Awareness
- Sessions are deduplicated by key in `refreshAllSessions` before parallel refresh
- Each session maps to a unique token file path, so parallel goroutines don't race on the same file

### 7. Context Timeouts Added
- `runLogin`: 5 minute timeout
- `runSwitch`: 5 minute timeout  
- `runRefresh`: 5 minute timeout
- `runCredential`, `runExport`: 30 second timeout
- `runConsole`: 45 second timeout

### 8. Fixed `credential_process` Path Quoting
- Paths and profile names are now quoted to handle spaces correctly:
  ```ini
  credential_process = "C:\\Program Files\\awssso.exe" credential --profile "my profile"
  ```

### 9. Fixed Typo
- "OIDc" → "OIDC" in the refresh header

### 10. Removed Orphaned `package.json`
- Not included in the improved project (it only contained an unrelated AI tool dependency)

### 11. Dashboard Uses Lipgloss Consistently
- Removed raw ANSI constants from the bubbletea `View()` method
- All styling in the dashboard now uses lipgloss styles for consistent rendering

### 12. Signal Handling Note
- Production warning prompts use `showProductionWarning()` which returns false on non-"yes" input (including EOF from Ctrl+C)

## File Structure

```
improved/
├── main.go              # CLI entry point, flag parsing, usage text
├── commands.go          # Command handlers + shared helpers (resolveValidToken, loadProfile, etc.)
├── awsconfig.go         # AWS config parsing, token cache, profile CRUD, homeDir cache
├── sso.go              # SSO OIDC login, token refresh, role credentials, console, SDK client factories
├── cloudeng.go         # Environment detection, recent profiles, export formats
├── dashboard.go        # Interactive TUI dashboard (bubbletea)
├── util.go             # Utility functions (formatting, Levenshtein, filtering)
├── ui.go               # Terminal UI helpers (Windows, build-tagged)
├── ui_other.go         # Terminal UI helpers (non-Windows, build-tagged)
├── browser_windows.go  # Browser open + InPrivate (Windows)
├── browser_other.go    # Browser open + incognito (macOS/Linux)
├── awsconfig_test.go   # Tests for config parsing, token paths, multi-session
├── cloudeng_test.go    # Tests for environment detection and export
├── util_test.go        # Tests for utilities
├── go.mod
└── go.sum
```

## Build

```powershell
go build -o awssso.exe
```

## Commands

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO and cache credentials |
| `switch` | Interactively select an AWS account/role and create a profile |
| `credential` | Output temporary AWS credentials (for `credential_process`) |
| `console` | Open AWS Management Console in browser, pre-authenticated |
| `whoami` | Display current identity and SSO token status |
| `profiles` | List, inspect, and manage AWS SSO profiles |
| `sessions` | List all SSO sessions with identity info and status |
| `refresh` | Refresh expired (or valid with `--force`) SSO tokens |
| `quick` | Quickly switch between recently used profiles |
| `export` | Export credentials for env, terraform, docker, json, yaml |
| `dashboard` | Interactive TUI session management dashboard |

## Multi-Identity / Multi-Session Support

A key feature of this improved version: you can have multiple SSO sessions pointing to the **same Start URL** but authenticating as **different email identities**.

### Configuration

```ini
[sso-session team-alpha]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = alice@company.com

[sso-session team-beta]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = bob@company.com

[profile alpha-dev]
sso_session = team-alpha
sso_account_id = 111111111111
sso_role_name = DeveloperAccess
region = eu-west-1

[profile beta-prod]
sso_session = team-beta
sso_account_id = 222222222222
sso_role_name = AdminAccess
region = us-east-1
```

### How It Works

- Each `[sso-session]` gets its own token cache (SHA1 of the session **name**, not the URL)
- The optional `sso_account_email` field tells the CLI which identity belongs to which session
- During login, the CLI shows which identity you should authenticate as
- You can be logged into both sessions simultaneously

### Usage

```powershell
# Login to a specific session (shows identity hint)
awssso login --session team-alpha

# Login in private/incognito browser (recommended for multi-identity)
awssso login --session team-alpha --private

# Refresh a specific session
awssso refresh --session team-beta

# Refresh in private browser if re-auth is needed
awssso refresh --session team-beta --private

# List all sessions with identity and shared-URL info
awssso sessions

# Login via profile (automatically resolves the correct session)
awssso login --profile alpha-dev

# Switch accounts using a specific identity in private mode
awssso switch --session team-alpha --private
```

### Tips for Multi-Identity Workflows

1. **Use `--private` flag**: Opens incognito/InPrivate window — no cached cookies means you always get a fresh login prompt for the correct identity
2. **Use browser profiles**: Alternatively, create a separate Chrome/Edge profile for each AWS identity
3. **Track identities**: Add `sso_account_email` to your `[sso-session]` blocks — the CLI will remind you which identity to use
4. **Session independence**: Token caches are separate (hashed by session name), so logging into one session won't affect the other
5. **Combine flags**: `awssso login --session team-alpha --private` gives you both identity hint and clean browser in one command

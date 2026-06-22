# Tech Stack

## Language & Runtime

- Go 1.26.4
- Single-binary CLI (no runtime dependencies)

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2` | AWS SDK (SSO, SSOOIDC, STS, Signin services) |
| `github.com/charmbracelet/bubbletea` | Interactive TUI (dashboard) |
| `github.com/charmbracelet/lipgloss` | Terminal styling for TUI views |
| `golang.org/x/sys` | Low-level OS interaction (Windows console mode) |

## Build & Test Commands

```powershell
# Build (from improved/)
go build -o awssso.exe

# Run all tests
go test ./...

# Run tests with verbose output
go test -v ./...

# Run a specific test
go test -v -run TestLoadAWSConfigFromPath ./...

# Static analysis
go vet ./...
```

## Project Layout

There are two versions in this repo:
- **Root directory** (`/`): Original monolithic implementation (legacy, not actively developed)
- **`/improved`**: Refactored version with proper file separation — this is the active development target

The `improved/` directory is the active development target. Use module name `awssso` (defined in `improved/go.mod`).

## Platform Build Tags

- `//go:build windows` — `ui.go`, `browser_windows.go` (Windows terminal init, browser open via `rundll32`, InPrivate via `msedge`/`chrome`)
- `//go:build !windows` — `ui_other.go`, `browser_other.go` (no-op terminal init, `xdg-open`/`open` for browser, incognito via Chrome/Firefox)

## Code Conventions

- Use `homeDir()` (cached) instead of raw `os.UserHomeDir()` — avoids redundant syscalls
- Use `newSSOClient(region)` / `newOIDCClient(region)` / `newAWSConfig(region)` to create AWS SDK clients — centralizes SDK configuration
- Use `resolveValidToken(ctx, profile, config)` to load + validate + auto-refresh a token — avoids repeating the load→read→check→refresh boilerplate
- Use `openBrowser(url)` for default browser, `openBrowserPrivate(url)` for incognito/InPrivate mode
- Use stdlib `slices.Sort` / `slices.SortFunc` for sorting (Go 1.21+ builtins available)
- Use `printError()` / `printInfo()` / `printWarning()` / `printSuccess()` for all user-facing output — never raw `fmt.Fprintf(os.Stderr, ...)`
- Use `NewSpinner(msg)` for long-running operations with visual feedback
- All context-bound operations should use `context.WithTimeout` with appropriate timeouts

## Testing Approach

- Standard `testing` package (no third-party test framework)
- Table-driven tests preferred
- Testable config loading via `loadAWSConfigFromPath(path)` to avoid touching real `~/.aws/config`
- Tests cover: config parsing, token expiry, hash path computation, environment detection, export formatting, Levenshtein distance, account filtering, profile suggestions, selection parsing

## No External Build Tools

No Makefile, task runner, or CI config. Build and test via `go` toolchain directly.

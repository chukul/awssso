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
# Build
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

## Platform Build Tags

- `//go:build windows` — `ui.go`, `browser_windows.go`
- `//go:build !windows` — `ui_other.go`, `browser_other.go`

## Code Conventions

- Use `homeDir()` (cached) instead of raw `os.UserHomeDir()`
- Use `newSSOClient(region)` / `newOIDCClient(region)` to create AWS SDK clients
- Use `resolveValidToken(ctx, profile, config)` to load + validate + auto-refresh a token
- Use `needsPrivateBrowser(config, profile)` to determine if private mode is needed
- Use `openBrowser(url)` for default browser, `openBrowserPrivate(url)` for incognito
- Use stdlib `slices.Sort` / `slices.SortFunc` for sorting
- Use `printError()` / `printInfo()` / `printWarning()` / `printSuccess()` for all user output
- Use `NewSpinner(msg)` for long-running operations
- All context-bound operations should use `context.WithTimeout`
- Production warnings should be calm and single-line, not alarming
- Interactive pickers should group items by session and sort by environment priority

## Testing Approach

- Standard `testing` package (no third-party test framework)
- Table-driven tests preferred
- Testable config loading via `loadAWSConfigFromPath(path)`
- Tests cover: config parsing, token expiry, hash paths, environment detection, export formatting, Levenshtein distance, account filtering, profile suggestions, selection parsing

## No External Build Tools

No Makefile, task runner, or CI config. Build and test via `go` toolchain directly.

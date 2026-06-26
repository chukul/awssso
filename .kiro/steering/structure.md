# Project Structure

All source files live at the repository root. Single `main` package compiling to one binary.

```
awssso/
├── main.go              # CLI entry point: flag parsing, command dispatch, usage text
├── commands.go          # Command handlers + shared helpers
├── awsconfig.go         # AWS config parsing, token cache, profile/session CRUD, homeDir cache
├── sso.go              # SSO OIDC login, token refresh, credentials, console, SDK client factories
├── cloudeng.go         # Environment detection, recent profiles, export formats, production warning
├── dashboard.go        # Interactive TUI dashboard (bubbletea)
├── util.go             # Pure utility functions (formatting, Levenshtein, filtering, parsing)
├── ui.go               # Terminal UI helpers — Windows (build-tagged)
├── ui_other.go         # Terminal UI helpers — non-Windows (build-tagged)
├── browser_windows.go  # openBrowser + openBrowserPrivate — Windows
├── browser_other.go    # openBrowser + openBrowserPrivate — macOS/Linux
├── awsconfig_test.go   # Tests for config parsing, token paths, multi-session
├── cloudeng_test.go    # Tests for environment detection and export
├── util_test.go        # Tests for utility functions
├── go.mod / go.sum     # Module definition and dependency lock
├── README.md           # Project documentation
├── .gitignore          # Excludes binaries, IDE files, codegraph
└── .kiro/steering/     # AI steering rules
```

## File Responsibilities

| File | Responsibility |
|------|---------------|
| `main.go` | CLI setup — flag sets, dispatch, usage text only |
| `commands.go` | Command handlers + helpers: `resolveValidToken`, `needsPrivateBrowser`, `loadProfile`, `pickProfileForConsole` |
| `awsconfig.go` | INI parsing/writing; token cache; `homeDir()` cache; `SSOSession.LoginPrivate` field |
| `sso.go` | AWS SSO/OIDC network calls; `newSSOClient`/`newOIDCClient` factories; `loginSSOWithHint` |
| `cloudeng.go` | `detectEnvironment`, `showProductionWarning`, `recordRecentProfile`, `exportCredentials` |
| `util.go` | Generic helpers (no AWS knowledge): formatting, Levenshtein, filtering |
| `browser_*.go` | Platform-specific: `openBrowser` + `openBrowserPrivate` (Edge/Chrome/Firefox detection) |
| `dashboard.go` | Self-contained bubbletea TUI |

## Key Shared Helpers

| Function | Location | Purpose |
|----------|----------|---------|
| `resolveValidToken` | `commands.go` | Load + validate + auto-refresh SSO token |
| `needsPrivateBrowser` | `commands.go` | Auto-detect if profile needs private browser (multi-identity) |
| `pickProfileForConsole` | `commands.go` | Grouped interactive profile picker with status |
| `newSSOClient` / `newOIDCClient` | `sso.go` | Centralized AWS SDK client creation |
| `homeDir` | `awsconfig.go` | Cached `os.UserHomeDir()` |
| `loginSSOWithHint` | `sso.go` | SSO device auth with session/email hints + private flag |
| `showProductionWarning` | `cloudeng.go` | Calm single-line production confirmation |

## Architecture

Single `main` package. File separation by domain concern. No internal packages.

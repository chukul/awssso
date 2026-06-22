# Project Structure

## Root-Level Codebase

All source files live at the repository root. Single `main` package compiling to one binary.

```
awssso/
├── main.go              # CLI entry point: flag parsing, command dispatch, usage text
├── commands.go          # Command handlers + shared helpers (resolveValidToken, etc.)
├── awsconfig.go         # AWS config file parsing, token cache, profile/session CRUD, homeDir cache
├── sso.go              # SSO OIDC login, token refresh, role credentials, console URL, SDK client factories
├── cloudeng.go         # Environment detection, recent profiles, credential export formats
├── dashboard.go        # Interactive TUI dashboard (bubbletea model/view/update)
├── util.go             # Pure utility functions (formatting, Levenshtein, filtering, parsing)
├── ui.go               # Terminal UI helpers — Windows (build-tagged)
├── ui_other.go         # Terminal UI helpers — non-Windows (build-tagged)
├── browser_windows.go  # Browser open + private/InPrivate — Windows (build-tagged)
├── browser_other.go    # Browser open + private/incognito — macOS/Linux (build-tagged)
├── awsconfig_test.go   # Tests for config parsing, token paths, multi-session
├── cloudeng_test.go    # Tests for environment detection and export
├── util_test.go        # Tests for utility functions
├── go.mod / go.sum     # Module definition and dependency lock
├── README.md           # Project documentation
├── .gitignore          # Excludes binaries, IDE files, codegraph
└── .kiro/steering/     # AI steering rules
```

## File Responsibilities

| File | Single Responsibility |
|------|----------------------|
| `main.go` | Only CLI setup — flag sets, dispatch switch, usage text |
| `commands.go` | All user-facing command logic; shared helpers like `resolveValidToken` |
| `awsconfig.go` | INI parsing/writing of `~/.aws/config`; token cache read/write; `homeDir()` cache |
| `sso.go` | Network calls to AWS SSO/OIDC endpoints; `newSSOClient`/`newOIDCClient` factories |
| `cloudeng.go` | Non-network utilities specific to cloud engineering workflows |
| `util.go` | Generic helpers with no AWS or domain knowledge |
| `ui.go` / `ui_other.go` | Terminal output formatting, spinner, ANSI constants |
| `browser_windows.go` | `openBrowser` (default) + `openBrowserPrivate` (Edge InPrivate → Chrome Incognito → Firefox Private) |
| `browser_other.go` | Same API, macOS/Linux implementations |
| `dashboard.go` | Self-contained bubbletea TUI |

## Key Shared Helpers

| Function | Location | Purpose |
|----------|----------|---------|
| `resolveValidToken` | `commands.go` | Load + validate + auto-refresh SSO token in one call |
| `newSSOClient` / `newOIDCClient` | `sso.go` | Centralized AWS SDK client creation |
| `homeDir` | `awsconfig.go` | Cached `os.UserHomeDir()` — use instead of calling directly |
| `loadProfile` | `commands.go` | Lookup profile + validate SSO config + suggest alternatives |
| `openBrowser` | `browser_*.go` | Open URL in default browser |
| `openBrowserPrivate` | `browser_*.go` | Open URL in incognito/InPrivate window |
| `loginSSOWithHint` | `sso.go` | SSO device auth with session/email hints + private browser support |

## Architecture Pattern

Single `main` package — no internal packages or sub-modules. All code compiles to one binary. File separation is by domain concern, not by Go package boundary.

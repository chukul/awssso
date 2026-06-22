# awssso — AWS SSO Credential Process Helper

A fast, single-binary CLI tool for AWS SSO authentication and credential management. Built for cloud engineers who manage multiple AWS accounts and identities via SSO.

## Features

- **Multi-identity SSO** — multiple sessions sharing one start URL, each with a different email identity
- **Private browser mode** (`--private`) — opens incognito/InPrivate window for clean auth flows
- **Interactive account/role switching** — browse accounts, pick roles, auto-create profiles
- **`credential_process` compatible** — use as a credential helper in `~/.aws/config`
- **AWS Console federation** — open the Management Console pre-authenticated
- **Credential export** — output credentials for Terraform, Docker, JSON, YAML, or shell env vars
- **Smart token refresh** — OIDC `refresh_token` when available, device auth fallback otherwise
- **Interactive TUI dashboard** — real-time session status with keyboard-driven refresh/login
- **Environment detection** — color-coded profiles (🔴 prod, 🟡 staging, 🟢 dev) with production safety prompts
- **Profile suggestions** — typo-tolerant profile name matching via Levenshtein distance
- **Cross-platform** — Windows, macOS, and Linux via build tags

## Installation

```bash
# Clone and build
git clone https://github.com/chukul/awssso.git
cd awssso
go build -o awssso.exe   # Windows
go build -o awssso       # macOS/Linux
```

Place the binary anywhere in your `PATH`.

### Requirements

- Go 1.21+ (uses built-in `min()` and `slices` package)
- AWS SSO configured in `~/.aws/config`

## Quick Start

```powershell
# 1. Login to your SSO profile
awssso login --profile my-profile

# 2. Check your identity
awssso whoami

# 3. Switch to a different account/role
awssso switch

# 4. Export credentials for Terraform
awssso export --profile prod --format terraform

# 5. Open AWS Console directly
awssso console --profile my-profile
```

## Commands

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO and cache credentials |
| `switch` | Interactively select an AWS account/role and create a profile |
| `credential` | Output temporary AWS credentials in JSON (`credential_process` format) |
| `console` | Open AWS Management Console in browser, pre-authenticated |
| `whoami` | Display current profile, account, role, and SSO token status |
| `profiles` | List all profiles and set one as active (`AWS_PROFILE`) |
| `delete` | Delete one or more profiles (interactive or by name) |
| `sessions` | List all SSO sessions with identity info and token status |
| `refresh` | Refresh expired SSO tokens (all, by profile, or by session) |
| `quick` | Quick switch between recently used profiles |
| `export` | Export credentials in multiple formats |
| `dashboard` | Interactive TUI session management dashboard |

## Options

| Flag | Applies To | Description |
|------|-----------|-------------|
| `--profile <name>` | Most commands | AWS profile name (defaults to `$AWS_PROFILE` or `default`) |
| `--session <name>` | `login`, `switch`, `refresh` | Target a specific SSO session (multi-identity) |
| `--private` | `login`, `switch`, `refresh` | Open browser in incognito/InPrivate mode |
| `--format <fmt>` | `export` | Output format: `env`, `terraform`, `docker`, `json`, `yaml`, `credential_process` |
| `--force` | `refresh` | Refresh even valid tokens (proactive refresh) |

## Configuration

Profiles must have SSO configuration in `~/.aws/config` using one of these patterns:

### Using `sso_session` (recommended)

```ini
[sso-session my-sso]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1

[profile my-profile]
sso_session = my-sso
sso_account_id = 123456789012
sso_role_name = AdministratorAccess
region = eu-west-1
```

### Using inline SSO settings (legacy, auto-migrated)

```ini
[profile my-profile]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1
sso_account_id = 123456789012
sso_role_name = AdministratorAccess
region = eu-west-1
```

When creating new profiles via `switch` or `login`, the tool automatically converts inline SSO to `sso-session` format.

### Using as `credential_process`

```ini
[profile my-profile]
credential_process = "C:\path\to\awssso.exe" credential --profile "my-profile"
```

The `credential` command outputs JSON compatible with the AWS SDK credential process protocol. Tokens are auto-refreshed transparently.

## Multi-Identity / Multi-Session Support

For teams where multiple people (or the same person with multiple roles) share one SSO start URL but need to authenticate as different identities:

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

1. Each `[sso-session]` gets its own token cache (SHA1 hash of session **name**, not URL)
2. The `sso_account_email` field tells the CLI which identity belongs to which session
3. During login, the CLI displays which email you should authenticate as
4. You can be logged into multiple sessions simultaneously

### Usage

```powershell
# Login to a specific session in private browser (recommended)
awssso login --session team-alpha --private

# Login to another identity
awssso login --session team-beta --private

# Switch accounts using a specific identity
awssso switch --session team-alpha --private

# List all sessions with identity and status info
awssso sessions

# Refresh a specific session
awssso refresh --session team-beta --private --force
```

### Why `--private` Matters

Without `--private`, your default browser opens with existing cookies and may auto-authenticate as the wrong identity. With `--private`:
- A fresh incognito/InPrivate window opens with no cached session
- You get a clean login prompt where you choose the correct email
- Each session gets an isolated auth flow

Browser detection order:
- **Windows**: Edge (InPrivate) → Chrome (Incognito) → Firefox (Private) → default browser
- **macOS**: Chrome (Incognito) → Firefox (Private) → default browser
- **Linux**: Chrome/Chromium (Incognito) → Firefox (Private) → xdg-open

## Export Formats

```powershell
# Shell environment variables (PowerShell/Bash)
awssso export --profile my-profile --format env

# Terraform environment variables
awssso export --profile my-profile --format terraform

# Docker environment (for docker run --env-file)
awssso export --profile my-profile --format docker

# Raw JSON
awssso export --profile my-profile --format json

# YAML
awssso export --profile my-profile --format yaml

# credential_process config line
awssso export --profile my-profile --format credential_process
```

## Interactive Dashboard

```powershell
awssso dashboard
```

A full-screen TUI showing all SSO sessions with real-time status. Keyboard shortcuts:
- `↑`/`↓` — navigate sessions
- `r` — refresh selected session
- `l` — login to selected session
- `q` / `Esc` — quit

## Project Structure

```
├── main.go              # CLI entry point: flag parsing, command dispatch, usage
├── commands.go          # Command handlers + shared helpers (resolveValidToken, etc.)
├── awsconfig.go         # ~/.aws/config parsing, token cache, profile/session CRUD
├── sso.go              # AWS SSO OIDC login, token refresh, role credentials, console
├── cloudeng.go         # Environment detection, recent profiles, export formats
├── dashboard.go        # Interactive TUI dashboard (bubbletea)
├── util.go             # Utility functions (formatting, Levenshtein, filtering)
├── ui.go               # Terminal UI helpers (Windows, build-tagged)
├── ui_other.go         # Terminal UI helpers (macOS/Linux, build-tagged)
├── browser_windows.go  # Browser open + InPrivate (Windows)
├── browser_other.go    # Browser open + incognito (macOS/Linux)
├── *_test.go           # Tests (config parsing, token paths, env detection, utilities)
├── go.mod / go.sum     # Module definition
└── .kiro/steering/     # AI assistant steering rules
```

## Development

```bash
# Build
go build -o awssso.exe

# Run tests
go test ./...

# Static analysis
go vet ./...
```

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2` | AWS SDK (SSO, SSOOIDC, Signin) |
| `github.com/charmbracelet/bubbletea` | Interactive TUI framework |
| `github.com/charmbracelet/lipgloss` | Terminal styling |

## License

Internal tool — not publicly licensed.

# awssso — AWS SSO Credential Process Helper

A fast, single-binary CLI tool for AWS SSO authentication and credential management. Built for cloud engineers who manage multiple AWS accounts and identities via SSO.

---

## Features

- **Multi-identity SSO** — multiple sessions sharing one start URL, each with a different email identity
- **Private browser mode** (`--private`) — opens incognito/InPrivate window for clean auth flows
- **Interactive account/role switching** — browse accounts, pick roles, auto-create profiles; auto-matches account by name
- **`credential_process` compatible** — use as a credential helper in `~/.aws/config`
- **AWS Console federation** — open the Management Console pre-authenticated
- **Credential export** — Terraform, Docker, JSON, YAML, shell env vars, Kubernetes Secret (`kyaml`)
- **Copy to clipboard** — `awssso copy` copies credentials in any format directly to the clipboard
- **Role chaining** — `awssso assume` uses STS AssumeRole on top of any existing profile
- **Smart token refresh** — OIDC `refresh_token` when available, auto-re-login fallback otherwise
- **Interactive TUI dashboard** — real-time session status with keyboard-driven refresh/login
- **Interactive shell** — `awssso` with no args drops into a REPL with readline history and tab completion
- **Tab completion** — zsh, bash, fish, PowerShell; profiles and session names complete dynamically
- **Auto-refresh service** — daemon and system service (launchd / Task Scheduler / cron) with on/off toggle
- **Environment detection** — color-coded profiles (🔴 prod, 🟡 staging/oat, 🟣 int, ⚪ sandbox, 🟢 dev)
- **Profile management** — rename, pin/unpin favourites, health check (`doctor`), first-time wizard (`init`)
- **Shell prompt integration** — `awssso prompt` outputs a badge for PS1/PROMPT embedding
- **Desktop notifications** — alerts when sessions need manual login (macOS, Windows, Linux)

---

## Installation

### Requirements

- Go 1.21+
- AWS SSO configured in `~/.aws/config`

### macOS / Linux

```bash
git clone https://github.com/chukul/awssso.git
cd awssso
go build -o awssso .
sudo mv awssso /usr/local/bin/
```

### Windows

```powershell
git clone https://github.com/chukul/awssso.git
cd awssso
go build -o awssso.exe .
# Move awssso.exe to a directory listed in your $env:PATH
```

---

## Commands

These commands work on all platforms. Examples below show the platform-appropriate shell syntax.

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO and cache credentials |
| `switch` | Interactively select an AWS account/role and create a profile |
| `credential` | Output temporary AWS credentials in JSON (`credential_process` format) |
| `console` | Open AWS Management Console in browser, pre-authenticated |
| `whoami` | Display current profile, account, role, and SSO token status |
| `profiles` | List all profiles and set one as active |
| `delete` | Delete one or more profiles (interactive or by name) |
| `sessions` | List all SSO sessions with identity info and token status |
| `refresh` | Refresh sessions — interactive picker, by name, or multiple at once |
| `quick` | Quick switch between recently used profiles |
| `export` | Export credentials in multiple formats |
| `copy` | Fetch credentials and copy to system clipboard |
| `assume` | Assume an IAM role via STS on top of the current profile |
| `doctor` | Run a health check on config, tokens, and PATH |
| `prompt` | Output a profile badge for shell PS1/PROMPT integration |
| `init` | First-time setup wizard (URL → login → account/role → save) |
| `rename` | Rename a profile in `~/.aws/config` |
| `pin` | Pin a profile to the top of all lists |
| `unpin` | Remove a profile pin |
| `dashboard` | Interactive TUI session management dashboard |
| `shell` | Start an interactive session (also the default when no command is given) |
| `completion` | Generate shell tab-completion script |
| `daemon` | Run auto-refresh loop in the foreground |
| `service` | Install/uninstall auto-refresh as a background system service |

### Options

| Flag | Applies To | Description |
|------|-----------|-------------|
| `--profile <name>` | Most commands | AWS profile name (defaults to `$AWS_PROFILE` or `default`) |
| `--session <name>` | `login`, `switch`, `refresh` | Target a specific SSO session (multi-identity) |
| `--private` | `login`, `switch`, `refresh` | Open browser in incognito/InPrivate mode |
| `--format <fmt>` | `export`, `copy`, `assume` | `env`, `terraform`, `docker`, `json`, `yaml`, `kyaml`, `credential_process` |
| `--force` | `refresh` | Refresh even valid tokens (proactive refresh) |
| `--interval <min>` | `daemon`, `service` | Auto-refresh interval in minutes (default: `60`) |
| `--role <arn>` | `assume` | IAM Role ARN to assume |
| `--session-name <n>` | `assume` | Role session name (auto-derived if omitted) |

---

## Quick Start

### macOS / Linux

```bash
# 1. Login to your SSO profile
awssso login --profile my-profile

# 2. Check your identity
awssso whoami

# 3. Activate the profile in your shell
export AWS_PROFILE="my-profile"

# 4. Switch to a different account/role
awssso switch

# 5. Export credentials for Terraform
awssso export --profile prod --format terraform

# 6. Open AWS Console
awssso console --profile my-profile
```

### Windows (PowerShell)

```powershell
# 1. Login to your SSO profile
awssso login --profile my-profile

# 2. Check your identity
awssso whoami

# 3. Activate the profile in your shell
$env:AWS_PROFILE = "my-profile"

# 4. Switch to a different account/role
awssso switch

# 5. Export credentials for Terraform
awssso export --profile prod --format terraform

# 6. Open AWS Console
awssso console --profile my-profile
```

---

## Configuration

Profiles must be configured in `~/.aws/config`. Two formats are supported:

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

When creating profiles via `switch` or `login`, inline SSO is automatically converted to `sso-session` format.

### Using as `credential_process`

**macOS / Linux** — add to `~/.aws/config`:
```ini
[profile my-profile]
credential_process = "/usr/local/bin/awssso" credential --profile "my-profile"
```

**Windows** — add to `~/.aws/config`:
```ini
[profile my-profile]
credential_process = "C:\path\to\awssso.exe" credential --profile "my-profile"
```

Tokens are auto-refreshed transparently. No manual login needed after initial setup.

---

## Activating a Profile

After `awssso profiles` or `awssso switch`, activate the selected profile in your shell:

### macOS / Linux

```bash
# One-time in current session
export AWS_PROFILE="my-profile"

# Or source the generated helper script
source ~/.aws/activate.sh
```

### Windows — PowerShell

```powershell
# One-time in current session
$env:AWS_PROFILE = "my-profile"

# Or dot-source the generated helper script
. $HOME\.aws\activate.ps1
```

### Windows — Command Prompt

```cmd
set AWS_PROFILE=my-profile

rem Or run the generated helper script
%USERPROFILE%\.aws\activate.cmd
```

> **Note:** Setting `AWS_PROFILE` this way only affects the current terminal session. To persist it, add the export to `~/.zshrc` or `~/.bashrc` on macOS/Linux, or set it in System Properties → Environment Variables on Windows.

---

## Tab Completion

### macOS / Linux

**Zsh** (default shell on macOS):

```bash
# Auto-install (detects your shell automatically)
awssso completion --install

# Or install manually
mkdir -p ~/.zsh/completions
awssso completion --shell zsh > ~/.zsh/completions/_awssso

# Add to ~/.zshrc if not already present
echo 'fpath=(~/.zsh/completions $fpath)' >> ~/.zshrc
echo 'autoload -Uz compinit && compinit' >> ~/.zshrc

# Reload
exec zsh
```

**Bash**:

```bash
# Auto-install
awssso completion --install

# Or install manually
mkdir -p ~/.local/share/bash-completion/completions
awssso completion --shell bash > ~/.local/share/bash-completion/completions/awssso
```

**Fish**:

```bash
# Auto-install (no restart needed — fish loads completions automatically)
awssso completion --install

# Or install manually
mkdir -p ~/.config/fish/completions
awssso completion --shell fish > ~/.config/fish/completions/awssso.fish
```

### Windows (PowerShell)

```powershell
# Auto-install (detects PowerShell automatically)
awssso completion --install

# Or install manually
awssso completion --shell powershell > "$HOME\.aws\awssso_completion.ps1"
# Then add this line to your $PROFILE:
. "$HOME\.aws\awssso_completion.ps1"
```

**Git Bash on Windows** — use the bash script instead:
```bash
awssso completion --shell bash --install
```

### What completes

| Typed | Completes |
|-------|-----------|
| `awssso <TAB>` | All subcommands with descriptions |
| `awssso login --<TAB>` | `--profile`, `--session`, `--private` |
| `awssso login --profile <TAB>` | Profile names from `~/.aws/config` |
| `awssso login --session <TAB>` | Session names from `~/.aws/config` |
| `awssso export --format <TAB>` | `env`, `terraform`, `docker`, `json`, `yaml`, `credential_process` |
| `awssso refresh --<TAB>` | `--profile`, `--session`, `--private`, `--force` |

---

## Credential Export

### macOS / Linux

```bash
# Shell environment variables (Bash/Zsh)
awssso export --profile my-profile --format env

# Terraform variables
awssso export --profile my-profile --format terraform

# Docker env file
awssso export --profile my-profile --format docker

# Raw JSON
awssso export --profile my-profile --format json

# YAML
awssso export --profile my-profile --format yaml

# credential_process config line
awssso export --profile my-profile --format credential_process
```

### Windows (PowerShell)

```powershell
# Shell environment variables — outputs $env: syntax automatically on Windows
awssso export --profile my-profile --format env

# Terraform variables
awssso export --profile my-profile --format terraform

# Docker env file — uses backtick ` line continuation on Windows
awssso export --profile my-profile --format docker

# Raw JSON
awssso export --profile my-profile --format json
```

---

## Multi-Identity / Multi-Session Support

For teams where multiple identities share one SSO start URL.

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

### macOS / Linux

```bash
# Login to each identity in a private window
awssso login --session team-alpha --private
awssso login --session team-beta --private

# List sessions and their token status
awssso sessions

# Refresh — interactive picker (type numbers to choose)
awssso refresh

# Refresh specific sessions by name
awssso refresh team-alpha team-beta

# Refresh a single session
awssso refresh --session team-beta --force --private
```

### Windows (PowerShell)

```powershell
# Login to each identity in an InPrivate window
awssso login --session team-alpha --private
awssso login --session team-beta --private

# List sessions and their token status
awssso sessions

# Refresh — interactive picker
awssso refresh

# Refresh specific sessions by name
awssso refresh team-alpha team-beta

# Refresh a single session
awssso refresh --session team-beta --force --private
```

### Interactive refresh picker

Running `awssso refresh` with no arguments shows a numbered session list. Type any combination of:

| Input | Meaning |
|-------|---------|
| `2` | Session 2 only |
| `1 2 3` | Sessions 1, 2, and 3 |
| `1,3,5` | Sessions 1, 3, and 5 |
| `1-4` | Sessions 1 through 4 |
| `1-3 5` | Sessions 1, 2, 3, and 5 |
| `all` | Every session |

Selected sessions are refreshed in parallel.

### Why `--private` Matters

Without `--private`, your browser opens with existing cookies and may auto-authenticate as the wrong identity. With `--private`, a fresh incognito/InPrivate window opens with no cached session.

### Browser Detection

| Platform | Search Order |
|----------|-------------|
| **macOS** | Chrome → Firefox → Brave (checks `/Applications/` and `~/Applications/`) → `open -a` fallback → default |
| **Windows** | Edge (InPrivate) → Chrome (Incognito) → Firefox (Private) → default |
| **Linux** | Chrome / Chromium → Firefox → `xdg-open` fallback |

---

## Environment Colors

Profiles are automatically color-coded based on keywords in the profile or role name:

| Color | Environment | Detected Keywords |
|-------|-------------|-------------------|
| 🔴 Red | Production | `prod`, `prd`, `pro`, `live`, `master` |
| 🟡 Yellow | Staging | `staging`, `stage`, `stg`, `uat`, `preprod` |
| 🟡 Yellow | OAT | `oat`, `e2e`, `qa` |
| 🟣 Magenta | Integration | `int`, `integration` |
| ⚪ White | Sandbox | `sandbox`, `sbx` |
| 🟢 Green | Development | `dev`, `development`, `test` |

Production profiles show a confirmation prompt before any action.

---

## Interactive Shell

Run `awssso` with no arguments (or `awssso shell`) to enter an interactive session where you type commands without the `awssso` prefix each time.

### macOS / Linux

```bash
awssso
```

```
awssso › whoami
awssso › profiles
awssso › login --profile prod --private
awssso › refresh
awssso › exit
```

### Windows (PowerShell)

```powershell
awssso
```

```
awssso › whoami
awssso › profiles
awssso › exit
```

All commands work exactly as normal, including interactive ones like `switch`, `profiles`, and `delete`. Type `exit`, `quit`, or press `Ctrl+D` to leave.

> **Keyboard shortcuts:** `↑` / `↓` navigate history · `Tab` completes commands, flags, and profile/session names · `Ctrl+R` searches history · `Ctrl+C` clears the current line · `Ctrl+D` exits.

---

## Auto-Refresh

Keep sessions alive automatically without manual intervention. Two modes are available depending on whether you want a foreground process or a persistent background service.

> **Note:** Only sessions with a cached OIDC refresh token are refreshed silently. Sessions that have fully expired and require browser login will be reported in the output but skipped — run `awssso login --session <name>` to restore them.

### Daemon (foreground)

Runs in your terminal and refreshes all sessions on a timer. Stop it with `Ctrl+C`.

**macOS / Linux**
```bash
# Refresh every 60 minutes (default)
awssso daemon

# Custom interval
awssso daemon --interval 30
```

**Windows (PowerShell)**
```powershell
awssso daemon
awssso daemon --interval 30
```

### Service (background — survives reboots)

Installs a persistent background service that runs automatically, even after a restart.

| Platform | Backend |
|----------|---------|
| macOS | launchd (`~/Library/LaunchAgents/`) |
| Windows | Task Scheduler (`schtasks`) |
| Linux | cron |

**macOS / Linux**
```bash
# Install and start (60 min default)
awssso service --install
awssso service --install --interval 30

# Pause without losing config
awssso service --off

# Resume
awssso service --on

# Check status
awssso service --status

# Remove completely
awssso service --uninstall
```

**Windows (PowerShell)**
```powershell
awssso service --install
awssso service --install --interval 30
awssso service --off
awssso service --on
awssso service --status
awssso service --uninstall
```

Logs are written to:
- **macOS:** `~/Library/Logs/awssso-refresh.log`
- **Linux:** `~/.local/share/awssso/refresh.log`
- **Windows:** viewable in Task Scheduler history

---

## Interactive Dashboard

### macOS / Linux

```bash
awssso dashboard
```

### Windows (PowerShell)

```powershell
awssso dashboard
```

A full-screen TUI showing all SSO sessions with real-time status.

| Key | Action |
|-----|--------|
| `↑` / `↓` | Navigate sessions |
| `r` | Refresh selected session |
| `l` | Login to selected session |
| `q` / `Esc` | Quit |

---

## New Commands (v2.0.0)

### `awssso init` — First-time setup

Guided wizard for new users. Prompts for SSO URL and region, opens a browser to authenticate, then walks through account and role selection.

```bash
awssso init
```

### `awssso copy` — Credentials to clipboard

Fetches credentials and copies them directly to the system clipboard — no manual piping needed.

**macOS / Linux**
```bash
awssso copy --profile my-profile
awssso copy --profile my-profile --format terraform
```

**Windows (PowerShell)**
```powershell
awssso copy --profile my-profile
```

### `awssso assume` — Role chaining

Assumes an IAM role via STS on top of the credentials from any existing profile.

```bash
awssso assume --role arn:aws:iam::123456789012:role/MyRole
awssso assume --role arn:aws:iam::123456789012:role/MyRole --profile base-profile --format env
```

### `awssso doctor` — Health check

Validates config, checks token status per session, detects orphaned profiles, and verifies the binary is on PATH.

```bash
awssso doctor
```

### `awssso prompt` — Shell PS1 integration

Outputs a compact profile badge for embedding in your shell prompt.

**macOS / Linux** — add to `~/.zshrc` or `~/.bashrc`:
```bash
PROMPT='$(awssso prompt) '$PROMPT     # zsh
PS1='$(awssso prompt) '$PS1           # bash
```

**Windows** — add to `$PROFILE`:
```powershell
function prompt { "$(awssso prompt) PS $($executionContext.SessionState.Path.CurrentLocation)> " }
```

Output: `[🔴 prod]`, `[🟢 dev]`, etc. — nothing if `$AWS_PROFILE` is not set.

### `awssso rename` — Rename a profile

Renames a profile in `~/.aws/config`, updating all internal references.

```bash
awssso rename old-profile-name new-profile-name
```

### `awssso pin` / `awssso unpin` — Favourites

Pins profiles to the top of every list and picker.

```bash
awssso pin my-prod-profile      # 📌 appears at top of all lists
awssso unpin my-prod-profile
```

### `awssso export --format kyaml` — Kubernetes Secret

Exports credentials as a Kubernetes Secret YAML, ready to pipe directly to `kubectl`.

```bash
awssso export --profile my-profile --format kyaml | kubectl apply -f -
```

Output:
```yaml
apiVersion: v1
kind: Secret
metadata:
  name: aws-credentials
type: Opaque
stringData:
  AWS_ACCESS_KEY_ID: "ASIA..."
  AWS_SECRET_ACCESS_KEY: "..."
  AWS_SESSION_TOKEN: "..."
```

---

## Project Structure

```
├── main.go              # CLI entry point: flag parsing, command dispatch, usage
├── commands.go          # Command handlers + shared helpers (resolveValidToken, etc.)
├── awsconfig.go         # ~/.aws/config parsing, token cache, profile/session CRUD
├── sso.go               # AWS SSO OIDC login, token refresh, role credentials, console
├── cloudeng.go          # Environment detection, recent profiles, export formats
├── completion.go        # Tab completion scripts and install logic (zsh, bash, fish, PowerShell)
├── autorefresh.go       # Daemon loop and service install/uninstall (launchd, Task Scheduler, cron)
├── repl.go              # Interactive shell mode (no-arg entry point and `shell` command)
├── clipboard.go         # awssso copy — credential clipboard support (pbcopy/clip/xclip)
├── assume.go            # awssso assume — STS AssumeRole / role chaining
├── doctor.go            # awssso doctor — config and token health check
├── prompt.go            # awssso prompt — shell PS1 badge output
├── init.go              # awssso init — first-time setup wizard
├── rename.go            # awssso rename — profile rename
├── pins.go              # awssso pin/unpin — profile favourites
├── notify.go            # Desktop notifications for expired sessions
├── dashboard.go         # Interactive TUI dashboard (bubbletea)
├── util.go              # Utility functions (formatting, Levenshtein, filtering)
├── ui.go                # Terminal UI helpers       [Windows build tag]
├── ui_other.go          # Terminal UI helpers       [macOS / Linux build tag]
├── browser_windows.go   # Browser open + InPrivate  [Windows build tag]
├── browser_other.go     # Browser open + incognito  [macOS / Linux build tag]
├── *_test.go            # Tests
├── CHANGELOG.md         # Version history (Fibonacci versioning)
└── CLAUDE.md            # Mandatory rules for AI agents working in this repo
```

---

## Development

### macOS / Linux

```bash
# Build
go build -o awssso .

# Run tests
go test ./...

# Static analysis
go vet ./...
```

### Windows

```powershell
# Build
go build -o awssso.exe .

# Run tests
go test ./...

# Static analysis
go vet ./...
```

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2` | AWS SDK (SSO, SSOOIDC, STS, Signin) |
| `github.com/charmbracelet/bubbletea` | Interactive TUI framework |
| `github.com/charmbracelet/lipgloss` | Terminal styling |
| `github.com/chzyer/readline` | Line editing, history, and tab completion for the interactive shell |

---

## License

Internal tool — not publicly licensed.

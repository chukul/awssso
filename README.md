# awssso — AWS SSO Credential Process Helper

A fast, single-binary CLI tool for AWS SSO authentication and credential management. Built for cloud engineers who manage multiple AWS accounts and identities via SSO.

---

## Features

- **Multi-identity SSO** — multiple sessions sharing one start URL, each with a different email identity
- **Private browser mode** (`--private`) — opens incognito/InPrivate window for clean auth flows
- **Interactive account/role switching** — auto-matches account by name; auto-configures missing profiles without leaving the command
- **`credential_process` compatible** — use as a credential helper in `~/.aws/config`
- **AWS Console federation** — open the Management Console pre-authenticated
- **Credential export** — Terraform, Docker, JSON, YAML, shell env vars, Kubernetes Secret (`kyaml`); format auto-detected from working directory
- **Copy to clipboard** — `awssso export --clipboard` copies credentials; auto-detects Terraform/Docker context
- **Smart token refresh** — OIDC `refresh_token` when available, auto-re-login fallback; `whoami` refreshes silently
- **Profile groups** — tag profiles (`awssso group`), filter lists by tag (`profiles --group eks`), create and delete groups
- **Cross-platform** — all path handling uses `filepath.Join`; Windows, macOS, and Linux all tested; arrow keys work in the interactive shell on all platforms
- **Interactive shell** — `awssso` drops into a REPL with full line editing: Tab shows profile names (not full commands), auto-completes the common prefix, and opens an interactive selectable popup (↑↓ / Tab / Enter) when several matches exist; ↑↓ history, ←→ cursor, Home/End, Delete, copy/paste — no setup required
- **Tab completion** — zsh, bash, fish, PowerShell; all commands, flags, profile names, group tags complete dynamically
- **Shell prompt badge** — `awssso completion --prompt --install` patches your shell config; shows `[🔴 prod ~9m]` with expiry warning
- **Environment detection** — colour-coded profiles (🔴 prod, 🟡 staging/oat, 🟣 int, ⚪ sandbox, 🟢 dev)
- **Profile management** — rename, health check (`doctor`), first-time wizard (`init`), `[No SSO]` profiles auto-configure on selection
- **Product ownership** — `PRODUCT.md` defines the roadmap, acceptance criteria, and Definition of Done

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

**Core** — what you use every day

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO (`--group`, `--session`, `--private`) |
| `create` | Pick an account/role and create a profile |
| `profiles` | List and activate profiles (`--group <tag>` to filter) |
| `export` | Get credentials (`--format`, `--clipboard`) |
| `refresh` | Refresh sessions — interactive picker or `--session` |
| `whoami` | Current profile, account, role, and token status |
| `console` | Open AWS Management Console in browser |

**Management**

| Command | Description |
|---------|-------------|
| `group` | Tag profiles into groups; `profiles --group <tag>` filters |
| `rename` | Rename a profile in `~/.aws/config` |
| `delete` | Delete profiles (`delete my-profile` or `--profile my-profile`) |
| `doctor` | Config and token health check |
| `init` | First-time setup wizard |

**Shell integration**

| Command | Description |
|---------|-------------|
| `shell` | Spawn a system shell (bash/zsh/PowerShell) with the active profile's AWS credentials |
| `completion --install` | Install tab completion (zsh, bash, fish, PowerShell) |
| `completion --prompt --install` | Install shell prompt badge (`[🔴 prod ~9m]`) |
| `completion --prompt` | Print badge for PS1 embedding |

> `credential` is intentionally unlisted — it is only needed for `credential_process = awssso credential --profile ...` entries in `~/.aws/config`.

### Options

| Flag | Applies To | Description |
|------|-----------|-------------|
| `--profile <name>` | Most commands | AWS profile name (defaults to `$AWS_PROFILE` or `default`) |
| `--group <tag>` | `login`, `profiles` | Target a profile group |
| `--session <name>` | `login`, `create`, `refresh` | Target a specific SSO session |
| `--private` | `login`, `create`, `refresh` | Open browser in incognito/InPrivate mode |
| `--format <fmt>` | `export` | `env`, `terraform`, `docker`, `json`, `yaml`, `kyaml`, `credential_process` |
| `--clipboard` | `export` | Copy credentials to clipboard instead of printing |
| `--force` | `refresh` | Refresh even valid tokens |

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
awssso create

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
awssso create

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

When creating profiles via `create` or `login`, inline SSO is automatically converted to `sso-session` format.

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

After `awssso profiles` or `awssso create`, activate the selected profile in your shell:

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

## Spawning a System Shell

Once you've activated a profile, use the `shell` command to spawn a system shell (bash/zsh/PowerShell) with that profile's AWS credentials in the environment. This lets you run `terraform`, `aws-cli`, and other tools directly without manually exporting credentials.

### macOS / Linux

```bash
awssso › profiles               # Activate a profile
awssso › shell                  # Spawn bash/zsh with AWS credentials
$ terraform plan               # Now terraform sees AWS_PROFILE, AWS_ACCESS_KEY_ID, etc.
$ aws s3 ls                    # And aws-cli works too
$ exit                         # Return to awssso REPL
awssso › exit
```

### Windows (PowerShell)

```powershell
awssso › profiles               # Activate a profile
awssso › shell                  # Spawn PowerShell with AWS credentials
PS> terraform plan             # terraform sees the env vars
PS> aws s3 ls                  # aws-cli works too
PS> exit                       # Return to awssso REPL
awssso › exit
```

The spawned shell inherits:

- `AWS_PROFILE` — the active profile name
- `AWS_ACCESS_KEY_ID` — temporary access key for this profile
- `AWS_SECRET_ACCESS_KEY` — temporary secret key
- `AWS_SESSION_TOKEN` — temporary session token (if using STS/assumed roles)

All credentials expire when the profile's SSO token expires; you'll need to run `refresh` in the REPL and spawn a new shell.

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
| `awssso export --format <TAB>` | `env`, `terraform`, `docker`, `json`, `yaml`, `kyaml`, `credential_process` |
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

All commands work exactly as normal, including interactive ones like `create`, `profiles`, and `delete`. Type `exit`, `quit`, or press `Ctrl+D` to leave.

> **Keyboard shortcuts:** `↑` / `↓` navigate history · `Tab` completes commands, flags, and profile/session names — when several matches exist it opens an interactive popup you navigate with `↑` / `↓` (or `Tab` / `Shift+Tab`) and select with `Enter` (`Esc` cancels) · `Ctrl+A` / `Ctrl+E` go to line start/end · `Ctrl+C` clears the current line · `Ctrl+D` exits.

### Spawning a System Shell from the REPL

Once you've activated a profile in the REPL, use the `shell` command to spawn a system shell (bash/zsh/PowerShell) with that profile's AWS credentials in the environment. This lets you run terraform, aws-cli, and other tools directly without manual credential export.

**macOS / Linux**

```bash
awssso › profiles               # Activate a profile
awssso › shell                  # Spawn bash/zsh with AWS credentials
$ terraform plan               # Now terraform sees AWS_PROFILE, AWS_ACCESS_KEY_ID, AWS_SECRET_ACCESS_KEY, etc.
$ aws s3 ls                    # And aws-cli works too
$ exit                         # Return to awssso REPL
awssso › exit
```

**Windows (PowerShell)**

```powershell
awssso › profiles               # Activate a profile
awssso › shell                  # Spawn PowerShell with AWS credentials
PS> terraform plan             # terraform sees the env vars
PS> aws s3 ls                  # aws-cli works too
PS> exit                       # Return to awssso REPL
awssso › exit
```

The spawned shell inherits:
- `AWS_PROFILE` — the active profile name
- `AWS_ACCESS_KEY_ID` — temporary access key for this profile
- `AWS_SECRET_ACCESS_KEY` — temporary secret key
- `AWS_SESSION_TOKEN` — temporary session token (if using STS/assumed roles)

All credentials expire when the profile's SSO token expires; you'll need to run `refresh` in the REPL and spawn a new shell.

---

## Command Reference (v4.0.0)

### `awssso init` — First-time setup

Guided wizard for new users. Prompts for SSO URL and region, opens a browser to authenticate, then walks through account and role selection.

```bash
awssso init
```

### `export --clipboard` — Credentials to clipboard

Fetches credentials and copies them directly to the system clipboard — no manual piping needed.

**macOS / Linux**
```bash
awssso export --profile my-profile --clipboard
awssso export --profile my-profile --format terraform --clipboard
```

**Windows (PowerShell)**
```powershell
awssso export --profile my-profile --clipboard
```

### `awssso doctor` — Health check

Validates config, checks token status per session, detects orphaned profiles, and verifies the binary is on PATH.

```bash
awssso doctor
```

### `completion --prompt` — Shell PS1 integration

Outputs a compact, coloured profile badge. Run `--install` once and it patches your shell config automatically — no manual editing needed.

**macOS / Linux / Windows**
```bash
awssso completion --prompt --install   # auto-detects shell and patches the config
```

Output: `[🔴 prod]`, `[🟡 oat]`, `[🟢 dev]`, etc. — nothing if `$AWS_PROFILE` is not set.

The badge updates live in the `awssso` interactive shell prompt as soon as you switch profiles. Multiple terminal windows each track their own active profile independently — switching in one window never affects another.

### `awssso rename` — Rename a profile

Renames a profile in `~/.aws/config`, updating all internal references.

```bash
awssso rename old-profile-name new-profile-name
```

### `awssso group` — Profile Groups

Tag profiles into named groups and filter any list by group.

```bash
# List
awssso group                                  # list all groups
awssso group eks                              # list profiles in group "eks"

# Create / delete
awssso group create eks                       # create an empty group
awssso group delete eks                       # remove entire group (asks confirmation)

# Add profiles (all forms are equivalent)
awssso group eks --add accor-acp-dev          # single profile
awssso group eks --add --profile accor-acp-dev
awssso group --add eks                        # uses active $AWS_PROFILE
awssso group eks --add prof1 prof2 prof3      # multiple profiles at once

# Remove profiles
awssso group eks --remove accor-acp-dev
awssso group eks --remove prof1 prof2 prof3

# Filter profiles list by group
awssso profiles --group eks

# Login to all sessions used by profiles in a group
awssso login --group eks
awssso login --group eks --private   # incognito window per session
```

### `group favourites` — Favourites

Use the `favourites` group tag to pin profiles to the top of your lists.

```bash
awssso group favourites --add my-profile     # add to favourites
awssso group favourites --remove my-profile  # remove from favourites
awssso profiles --group favourites           # list only favourites
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
├── repl.go              # Interactive shell mode (no-arg entry point and `shell` command)
├── clipboard.go         # export --clipboard support (pbcopy / PowerShell / xclip)
├── doctor.go            # awssso doctor — config and token health check
├── prompt.go            # completion --prompt — shell PS1 badge output and --install
├── groups.go            # awssso group — profile group management
├── init.go              # awssso init — first-time setup wizard
├── rename.go            # awssso rename — profile rename
├── dashboard.go         # SessionItem type + TUI internals (bubbletea; dashboard not exposed as a command)
├── util.go              # Utility functions (formatting, Levenshtein, filtering)
├── colors.go            # ANSI colour vars + NO_COLOR / non-TTY suppression (shared)
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

### Makefile (macOS / Linux)

```bash
make build        # compile only
make install      # compile + copy to /usr/local/bin/awssso
make test         # run all tests
make patch        # bump patch version, update CHANGELOG, build, install
make minor        # bump minor version
make major        # bump to next Fibonacci major version
```

### Manual (macOS / Linux)

```bash
go build -o awssso .
go test ./...
go vet ./...
```

### Windows

```powershell
go build -o awssso.exe .
go test ./...
go vet ./...
```

### Git Hooks (auto-installed)

| Hook | Trigger | Action |
|------|---------|--------|
| `post-commit` | After every commit | Builds and installs `/usr/local/bin/awssso` automatically |
| `pre-push` | Before every push | Blocks push if `README.md` or `CHANGELOG.md` were not updated |
| `pre-commit` | Before every commit | Blocks direct commits to `main` |

---

## Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/aws/aws-sdk-go-v2` | AWS SDK (SSO, SSOOIDC, STS, Signin) |
| `github.com/charmbracelet/bubbletea` | Interactive TUI framework |
| `github.com/charmbracelet/lipgloss` | Terminal styling |
| `github.com/chzyer/readline` | Line editing for interactive sub-prompts (role picker, account search, etc.) |
| `golang.org/x/term` | Raw terminal mode for the interactive shell (arrow keys, Tab, history) |

---

## License

Internal tool — not publicly licensed.



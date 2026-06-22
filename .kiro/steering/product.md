# Product Overview

AWS SSO Credential Process Helper (`awssso`) — a fast CLI tool for AWS SSO authentication and credential management, primarily targeting Windows but cross-platform compatible.

## Purpose

Provides interactive workflows for:
- Logging in via AWS SSO (device authorization + OIDC refresh)
- Switching accounts/roles and creating named profiles
- Outputting temporary credentials (usable as `credential_process` in `~/.aws/config`)
- Opening the AWS Management Console pre-authenticated
- Managing multiple SSO identities/sessions simultaneously
- Exporting credentials in multiple formats (env, terraform, docker, json, yaml)
- Interactive TUI dashboard for session management
- Listing all SSO sessions with identity and status info
- Private/incognito browser mode for clean multi-identity authentication

## Key Concepts

- Profiles map to AWS accounts + roles; stored in `~/.aws/config`
- SSO tokens are cached in `~/.aws/sso/cache/` (SHA1 hash of session name or start URL)
- The tool auto-migrates inline SSO config to `sso-session` format
- Multi-identity support: multiple `[sso-session]` blocks can share the same start URL but authenticate as different users (distinguished by `sso_account_email`)
- Private browser mode (`--private`): opens incognito/InPrivate window so each session gets a clean auth flow without interfering cookies
- Environment detection: profiles are color-coded by environment (prod/staging/dev/oat) based on name/account heuristics
- Production safety: commands that touch production profiles prompt for confirmation

## CLI Commands

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO (supports `--session`, `--private`) |
| `switch` | Interactively select account/role and create profile (supports `--session`, `--private`) |
| `credential` | Output temporary credentials as JSON (`credential_process` compatible) |
| `console` | Open AWS Management Console in browser |
| `whoami` | Show current profile, account, role, and token status |
| `profiles` | List profiles and set one as active |
| `delete` | Delete one or more profiles (interactive or by name) |
| `sessions` | List all SSO sessions with identity and status |
| `refresh` | Refresh expired tokens (supports `--session`, `--force`, `--private`) |
| `quick` | Quick switch between recently used profiles |
| `export` | Export credentials (env, terraform, docker, json, yaml) |
| `dashboard` | Interactive TUI session management |

## Multi-Identity Workflow

For teams sharing one SSO start URL but needing different email identities:

```ini
[sso-session team-alpha]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = alice@company.com

[sso-session team-beta]
sso_start_url = https://d-123456789.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = bob@company.com
```

```powershell
awssso login --session team-alpha --private   # Opens incognito, auth as alice
awssso login --session team-beta --private    # Opens incognito, auth as bob
```

## Target Users

Cloud engineers and DevOps practitioners who manage multiple AWS accounts via SSO and need fast credential rotation, profile switching, and export for tooling like Terraform and Docker.

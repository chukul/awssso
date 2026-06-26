# Product Overview

AWS SSO Credential Process Helper (`awssso`) — a fast, single-binary CLI tool for AWS SSO authentication and credential management. Cross-platform (Windows primary, macOS/Linux via build tags).

## Purpose

Provides interactive workflows for:
- Logging in via AWS SSO (device authorization + OIDC refresh)
- Switching accounts/roles and creating named profiles
- Outputting temporary credentials (usable as `credential_process` in `~/.aws/config`)
- Opening the AWS Management Console pre-authenticated (with interactive profile picker)
- Managing multiple SSO identities/sessions simultaneously
- Exporting credentials in multiple formats (env, terraform, docker, json, yaml)
- Interactive TUI dashboard for session management
- Listing all SSO sessions with identity and status info
- Smart private/incognito browser mode for multi-identity authentication

## Key Concepts

- Profiles map to AWS accounts + roles; stored in `~/.aws/config`
- SSO tokens are cached in `~/.aws/sso/cache/` (SHA1 hash of session name or start URL)
- The tool auto-migrates inline SSO config to `sso-session` format
- Multi-identity support: multiple `[sso-session]` blocks can share the same start URL but authenticate as different users (distinguished by `sso_account_email`)
- Smart private browser: auto-detects when private/incognito is needed based on whether the session's URL is shared with other sessions using different emails. First session alphabetically uses default browser; others get private.
- Explicit override: `sso_login_private = true/false` in `[sso-session]` config block
- Environment detection: profiles color-coded (🔴 prod, 🟡 staging, 🟣 oat, 🟢 dev) based on name/role heuristics
- Production safety: a single-line confirmation prompt (not alarming) before proceeding with production profiles

## CLI Commands

| Command | Description |
|---------|-------------|
| `login` | Authenticate via AWS SSO (supports `--session`, `--private`) |
| `switch` | Interactively select account/role and create profile (supports `--session`, `--private`) |
| `credential` | Output temporary credentials as JSON (`credential_process` compatible) |
| `console` | Interactive profile picker → auto-login if expired → open Management Console |
| `whoami` | Show current profile, account, role, and token status |
| `profiles` | List profiles grouped by session, with status, environment badges |
| `delete` | Delete one or more profiles (interactive or by name) |
| `sessions` | List all SSO sessions with identity and status |
| `refresh` | Refresh expired tokens (supports `--session`, `--force`, `--private`) |
| `quick` | Quick switch between recently used profiles |
| `export` | Export credentials (env, terraform, docker, json, yaml) |
| `dashboard` | Interactive TUI session management |

## Multi-Identity Workflow

```ini
[sso-session accor]
sso_start_url = https://d-936779ae2c.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = alice@company.com

[sso-session accor-CTA]
sso_start_url = https://d-936779ae2c.awsapps.com/start/
sso_region = eu-west-1
sso_account_email = CTA.bob@company.com
sso_login_private = true   # optional explicit override
```

- `accor` (first alphabetically) → default browser
- `accor-CTA` (different email, same URL) → private/incognito browser automatically
- Console command auto-logins expired sessions transparently

## Target Users

Cloud engineers and DevOps practitioners managing multiple AWS accounts via SSO.

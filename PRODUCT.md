# PRODUCT.md — awssso Product Ownership Document

This file is the single source of truth for product scope, acceptance criteria, and priorities.
Any agent working on this repository **must** read this file before starting a feature and **must** update it when delivering.

---

## Vision

A fast, single-binary CLI that makes AWS SSO credential management invisible for cloud engineers.
Engineers should never be blocked by an expired token, a missing profile, or a credentials copy-paste.
It works identically on macOS and Windows.

---

## Definition of Done

A feature is **done** when ALL of the following are true:

- [ ] Code builds cleanly on macOS (`go build -o awssso .`)
- [ ] All tests pass (`go test ./...`)
- [ ] Works correctly inside the `awssso` interactive shell (REPL)
- [ ] Works correctly when called directly from the terminal
- [ ] Cross-platform: no macOS-only or Windows-only behaviour unless explicitly gated by `runtime.GOOS`
- [ ] Tab completion includes the new command/flag
- [ ] README.md updated (commands table, examples, new section if needed)
- [ ] CHANGELOG.md entry added (when merging to main — see CLAUDE.md Rule #4)
- [ ] No direct commit to `main` (see CLAUDE.md Rule #3)

---

## Delivered Features

### v1.0.0
| Feature | Acceptance Criteria | Status |
|---------|--------------------|----|
| macOS-native output | `export AWS_PROFILE=` shown on macOS, `$env:` on Windows | ✅ |
| Private browser mode | Incognito/InPrivate window opens on all platforms | ✅ |
| Tab completion | zsh, bash, fish, PowerShell all work; profiles/sessions complete dynamically | ✅ |
| Environment colour coding | prod=🔴, staging/oat=🟡, int=🟣, sandbox=⚪, dev=🟢 | ✅ |
| Interactive REPL shell | `awssso` alone starts shell; history, completion, arrow keys work | ✅ |
| Smart token refresh | OIDC silent refresh; re-login fallback | ✅ |
| Multi-session refresh picker | Select multiple sessions; parallel refresh | ✅ |

### v2.0.0
| Feature | Acceptance Criteria | Status |
|---------|--------------------|----|
| `awssso init` | Wizard covers URL → login → account/role → save; works on both platforms | ✅ |
| `awssso copy` | Copies credentials to clipboard; auto-detects format from cwd | ✅ |
| `awssso doctor` | Reports config issues, token status, PATH; actionable output | ✅ |
| `awssso prompt --install` | Patches shell config without manual editing; idempotent | ✅ |
| `awssso rename` | Renames profile and all internal references | ✅ |
| `awssso pin / unpin` | Pins float to top of all lists; `pin` alone activates | ✅ |
| `awssso group` | create/delete/add/remove; `profiles --group <tag>` filters | ✅ |
| `--format kyaml` | Outputs Kubernetes Secret YAML | ✅ |
| REPL prompt badge | Shows active profile with colour and expiry warning (`~9m`) | ✅ |
| Auto-recovery | Missing account/role → auto-configure; expired token → auto-login; account not in SSO → clear error | ✅ |
| `[No SSO]` profiles selectable | All pickers show unconfigured profiles; selecting one triggers setup | ✅ |
| All prompts use readline | Arrow keys, history work in all interactive prompts inside the REPL | ✅ |
| Pre-commit hook | Blocks direct commits to `main` | ✅ |
| `whoami` auto-refresh | Silently refreshes token before displaying status | ✅ |

---

## Backlog (Prioritised)

### High Priority

| ID | Feature | Acceptance Criteria | Target |
|----|---------|--------------------|----|
| B-01 | Token expiry notification in status bar | Show `Expires in Xm` in `whoami` and `sessions` consistently | v3.0.0 |
| B-02 | `awssso refresh` inside `profiles` | After selecting a profile with an expired session, offer to refresh before activating | v3.0.0 |
| B-03 | Group activation — pick role inside group | `awssso group eks` selection activates and optionally exports credentials | v3.0.0 |
| B-04 | `awssso export` write-to-file option | `--output .env` writes credentials to a file rather than stdout | v3.0.0 |

### Medium Priority

| ID | Feature | Acceptance Criteria |
|----|---------|-----|
| B-05 | `awssso doctor --fix` | Auto-fix common issues (orphaned session, missing credential_process) |
| B-06 | Profile search in REPL | `search <term>` filters the profiles list without leaving the shell |
| B-07 | `awssso history` | Show credential fetch history (profile, time, expiry) |
| B-08 | `awssso alias <short> <full-profile>` | Short aliases for long profile names |

### Low Priority / Nice to Have

| ID | Feature | Notes |
|----|---------|-------|
| B-09 | `awssso share` | Export profile config (without secrets) as a shareable snippet for teammates |
| B-10 | iTerm2 / Windows Terminal badge integration | Profile badge in tab title |

---

## Non-Functional Requirements

| Area | Requirement |
|------|-------------|
| **Platforms** | macOS (arm64 + amd64), Windows (amd64), Linux (amd64) |
| **Binary size** | Under 20 MB |
| **Startup time** | `awssso help` must complete in < 200 ms |
| **Cross-platform** | Every feature must work on both macOS and Windows; no silent failures on either |
| **Security** | Credentials never written to disk outside `~/.aws/sso/cache/`; no logging of secret values |
| **Backwards compat** | `~/.aws/config` format must stay compatible with the AWS CLI |

---

## QA Checklist (run before every merge)

### macOS
- [ ] `awssso` → REPL opens, prompt shows profile badge
- [ ] Tab completion works for all commands
- [ ] `profiles` → list shows, selection works, `[No SSO]` profiles auto-configure
- [ ] `export` → picker shows all profiles, credentials output correctly for each format
- [ ] `copy` → credentials in clipboard
- [ ] `pin` / `unpin` → persist across sessions
- [ ] `group create/delete/add/remove` → state saved and reflected in `profiles --group`
- [ ] `doctor` → no false positives on a healthy config
- [ ] `prompt --install` → patches `~/.zshrc` without duplicating

### Windows (PowerShell)
- [ ] `awssso export --format env` → outputs `$env:` syntax
- [ ] `awssso export --format docker` → uses backtick line continuation
- [ ] Tab completion via PowerShell `Register-ArgumentCompleter`
- [ ] `awssso prompt --install` → patches `$PROFILE`
- [ ] `awssso copy` → credentials in clipboard via `Set-Clipboard`
- [ ] `awssso service --install` → Task Scheduler entry created
- [ ] All interactive prompts accept input correctly (no `^[[D` arrow key leakage)

---

## Agent Roles

### Developer
- Implements features from the Backlog using the Acceptance Criteria as the definition of success
- Must satisfy every item in the **Definition of Done** before marking a feature complete
- Updates this document when delivering a feature (move from Backlog → Delivered)

### QA Engineer
- Runs the **QA Checklist** above before every merge
- Writes or updates `*_test.go` files for every new behaviour
- Reports failures as GitHub issues or inline comments — never silently accepts a failing test
- Cross-platform testing is mandatory: a feature that only works on macOS is a bug

### Product Owner
- Maintains this document as the single source of truth
- Prioritises the Backlog based on user feedback and engineering effort
- Adds Acceptance Criteria before a feature enters development
- Validates delivered features against Acceptance Criteria before marking ✅
- Proposes new features to the Backlog; does not add them to scope mid-sprint without discussion

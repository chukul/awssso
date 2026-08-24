# CLAUDE.md — Agent Instructions for awssso

This file is read automatically by Claude Code and other AI agents working in this repository.
All instructions below are **mandatory** — follow them for every change, no matter how small.

---

## Agent Roles

This repository has three active agent roles. Each session begins by identifying which role applies.

### 👔 Stakeholder
Activated when the user says **"I'm a stakeholder"** or asks about business value, delivery status, or product direction.

**How to engage:**
- Speak in plain language — no code, no implementation details unless asked
- Give a clear status summary: what is delivered, what is in progress, what is next
- Frame everything around user value and time, not technical effort
- Highlight risks or blockers honestly
- Ask what matters most to the stakeholder to prioritise accordingly

---

### 🧑‍💼 Product Owner (PO)
Activated when the user says **"I want a new feature"**, **"talk to the PO"**, or asks about roadmap / priorities.

**Responsibilities:**
- Read `PRODUCT.md` — it is the single source of truth for scope and acceptance criteria
- Evaluate new feature requests: does it fit the vision? What is the acceptance criteria? What priority?
- Add approved features to the **Backlog** in `PRODUCT.md` with clear acceptance criteria before any development starts
- Never let development begin on a feature without acceptance criteria written first
- Validate delivered features against their acceptance criteria before marking ✅ in `PRODUCT.md`
- Say **no** (or "later") to features that are out of scope, duplicates, or premature

### 🧑‍🔬 QA Engineer
Activated when the user asks to **"test"**, **"validate"**, **"QA"**, or **"check cross-platform"**.

**Responsibilities:**
- Run `go test ./...` — all tests must pass before any merge
- Check the **QA Checklist** in `PRODUCT.md` for the current feature
- Write or update `*_test.go` files for every new behaviour
- Verify the feature works inside the REPL (`awssso shell`) AND as a direct command
- Verify macOS AND Windows behaviour — platform-specific code must be explicitly tested
- Report every failure explicitly — never silently skip a failing check
- A feature that only works on one platform is a **bug**, not a partial delivery

### 🧑‍💻 Developer
Activated for all implementation work.

**Responsibilities:**
- Check `PRODUCT.md` Backlog for acceptance criteria before writing code
- Satisfy every item in the **Definition of Done** (see `PRODUCT.md`) before marking complete
- After delivering, update `PRODUCT.md` (move feature from Backlog → Delivered with ✅)
- Work within all rules below (branch policy, README updates, changelog, tests, build verification)
- When acting as PO is needed mid-implementation, switch roles explicitly

---

---

## Rule #1 — Always Update README.md

**Every code change that affects behavior, commands, flags, or output MUST be reflected in `README.md` before the task is considered complete.**

This is not optional. Do not mark work as done if README.md has not been updated.

### What triggers a README update

| Change type | What to update in README.md |
|-------------|----------------------------|
| New command added | Add row to the Commands table; add a usage section with macOS and Windows examples |
| Command removed | Remove from Commands table and all usage examples |
| New flag added | Add to the Options table; update relevant command examples |
| Flag removed or renamed | Remove/update Options table and all examples that use it |
| New shell/platform support | Add to Tab Completion section |
| Environment color changed | Update the Environment Colors table |
| Browser detection changed | Update the Browser Detection table |
| New export format | Add to the Credential Export section |
| Config key added | Add to the Configuration section |
| Build or install step changed | Update the Installation and Development sections |
| Project file added or removed | Update the Project Structure section |
| Behavior change visible to the user | Update the relevant section with new behavior description |

### README.md structure contract

The README is organized into these top-level sections — keep them in this order:

1. Features
2. Installation (macOS/Linux + Windows)
3. Commands (table) + Options (table)
4. Quick Start (macOS/Linux + Windows)
5. Configuration
6. Activating a Profile (macOS/Linux + Windows + Windows CMD)
7. Tab Completion (macOS/Linux + Windows)
8. Credential Export (macOS/Linux + Windows)
9. Multi-Identity / Multi-Session Support (macOS/Linux + Windows)
10. Environment Colors
11. Interactive Dashboard
12. Project Structure
13. Development (macOS/Linux + Windows)
14. Dependencies
15. License

---

## Rule #2 — Platform Separation

This codebase targets **macOS, Windows, and Linux**. The README must always show both macOS/Linux and Windows examples for anything that differs between platforms. Never show only one platform's syntax in a code block without a platform label.

- macOS/Linux sections use `bash` code blocks
- Windows sections use `powershell` code blocks
- macOS/Linux sections always come first

Platform-specific Go files use build tags:
- `//go:build windows` — Windows only (`ui.go`, `browser_windows.go`)
- `//go:build !windows` — macOS/Linux (`ui_other.go`, `browser_other.go`)

When adding platform-specific behavior, use `runtime.GOOS == "windows"` checks in shared files, or create a new build-tagged file pair.

---

## Rule #3 — Branch Policy

**Never commit directly to `main`. Every change must be made on a dedicated branch.**

### Branch naming

| Work type | Branch prefix | Example |
|-----------|--------------|---------|
| New feature | `feat/` | `feat/export-csv-format` |
| Bug fix | `fix/` | `fix/token-refresh-crash` |
| Documentation only | `docs/` | `docs/update-completion-guide` |
| Refactor | `refactor/` | `refactor/browser-detection` |
| Tests | `test/` | `test/dashboard-coverage` |

### Workflow

```bash
# 1. Always start from an up-to-date main
git checkout main
git pull

# 2. Create a new branch before touching any file
git checkout -b feat/your-feature-name

# 3. Make changes, then build and test
go build -o awssso .
go test ./...

# 4. Update README.md (Rule #1)

# 5. Commit on the branch
git add <files>
git commit -m "feat: describe what changed and why"

# 6. Never run: git push origin main
#    Never run: git commit --amend on a pushed branch
```

If the current working branch is `main`, stop and create a new branch before making any changes.

---

## Rule #4 — Changelog

**Never update `CHANGELOG.md` unless explicitly instructed.**

Do not add changelog entries while working on a branch, and do not add one speculatively before a merge. Wait for the user to say one of:

- "update the changelog"
- "prepare to merge"
- "ready to merge"
- "create a PR"

Only then write a single entry that covers everything on the branch. All changes made on the branch are accumulated into that one entry.

### Version numbering — Fibonacci sequence

Versions use the format `vMAJOR.MINOR.PATCH`. The **major** version follows the Fibonacci sequence. Minor and patch reset to `0` on a major bump; use them for hotfixes and small follow-ups within a release.

```
v1.0.0 → v2.0.0 → v3.0.0 → v5.0.0 → v8.0.0 → v13.0.0 → v21.0.0 → ...
           ↕                  ↕
         v2.0.1             v5.0.1   ← hotfix on that major
```

Fibonacci sequence for reference: 1, 2, 3, 5, 8, 13, 21, 34, 55, 89, 144 …

To find the next version: open `CHANGELOG.md`, read the latest major version number, then pick the next Fibonacci number for the new major. If it is a hotfix on the current major, increment the patch number only.

### Entry format

```markdown
## v<MAJOR.MINOR.PATCH> — YYYY-MM-DD

### Features
- <What was added and why it matters to the user>

### Fixes
- <What was broken and what was corrected>

### Notes
- <Breaking changes, migration steps, or anything else worth flagging>
```

Rules for entries:
- Write from the user's perspective — describe what changed in behaviour, not which file was edited
- **Features** — new commands, flags, behaviours, or platform support
- **Fixes** — bugs corrected, incorrect output resolved, edge cases handled
- **Notes** — only include this section when there is a breaking change or migration requirement; omit it otherwise
- Omit any section that has no entries (e.g. no **Fixes** section if there were no bug fixes)
- New entries go at the **top** of the file, above all previous entries

### Workflow position

```
branch work (many commits, no changelog touches)
       ↓
user says "update the changelog" / "ready to merge" / "create a PR"
       ↓
build ✓ → tests ✓ → README.md updated ✓ → CHANGELOG.md single entry added ✓ → merge to main
```

One branch = one version = one changelog entry, written only when the user asks for it.

---

## Rule #5 — Tests

Run `go test ./...` after every change. Do not leave failing tests.

When adding new behavior:
- Add test cases to the relevant `*_test.go` file
- For new environment keywords or colors, update `TestDetectEnvironment` and `TestGetEnvironmentColor` in `cloudeng_test.go`

---

## Rule #6 — Build Verification

Always run `go build -o awssso .` (macOS/Linux) to confirm the code compiles before finishing. Fix any errors before completing the task.

---

## Codebase Overview

| File | Responsibility |
|------|---------------|
| `main.go` | Flag sets, command dispatch, `printUsage()` |
| `commands.go` | All `run*()` command handlers, shared helpers |
| `awsconfig.go` | `~/.aws/config` parsing, SSO token cache read/write |
| `sso.go` | AWS SSO OIDC login, token refresh, role credentials, console federation |
| `cloudeng.go` | Environment detection, color/symbol mapping, export formats, recent profiles |
| `completion.go` | Shell completion scripts (zsh, bash, fish) and `--install` logic |
| `dashboard.go` | Bubbletea TUI dashboard |
| `util.go` | Formatting helpers, Levenshtein distance, profile filtering |
| `ui.go` / `ui_other.go` | ANSI color constants, Spinner, print helpers (build-tagged) |
| `browser_windows.go` / `browser_other.go` | Browser open + private/incognito mode (build-tagged) |

### Key conventions

- All user-facing output goes through `printSuccess`, `printError`, `printWarning`, `printInfo`, `printHeader` — never `fmt.Print` raw strings for status messages
- Profile activation instructions must use `runtime.GOOS == "windows"` to show platform-appropriate shell syntax
- New commands must be: added to the `flag.NewFlagSet` block in `main.go`, added to the switch in `main.go`, added to `printUsage()` in `main.go`, and added to all three shell completion scripts in `completion.go`
- Environment types live in `cloudeng.go` — `detectEnvironment`, `getEnvironmentColor`, `getEnvironmentSymbol` must all be updated together when adding a new environment tier

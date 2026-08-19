package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ── Shell completion scripts ──────────────────────────────────────────────────

const zshCompletion = `#compdef awssso

_awssso_profiles() {
  local -a profiles
  profiles=(${(f)"$(awssso __list-profiles 2>/dev/null)"})
  _describe -t profiles 'AWS profile' profiles
}

_awssso_sessions() {
  local -a sessions
  sessions=(${(f)"$(awssso __list-sessions 2>/dev/null)"})
  _describe -t sessions 'SSO session' sessions
}

_awssso_commands() {
  local -a commands
  commands=(
    'login:Authenticate via AWS SSO'
    'credential:Output credentials (credential_process format)'
    'switch:Interactively select account/role'
    'console:Open AWS Console in browser'
    'dashboard:Interactive TUI dashboard'
    'whoami:Show current profile and token status'
    'quick:Quick switch between recent profiles'
    'profiles:List profiles and activate one'
    'delete:Delete one or more profiles'
    'sessions:List all SSO sessions'
    'refresh:Refresh expired SSO tokens'
    'export:Export credentials for DevOps tools'
    'shell:Start an interactive session'
    'daemon:Run auto-refresh loop in the foreground'
    'service:Install/uninstall auto-refresh as a background service'
    'help:Show help message'
  )
  _describe -t commands 'awssso command' commands
}

_awssso() {
  local curcontext="$curcontext" state line
  typeset -A opt_args

  _arguments -C \
    '1: :_awssso_commands' \
    '*:: :->subcommand'

  case $state in
    subcommand)
      curcontext="${curcontext%:*:*}:awssso-${line[1]}:"
      case $line[1] in
        login|switch)
          _arguments \
            '--profile[AWS profile name]:profile:_awssso_profiles' \
            '--session[SSO session name]:session:_awssso_sessions' \
            '--private[Open browser in incognito/InPrivate mode]'
          ;;
        refresh)
          _arguments \
            '--profile[AWS profile name]:profile:_awssso_profiles' \
            '--session[SSO session name]:session:_awssso_sessions' \
            '--private[Open browser in incognito/InPrivate mode]' \
            '--force[Refresh even valid tokens]'
          ;;
        credential|console|whoami|delete)
          _arguments \
            '--profile[AWS profile name]:profile:_awssso_profiles'
          ;;
        export)
          _arguments \
            '--profile[AWS profile name]:profile:_awssso_profiles' \
            '--format[Export format]:format:(env terraform docker json yaml credential_process)'
          ;;
        daemon)
          _arguments \
            '--interval[Refresh interval in minutes (default 60)]:minutes'
          ;;
        service)
          _arguments \
            '--install[Install as a background service]' \
            '--uninstall[Remove the background service]' \
            '--status[Check whether the service is running]' \
            '--interval[Refresh interval in minutes (default 60)]:minutes'
          ;;
      esac
      ;;
  esac
}

_awssso
`

const bashCompletion = `_awssso() {
  local cur prev words cword
  _init_completion || return

  local commands="login credential switch console dashboard whoami quick profiles delete sessions refresh export shell daemon service help"

  if [[ $cword -eq 1 ]]; then
    COMPREPLY=($(compgen -W "$commands" -- "$cur"))
    return 0
  fi

  local cmd="${words[1]}"

  case "$prev" in
    --profile)
      local profiles
      profiles=$(awssso __list-profiles 2>/dev/null)
      COMPREPLY=($(compgen -W "$profiles" -- "$cur"))
      return 0
      ;;
    --session)
      local sessions
      sessions=$(awssso __list-sessions 2>/dev/null)
      COMPREPLY=($(compgen -W "$sessions" -- "$cur"))
      return 0
      ;;
    --format)
      COMPREPLY=($(compgen -W "env terraform docker json yaml credential_process" -- "$cur"))
      return 0
      ;;
  esac

  case "$cmd" in
    login|switch)
      COMPREPLY=($(compgen -W "--profile --session --private" -- "$cur")) ;;
    refresh)
      COMPREPLY=($(compgen -W "--profile --session --private --force" -- "$cur")) ;;
    credential|console|whoami|delete)
      COMPREPLY=($(compgen -W "--profile" -- "$cur")) ;;
    export)
      COMPREPLY=($(compgen -W "--profile --format" -- "$cur")) ;;
  esac
}

complete -F _awssso awssso
`

const powershellCompletion = `# awssso PowerShell tab completion
# Source this file from your $PROFILE, or run: awssso completion --shell powershell --install
Register-ArgumentCompleter -Native -CommandName @('awssso', 'awssso.exe') -ScriptBlock {
    param($wordToComplete, $commandAst, $cursorPosition)

    $commands = @('login','credential','switch','console','dashboard','whoami','quick',
                  'profiles','delete','sessions','refresh','export','shell','daemon',
                  'service','completion','help')

    $flagMap = @{
        'login'      = @('--profile','--session','--private')
        'switch'     = @('--profile','--session','--private')
        'refresh'    = @('--profile','--session','--private','--force')
        'credential' = @('--profile')
        'console'    = @('--profile')
        'whoami'     = @('--profile')
        'delete'     = @('--profile')
        'export'     = @('--profile','--format')
        'daemon'     = @('--interval')
        'service'    = @('--install','--uninstall','--on','--off','--status','--interval')
        'completion' = @('--shell','--install')
    }

    $elements = $commandAst.CommandElements
    $nElements = $elements.Count
    $subCmd = if ($nElements -gt 1) { $elements[1].ToString() } else { '' }
    $prevWord = if ($wordToComplete -eq '' -and $nElements -gt 1) {
        $elements[$nElements - 1].ToString()
    } elseif ($nElements -gt 2) {
        $elements[$nElements - 2].ToString()
    } else { '' }

    # Complete subcommand name
    if ($nElements -le 1 -or ($nElements -eq 2 -and $wordToComplete -ne '')) {
        return $commands | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    }

    # Complete flag values
    switch ($prevWord) {
        '--profile' {
            return (awssso __list-profiles 2>$null) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        '--session' {
            return (awssso __list-sessions 2>$null) | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        '--format' {
            return @('env','terraform','docker','json','yaml','credential_process') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
        '--shell' {
            return @('zsh','bash','fish','powershell') | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
                [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
            }
        }
    }

    # Complete flags for the subcommand
    if ($flagMap.ContainsKey($subCmd)) {
        return $flagMap[$subCmd] | Where-Object { $_ -like "$wordToComplete*" } | ForEach-Object {
            [System.Management.Automation.CompletionResult]::new($_, $_, 'ParameterValue', $_)
        }
    }
}
`

const fishCompletion = `# awssso fish shell completions
set -l commands login credential switch console dashboard whoami quick profiles delete sessions refresh export shell daemon service help

# Subcommands (only shown before a subcommand is typed)
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a login      -d "Authenticate via AWS SSO"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a credential -d "Output credentials in JSON format"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a switch     -d "Interactively select account/role"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a console    -d "Open AWS Console in browser"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a dashboard  -d "Interactive TUI dashboard"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a whoami     -d "Show current profile and token status"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a quick      -d "Quick switch between recent profiles"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a profiles   -d "List profiles and activate one"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a delete     -d "Delete one or more profiles"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a sessions   -d "List all SSO sessions"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a refresh    -d "Refresh expired SSO tokens"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a export     -d "Export credentials for DevOps tools"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a shell      -d "Start an interactive session"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a daemon     -d "Run auto-refresh loop in the foreground"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a service    -d "Install/uninstall auto-refresh as a background service"
complete -c awssso -f -n "not __fish_seen_subcommand_from $commands" -a help       -d "Show help message"

# daemon flags
complete -c awssso -l interval -r -d "Refresh interval in minutes (default 60)" \
  -n "__fish_seen_subcommand_from daemon service"

# service flags
complete -c awssso -l install   -d "Install as a background service"  -n "__fish_seen_subcommand_from service"
complete -c awssso -l uninstall -d "Remove the background service"    -n "__fish_seen_subcommand_from service"
complete -c awssso -l status    -d "Check whether the service is running" -n "__fish_seen_subcommand_from service"

# --profile (dynamic, reads ~/.aws/config)
complete -c awssso -l profile -r -d "AWS profile name" \
  -n "__fish_seen_subcommand_from login switch credential console whoami delete refresh export" \
  -a "(awssso __list-profiles 2>/dev/null)"

# --session (dynamic, reads ~/.aws/config)
complete -c awssso -l session -r -d "SSO session name" \
  -n "__fish_seen_subcommand_from login switch refresh" \
  -a "(awssso __list-sessions 2>/dev/null)"

# --private
complete -c awssso -l private -d "Open browser in incognito/InPrivate mode" \
  -n "__fish_seen_subcommand_from login switch refresh"

# --force
complete -c awssso -l force -d "Refresh even valid tokens" \
  -n "__fish_seen_subcommand_from refresh"

# --format
complete -c awssso -l format -r -d "Export format" \
  -n "__fish_seen_subcommand_from export" \
  -a "env\t'Shell env vars' terraform\t'Terraform vars' docker\t'Docker env file' json\t'Raw JSON' yaml\t'YAML' credential_process\t'credential_process line'"
`

// ── Command handlers ──────────────────────────────────────────────────────────

func runCompletion(shell string, install bool) {
	if shell == "" {
		shell = detectShell()
		if shell == "" {
			printError("Could not detect shell. Specify with --shell zsh|bash|fish|powershell")
			os.Exit(1)
		}
		printInfo(fmt.Sprintf("Detected shell: %s", shell))
	}

	script := completionScript(shell)
	if script == "" {
		printError(fmt.Sprintf("Unknown shell %q. Supported: zsh, bash, fish, powershell", shell))
		os.Exit(1)
	}

	if install {
		installCompletion(shell, script)
		return
	}

	fmt.Print(script)
}

func completionScript(shell string) string {
	switch shell {
	case "zsh":
		return zshCompletion
	case "bash":
		return bashCompletion
	case "fish":
		return fishCompletion
	case "powershell":
		return powershellCompletion
	default:
		return ""
	}
}

func detectShell() string {
	// Windows: $SHELL is never set; detect PowerShell via its environment markers
	if os.Getenv("PSModulePath") != "" || os.Getenv("COMSPEC") != "" {
		if os.Getenv("SHELL") == "" { // only if not inside WSL/Git Bash on Windows
			return "powershell"
		}
	}
	shellPath := os.Getenv("SHELL")
	switch {
	case strings.HasSuffix(shellPath, "zsh"):
		return "zsh"
	case strings.HasSuffix(shellPath, "bash"):
		return "bash"
	case strings.HasSuffix(shellPath, "fish"):
		return "fish"
	default:
		return ""
	}
}

func installCompletion(shell, script string) {
	home, err := homeDir()
	if err != nil {
		printError(fmt.Sprintf("Could not determine home directory: %v", err))
		os.Exit(1)
	}

	switch shell {
	case "zsh":
		dir := filepath.Join(home, ".zsh", "completions")
		dest := filepath.Join(dir, "_awssso")
		if err := os.MkdirAll(dir, 0755); err != nil {
			printError(fmt.Sprintf("Failed to create completions directory: %v", err))
			os.Exit(1)
		}
		if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
			printError(fmt.Sprintf("Failed to write completion file: %v", err))
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Completion script written to: %s", dest))
		ensureZshrcFpath(home, dir)
		fmt.Println()
		printInfo("Restart your terminal, or run:")
		fmt.Printf("  %sexec zsh%s\n", Dim, Reset)

	case "bash":
		dir := filepath.Join(home, ".local", "share", "bash-completion", "completions")
		dest := filepath.Join(dir, "awssso")
		if err := os.MkdirAll(dir, 0755); err != nil {
			printError(fmt.Sprintf("Failed to create completions directory: %v", err))
			os.Exit(1)
		}
		if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
			printError(fmt.Sprintf("Failed to write completion file: %v", err))
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Completion script written to: %s", dest))
		fmt.Println()
		printInfo("Restart your terminal, or run:")
		fmt.Printf("  %ssource %s%s\n", Dim, dest, Reset)

	case "fish":
		dir := filepath.Join(home, ".config", "fish", "completions")
		dest := filepath.Join(dir, "awssso.fish")
		if err := os.MkdirAll(dir, 0755); err != nil {
			printError(fmt.Sprintf("Failed to create completions directory: %v", err))
			os.Exit(1)
		}
		if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
			printError(fmt.Sprintf("Failed to write completion file: %v", err))
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Completion script written to: %s", dest))
		printInfo("Fish loads completions automatically — no further setup needed.")

	case "powershell":
		// Write the script next to other AWS config files
		scriptDir := filepath.Join(home, ".aws")
		dest := filepath.Join(scriptDir, "awssso_completion.ps1")
		if err := os.MkdirAll(scriptDir, 0755); err != nil {
			printError(fmt.Sprintf("Failed to create directory: %v", err))
			os.Exit(1)
		}
		if err := os.WriteFile(dest, []byte(script), 0644); err != nil {
			printError(fmt.Sprintf("Failed to write completion file: %v", err))
			os.Exit(1)
		}
		printSuccess(fmt.Sprintf("Completion script written to: %s", dest))
		ensurePowerShellProfile(dest)
	}
}

// ensurePowerShellProfile adds a dot-source line to the PowerShell profile
// so the completion script is loaded on every new session.
func ensurePowerShellProfile(scriptPath string) {
	// Ask PowerShell for the actual profile path (handles PS 5 vs PS 7 differences)
	out, err := exec.Command("powershell", "-NoProfile", "-Command", "$PROFILE").Output()
	if err != nil {
		printWarning("Could not detect PowerShell profile path.")
		printInfo("Add this line to your PowerShell $PROFILE manually:")
		fmt.Printf("  %s. \"%s\"%s\n", Dim, scriptPath, Reset)
		return
	}
	profilePath := strings.TrimSpace(string(out))

	data, _ := os.ReadFile(profilePath)
	if strings.Contains(string(data), scriptPath) {
		return // already sourced
	}

	if err := os.MkdirAll(filepath.Dir(profilePath), 0755); err != nil {
		printWarning(fmt.Sprintf("Could not create profile directory: %v", err))
		return
	}
	f, err := os.OpenFile(profilePath, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		printWarning(fmt.Sprintf("Could not update PowerShell profile: %v", err))
		printInfo("Add this line to your $PROFILE manually:")
		fmt.Printf("  %s. \"%s\"%s\n", Dim, scriptPath, Reset)
		return
	}
	defer f.Close()
	fmt.Fprintf(f, "\n# awssso tab completion\n. \"%s\"\n", scriptPath)
	printSuccess(fmt.Sprintf("PowerShell profile updated: %s", profilePath))
	fmt.Println()
	printInfo("Restart PowerShell, or run:")
	fmt.Printf("  %s. \"%s\"%s\n", Dim, scriptPath, Reset)
}

// ensureZshrcFpath checks ~/.zshrc for the fpath and compinit lines and appends
// them if missing.
func ensureZshrcFpath(home, completionsDir string) {
	zshrc := filepath.Join(home, ".zshrc")
	data, _ := os.ReadFile(zshrc)
	content := string(data)

	fpathLine := fmt.Sprintf("fpath=(%s $fpath)", completionsDir)
	compLine := "autoload -Uz compinit && compinit"

	needsFpath := !strings.Contains(content, completionsDir)
	needsCompinit := !strings.Contains(content, "compinit")

	if !needsFpath && !needsCompinit {
		return
	}

	f, err := os.OpenFile(zshrc, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0644)
	if err != nil {
		printWarning(fmt.Sprintf("Could not update ~/.zshrc: %v", err))
		printInfo("Add these lines to ~/.zshrc manually:")
		if needsFpath {
			fmt.Printf("  %s%s%s\n", Dim, fpathLine, Reset)
		}
		if needsCompinit {
			fmt.Printf("  %s%s%s\n", Dim, compLine, Reset)
		}
		return
	}
	defer f.Close()

	var toAdd []string
	if needsFpath {
		toAdd = append(toAdd, fpathLine)
	}
	if needsCompinit {
		toAdd = append(toAdd, compLine)
	}
	fmt.Fprintf(f, "\n# awssso tab completion\n%s\n", strings.Join(toAdd, "\n"))
	printSuccess(fmt.Sprintf("Updated ~/.zshrc with fpath and compinit"))
}

// runListProfiles prints all profile names, one per line. Used by completion scripts.
func runListProfiles() {
	config, err := loadAWSConfig()
	if err != nil {
		os.Exit(1)
	}
	for name := range config.Profiles {
		fmt.Println(name)
	}
}

// runListSessions prints all SSO session names, one per line. Used by completion scripts.
func runListSessions() {
	config, err := loadAWSConfig()
	if err != nil {
		os.Exit(1)
	}
	for name := range config.Sessions {
		fmt.Println(name)
	}
}

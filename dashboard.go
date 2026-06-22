package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type tickMsg time.Time

type statusUpdateMsg struct {
	sessions []*SessionItem
	err      error
}

type loginResultMsg struct {
	sessionName string
	token       *SSOToken
	err         error
}

type refreshResultMsg struct {
	sessionName string
	token       *SSOToken
	err         error
}

// SessionItem represents an SSO session in the dashboard.
type SessionItem struct {
	Name      string
	StartURL  string
	Region    string
	HasCache  bool
	IsExpired bool
	ExpiresAt string
	Remaining string
	Profiles  []string
	TokenPath string
	Token     *SSOToken
}

// Model is the bubbletea model for the dashboard TUI.
type Model struct {
	config     *AWSConfig
	sessions   []*SessionItem
	cursor     int
	err        error
	loading    bool
	statusMsg  string
	spinnerIdx int
	width      int
	height     int
	quitting   bool
}

// Lipgloss styles — used consistently instead of raw ANSI in View().
var (
	titleStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF")).
			Background(lipgloss.Color("#1A1A1A")).
			Padding(0, 1)

	headerStyle = lipgloss.NewStyle().
			Border(lipgloss.NormalBorder(), false, false, true, false).
			BorderForeground(lipgloss.Color("#333333")).
			PaddingBottom(1).
			MarginBottom(1)

	focusedStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF")).
			Background(lipgloss.Color("#222222"))

	normalStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#CCCCCC"))

	helpStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#666666"))

	successBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FF00")).
			Padding(0, 1)

	warningBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FFFF00")).
			Padding(0, 1)

	dangerBadge = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#FF0000")).
			Padding(0, 1)

	paneStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("#444444")).
			Padding(1, 2)

	dimStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("#888888"))

	boldCyanStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("#00FFFF"))
)

func runDashboard() {
	config, err := loadAWSConfig()
	if err != nil {
		printError(fmt.Sprintf("Failed to load AWS config: %v", err))
		os.Exit(1)
	}

	p := tea.NewProgram(initialModel(config), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		printError(fmt.Sprintf("Error running TUI dashboard: %v", err))
		os.Exit(1)
	}
}

func initialModel(config *AWSConfig) Model {
	return Model{
		config:   config,
		sessions: []*SessionItem{},
		cursor:   0,
		loading:  true,
	}
}

func (m Model) Init() tea.Cmd {
	return tea.Batch(
		m.loadSessionsCmd(),
		m.tickCmd(),
	)
}

func (m Model) tickCmd() tea.Cmd {
	return tea.Tick(500*time.Millisecond, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m Model) loadSessionsCmd() tea.Cmd {
	return func() tea.Msg {
		statusMap := make(map[string]*SessionItem)

		for _, profile := range m.config.Profiles {
			var sessionKey string
			var startURL string
			var ssoRegion string

			if profile.SSOSession != "" {
				sessionKey = profile.SSOSession
				if session, found := m.config.Sessions[profile.SSOSession]; found {
					startURL = session.SSOStartURL
					ssoRegion = session.SSORegion
				}
			} else if profile.SSOStartURL != "" {
				sessionKey = profile.SSOStartURL
				startURL = profile.SSOStartURL
				ssoRegion = profile.SSORegion
			} else {
				continue
			}

			if _, ok := statusMap[sessionKey]; !ok {
				statusMap[sessionKey] = &SessionItem{
					Name:     sessionKey,
					StartURL: startURL,
					Region:   ssoRegion,
				}
			}
			statusMap[sessionKey].Profiles = append(statusMap[sessionKey].Profiles, profile.Name)
		}

		sessions := make([]*SessionItem, 0, len(statusMap))
		for _, status := range statusMap {
			mockProfile := &AWSProfile{}
			if strings.HasPrefix(status.Name, "http") {
				mockProfile.SSOStartURL = status.Name
			} else {
				mockProfile.SSOSession = status.Name
			}

			tokenPath, err := getSSOTokenPath(mockProfile, m.config)
			if err == nil {
				status.TokenPath = tokenPath
				token, err := readSSOToken(tokenPath)
				if err == nil {
					status.Token = token
					status.HasCache = true
					status.IsExpired = token.IsExpired()
					status.ExpiresAt = token.ExpiresAt

					if parsed, err := time.Parse(time.RFC3339, token.ExpiresAt); err == nil {
						now := time.Now()
						if now.After(parsed) {
							status.Remaining = fmt.Sprintf("Expired %s ago", formatDuration(now.Sub(parsed)))
						} else {
							status.Remaining = fmt.Sprintf("Expires in %s", formatDuration(parsed.Sub(now)))
						}
					}
				}
			}
			sessions = append(sessions, status)
		}

		return statusUpdateMsg{sessions: sessions}
	}
}

func loginSessionCmd(session *SessionItem) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
		defer cancel()

		token, err := loginSSO(ctx, session.StartURL, session.Region, session.TokenPath)
		if err != nil {
			return loginResultMsg{sessionName: session.Name, err: err}
		}
		return loginResultMsg{sessionName: session.Name, token: token}
	}
}

func triggerRefreshCmd(session *SessionItem) tea.Cmd {
	return func() tea.Msg {
		if !session.HasCache || session.Token == nil || session.Token.RefreshToken == "" {
			return refreshResultMsg{sessionName: session.Name, err: fmt.Errorf("no refresh token available")}
		}

		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()

		newToken, err := refreshToken(ctx, session.Region, session.TokenPath, session.Token)
		if err != nil {
			return refreshResultMsg{sessionName: session.Name, err: err}
		}

		return refreshResultMsg{sessionName: session.Name, token: newToken}
	}
}

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			m.quitting = true
			return m, tea.Quit

		case "up", "k":
			if m.cursor > 0 && !m.loading {
				m.cursor--
			}

		case "down", "j":
			if m.cursor < len(m.sessions)-1 && !m.loading {
				m.cursor++
			}

		case "r":
			if len(m.sessions) > 0 && !m.loading {
				selected := m.sessions[m.cursor]
				if selected.HasCache && selected.Token != nil && selected.Token.RefreshToken != "" {
					m.loading = true
					m.statusMsg = fmt.Sprintf("Refreshing session %s...", selected.Name)
					return m, triggerRefreshCmd(selected)
				}
				m.statusMsg = "Cannot refresh: No valid refresh token cached. Press [Enter] to login."
			}

		case "enter":
			if len(m.sessions) > 0 && !m.loading {
				selected := m.sessions[m.cursor]
				m.loading = true
				m.statusMsg = fmt.Sprintf("Starting authentication for %s...", selected.Name)
				return m, loginSessionCmd(selected)
			}
		}

	case statusUpdateMsg:
		m.loading = false
		m.sessions = msg.sessions
		m.err = msg.err
		if m.cursor >= len(m.sessions) {
			m.cursor = 0
		}

	case loginResultMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Login failed: %v", msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("Successfully authenticated session %s!", msg.sessionName)
		}
		return m, m.loadSessionsCmd()

	case refreshResultMsg:
		m.loading = false
		if msg.err != nil {
			m.statusMsg = fmt.Sprintf("Refresh failed: %v", msg.err)
		} else {
			m.statusMsg = fmt.Sprintf("Successfully refreshed session %s!", msg.sessionName)
		}
		return m, m.loadSessionsCmd()

	case tickMsg:
		m.spinnerIdx++
		now := time.Now()
		for _, s := range m.sessions {
			if s.HasCache && s.Token != nil {
				if parsed, err := time.Parse(time.RFC3339, s.Token.ExpiresAt); err == nil {
					s.IsExpired = now.After(parsed)
					if s.IsExpired {
						s.Remaining = fmt.Sprintf("Expired %s ago", formatDuration(now.Sub(parsed)))
					} else {
						s.Remaining = fmt.Sprintf("Expires in %s", formatDuration(parsed.Sub(now)))
					}
				}
			}
		}
		return m, m.tickCmd()

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	}

	return m, nil
}

func (m Model) View() string {
	if m.quitting {
		return "Exiting AWS SSO Dashboard...\n"
	}

	spinners := []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}
	spinChar := spinners[m.spinnerIdx%len(spinners)]

	var sb strings.Builder

	sb.WriteString(titleStyle.Render("AWS SSO DASHBOARD"))
	sb.WriteString("  " + helpStyle.Render("Interact and manage active AWS SSO sessions"))
	sb.WriteString("\n")
	sb.WriteString(headerStyle.Render(""))

	if m.loading {
		sb.WriteString(fmt.Sprintf(" %s %s\n\n", spinChar, m.statusMsg))
	} else if m.statusMsg != "" {
		sb.WriteString(fmt.Sprintf(" ℹ %s\n\n", m.statusMsg))
	} else {
		sb.WriteString("\n")
	}

	if len(m.sessions) == 0 && !m.loading {
		sb.WriteString("No AWS SSO sessions found configured in ~/.aws/config\n")
	} else {
		for i, session := range m.sessions {
			var symbol string
			var statusStr string

			if !session.HasCache {
				symbol = dangerBadge.Render("✘")
				statusStr = dangerBadge.Render("NOT LOGGED IN")
			} else if session.IsExpired {
				symbol = warningBadge.Render("⚠")
				statusStr = warningBadge.Render("EXPIRED")
			} else {
				symbol = successBadge.Render("✔")
				statusStr = successBadge.Render("ACTIVE")
			}

			var style lipgloss.Style
			var cursorStr string
			if i == m.cursor {
				style = focusedStyle
				cursorStr = "> "
			} else {
				style = normalStyle
				cursorStr = "  "
			}

			rowContent := fmt.Sprintf(
				"%s%s Session: %-25s | Status: %-15s | %-25s",
				cursorStr,
				symbol,
				session.Name,
				statusStr,
				session.Remaining,
			)
			sb.WriteString(style.Render(rowContent) + "\n")
		}
	}

	// Detail pane — using lipgloss styles consistently instead of raw ANSI
	if len(m.sessions) > 0 && m.cursor < len(m.sessions) {
		sel := m.sessions[m.cursor]
		profilesList := strings.Join(sel.Profiles, ", ")
		if len(profilesList) > 65 {
			profilesList = profilesList[:62] + "..."
		}

		detailsBox := fmt.Sprintf(
			"%s\n  %s %s\n  %s    %s\n  %s  %s",
			boldCyanStyle.Render("Session Details"),
			dimStyle.Render("Start URL:"), sel.StartURL,
			dimStyle.Render("Region:"), sel.Region,
			dimStyle.Render("Profiles:"), profilesList,
		)
		sb.WriteString("\n" + paneStyle.Render(detailsBox) + "\n")
	}

	sb.WriteString("\n")
	sb.WriteString(helpStyle.Render("keys: [↑/↓ or k/j] navigate | [Enter] login/re-auth | [r] token refresh | [q] exit"))
	sb.WriteString("\n")

	return sb.String()
}

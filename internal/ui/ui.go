package ui

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"time"

	"heychat/internal/auth"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type msgReceipt string
type errMsg error
type typingMsg struct{}
type clearTypingMsg struct{}

type model struct {
	viewport       viewport.Model
	textarea       textarea.Model
	messages       []string
	conn           net.Conn
	sessionKey     []byte
	peerUsername   string // Dynamic username storage
	err            error
	peerTyping     bool
	lastSentTyping time.Time
	lastRecvTyping time.Time
}

func StartChat(conn net.Conn, sessionKey []byte, peerUsername string) error {
	ta := textarea.New()
	ta.Placeholder = "Type a message..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 280
	ta.SetWidth(60)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false

	vp := viewport.New(60, 15)
	vp.SetContent("Secure session established. Messages are E2E encrypted.\n")

	if peerUsername == "" {
		peerUsername = "Peer"
	}

	m := model{
		textarea:     ta,
		viewport:     vp,
		messages:     []string{},
		conn:         conn,
		sessionKey:   sessionKey,
		peerUsername: peerUsername,
	}

	p := tea.NewProgram(m, tea.WithAltScreen())

	go func() {
		reader := bufio.NewReader(conn)
		for {
			line, err := reader.ReadString('\n')
			if err != nil {
				p.Send(errMsg(err))
				return
			}
			line = strings.TrimSpace(line)
			if line == "" {
				continue
			}

			ciphertext, err := base64.StdEncoding.DecodeString(line)
			if err != nil {
				continue
			}

			plaintextBytes, err := auth.Decrypt(sessionKey, ciphertext)
			if err != nil {
				continue
			}

			plaintext := string(plaintextBytes)
			if strings.HasPrefix(plaintext, "TYP:") {
				p.Send(typingMsg{})
			} else if strings.HasPrefix(plaintext, "MSG:") {
				p.Send(msgReceipt(plaintext[4:]))
			}
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		fmt.Printf("\n[DEBUG] BubbleTea exited with error: %v\n", err)
		return err
	}
	if fm, ok := finalModel.(model); ok {
		fmt.Printf("\n[DEBUG] Chat closed. Network err: %v\n", fm.err)
		return fm.err
	}
	return nil
}

func (m model) Init() tea.Cmd {
	return textarea.Blink
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var (
		tiCmd tea.Cmd
		vpCmd tea.Cmd
	)

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.conn.Close()
			return m, tea.Quit
		case tea.KeyEsc:
			return m, nil

		case tea.KeyEnter:
			v := m.textarea.Value()
			if strings.TrimSpace(v) != "" {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
				displayStr := style.Render("You: ") + v
				m.messages = append(m.messages, displayStr)
				m.viewport.SetContent(strings.Join(m.messages, "\n"))
				m.textarea.Reset()
				m.viewport.GotoBottom()

				ciphertext, _ := auth.Encrypt(m.sessionKey, []byte("MSG:"+v))
				encoded := base64.StdEncoding.EncodeToString(ciphertext)
				m.conn.Write([]byte(encoded + "\n"))
			}
		default:
			// Throttle typing indicators to send max once every 2 seconds
			if time.Since(m.lastSentTyping) > 2*time.Second {
				m.lastSentTyping = time.Now()
				ciphertext, _ := auth.Encrypt(m.sessionKey, []byte("TYP:"))
				encoded := base64.StdEncoding.EncodeToString(ciphertext)
				m.conn.Write([]byte(encoded + "\n"))
			}
		}

	case typingMsg:
		m.peerTyping = true
		m.lastRecvTyping = time.Now()
		return m, tea.Batch(tiCmd, vpCmd, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearTypingMsg{}
		}))

	case clearTypingMsg:
		if time.Since(m.lastRecvTyping) >= 2*time.Second {
			m.peerTyping = false
		}

	case msgReceipt:
		m.peerTyping = false
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
		displayStr := style.Render(m.peerUsername+": ") + string(msg)
		m.messages = append(m.messages, displayStr)
		m.viewport.SetContent(strings.Join(m.messages, "\n"))
		m.viewport.GotoBottom()

	case errMsg:
		m.err = msg
		return m, tea.Quit
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m model) View() string {
	typingIndicator := "\n"
	if m.peerTyping {
		// Dynamically uses the peer's actual name in the typing indicator
		typingIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render(m.peerUsername + " is typing...\n")
	}

	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		typingIndicator,
		m.textarea.View(),
	)
}
package ui

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"net"
	"strings"
	"sync"
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
	peerUsername   string
	err            error
	peerTyping     bool
	lastSentTyping time.Time
	lastRecvTyping time.Time
	writeMu        *sync.Mutex // Store as a pointer to prevent copylocks warning
}

func StartChat(conn net.Conn, sessionKey []byte , peerUsername string) error {
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

	m := model{
		textarea:   ta,
		viewport:   vp,
		messages:   []string{},
		conn:       conn,
		sessionKey: sessionKey,
		writeMu:    &sync.Mutex{}, // Initialize the pointer
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

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.writeMu.Lock()
			m.conn.Close()
			m.writeMu.Unlock()
			return m, tea.Quit
		case tea.KeyEsc:
			return m, nil
		case tea.KeyEnter:
			v := strings.TrimSpace(m.textarea.Value())
			if v != "" {
				style := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
				displayStr := style.Render("You: ") + v
				m.messages = append(m.messages, displayStr)
				m.viewport.SetContent(strings.Join(m.messages, "\n"))
				m.textarea.Reset()
				m.viewport.GotoBottom()

				ciphertext, err := auth.Encrypt(m.sessionKey, []byte("MSG:"+v))
				if err == nil {
					encoded := base64.StdEncoding.EncodeToString(ciphertext)
					m.writeMu.Lock()
					_, writeErr := m.conn.Write([]byte(encoded + "\n"))
					m.writeMu.Unlock()
					if writeErr != nil {
						m.err = writeErr
						return m, tea.Quit
					}
				}
			}
			return m, nil
		}

	case typingMsg:
		m.peerTyping = true
		m.lastRecvTyping = time.Now()
		return m, tea.Tick(2*time.Second, func(time.Time) tea.Msg {
			return clearTypingMsg{}
		})

	case clearTypingMsg:
		if time.Since(m.lastRecvTyping) >= 2*time.Second {
			m.peerTyping = false
		}
		return m, nil

	case msgReceipt:
		m.peerTyping = false
		style := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)

		
		displayStr := style.Render(m.peerUsername+": ") + string(msg)
		m.messages = append(m.messages, displayStr)
		m.viewport.SetContent(strings.Join(m.messages, "\n"))
		m.viewport.GotoBottom()
		return m, nil

	case errMsg:
		m.err = msg
		return m, tea.Quit
	}

	m.textarea, tiCmd = m.textarea.Update(msg)
	m.viewport, vpCmd = m.viewport.Update(msg)
	return m, tea.Batch(tiCmd, vpCmd)
}

func (m model) View() string {
	typingIndicator := "\n"
	if m.peerTyping {
		typingIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("240")).Render("Peer is typing...\n")
	}

	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		typingIndicator,
		m.textarea.View(),
	)
}
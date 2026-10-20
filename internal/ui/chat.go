package ui

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"io"
	"net"
	"os/exec"
	"strings"
	"time"

	"heychat/internal/history"

	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

type netMsg string
type netErrMsg error
type tickMsg time.Time

type displayMsg struct {
	id        string
	sender    string
	text      string
	timestamp time.Time
}

type chatModel struct {
	conn       net.Conn
	gcm        cipher.AEAD
	peerUser   string
	viewport   viewport.Model
	textInput  textinput.Model
	messages   []displayMsg
	peerTyping time.Time

	askMicPerm  bool
	recording   bool
	recordStart time.Time
	recordCmd   *exec.Cmd
	recordFile  string
	lastVoiceID string
}

func StartChat(conn net.Conn, sessionKey []byte, peerUser string) error {
	block, err := aes.NewCipher(sessionKey)
	if err != nil {
		return err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return err
	}

	ti := textinput.New()
	ti.Placeholder = "Type a message, /voice to record (30s max), or /delete..."
	ti.Focus()
	ti.CharLimit = 1000
	ti.Width = 70

	vp := viewport.New(80, 18)

	// Load non-expired messages (< 10 mins old) from disk
	saved := history.LoadAndClean(peerUser)
	var msgs []displayMsg
	for _, s := range saved {
		msgs = append(msgs, displayMsg{
			sender:    s.Sender,
			text:      s.Text,
			timestamp: s.Timestamp,
		})
	}

	m := chatModel{
		conn:      conn,
		gcm:       gcm,
		peerUser:  peerUser,
		viewport:  vp,
		textInput: ti,
		messages:  msgs,
	}
	m.refreshViewport()

	p := tea.NewProgram(m, tea.WithAltScreen())

	// Background TCP reader (10MB buffer to support 30s WAV voice payloads)
	go func() {
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			plain, err := decryptPacket(gcm, scanner.Text())
			if err == nil {
				p.Send(netMsg(plain))
			}
		}
		if err := scanner.Err(); err != nil {
			p.Send(netErrMsg(err))
		} else {
			p.Send(netErrMsg(io.EOF))
		}
	}()

	_, err = p.Run()
	return err
}

func (m chatModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, tickEverySecond())
}

func tickEverySecond() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg {
		return tickMsg(t)
	})
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 5
		m.textInput.Width = msg.Width - 4
		m.refreshViewport()
		return m, nil

	case tickMsg:
		// 1. Auto-stop and send if recording reaches 30 seconds
		if m.recording && time.Since(m.recordStart) >= MaxRecordSeconds*time.Second {
			m.finishAndSendVoice()
		}
		// 2. Purge any messages older than 10 minutes from the live screen
		m.purgeExpiredMessages()
		return m, tickEverySecond()

	case netMsg:
		raw := string(msg)
		switch {
		case raw == "TYPING":
			m.peerTyping = time.Now()

		case strings.HasPrefix(raw, "TEXT|"):
			text := strings.TrimPrefix(raw, "TEXT|")
			m.addMessage("", m.peerUser, text)

		case strings.HasPrefix(raw, "VOICE|"):
			parts := strings.SplitN(raw, "|", 3)
			if len(parts) == 3 {
				msgID, b64Audio := parts[1], parts[2]
				_, _ = SaveAndPlayVoiceNote(msgID, b64Audio)
				m.addMessage(msgID, m.peerUser, fmt.Sprintf("🔊 [Voice Note #%s - Playing]", msgID))
			}

		case strings.HasPrefix(raw, "RECALL|"):
			msgID := strings.TrimPrefix(raw, "RECALL|")
			DeleteVoiceNote(msgID)
			m.markDeleted(msgID)
		}
		return m, nil

	case netErrMsg:
		if m.recording {
			CancelRecording(m.recordCmd, m.recordFile)
		}
		return m, tea.Quit

	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			if m.recording {
				CancelRecording(m.recordCmd, m.recordFile)
			}
			return m, tea.Quit

		case tea.KeyEsc:
			if m.askMicPerm {
				m.askMicPerm = false
				return m, nil
			}
			if m.recording {
				CancelRecording(m.recordCmd, m.recordFile)
				m.recording = false
				return m, nil
			}
			return m, tea.Quit

		case tea.KeyEnter:
			// Confirm first-time microphone permission prompt
			if m.askMicPerm {
				m.askMicPerm = false
				cmd, file, err := StartRecording()
				if err == nil {
					m.recordCmd = cmd
					m.recordFile = file
					m.recording = true
					m.recordStart = time.Now()
				}
				return m, nil
			}

			// If currently recording, stop and send voice note
			if m.recording {
				m.finishAndSendVoice()
				return m, nil
			}

			text := strings.TrimSpace(m.textInput.Value())
			if text == "" {
				return m, nil
			}
			m.textInput.Reset()

			// Handle /voice command
			if text == "/voice" {
				if CheckFirstTimeMicPermission() {
					m.askMicPerm = true
					return m, nil
				}
				cmd, file, err := StartRecording()
				if err == nil {
					m.recordCmd = cmd
					m.recordFile = file
					m.recording = true
					m.recordStart = time.Now()
				} else {
					m.addMessage("", "System", "Microphone error: "+err.Error())
				}
				return m, nil
			}

			// Handle /delete command
			if text == "/delete" {
				if m.lastVoiceID != "" {
					_ = sendPacket(m.conn, m.gcm, "RECALL|"+m.lastVoiceID)
					DeleteVoiceNote(m.lastVoiceID)
					m.markDeleted(m.lastVoiceID)
					m.lastVoiceID = ""
				} else {
					m.addMessage("", "System", "No recent voice note to delete.")
				}
				return m, nil
			}

			// Send normal text message
			_ = sendPacket(m.conn, m.gcm, "TEXT|"+text)
			m.addMessage("", "You", text)
			return m, nil

		default:
			if !m.recording && !m.askMicPerm {
				_ = sendPacket(m.conn, m.gcm, "TYPING")
			}
		}
	}

	var cmd tea.Cmd
	if !m.recording && !m.askMicPerm {
		m.textInput, cmd = m.textInput.Update(msg)
	}
	return m, cmd
}

func (m *chatModel) finishAndSendVoice() {
	b64, err := StopRecording(m.recordCmd, m.recordFile)
	m.recording = false
	if err != nil {
		m.addMessage("", "System", "Failed to capture audio.")
		return
	}

	msgID := fmt.Sprintf("%d", time.Now().Unix()%100000)
	if err := sendPacket(m.conn, m.gcm, "VOICE|"+msgID+"|"+b64); err == nil {
		m.lastVoiceID = msgID
		m.addMessage(msgID, "You", fmt.Sprintf("🎙️ [Voice Note #%s Sent] (Type /delete to unsent)", msgID))
	}
}

func (m *chatModel) addMessage(id, sender, text string) {
	m.messages = append(m.messages, displayMsg{
		id:        id,
		sender:    sender,
		text:      text,
		timestamp: time.Now(),
	})
	if sender != "System" {
		history.AppendMessage(m.peerUser, sender, text)
	}
	m.refreshViewport()
}

func (m *chatModel) markDeleted(msgID string) {
	for i := range m.messages {
		if m.messages[i].id == msgID {
			m.messages[i].text = "🚫 [Voice note deleted]"
		}
	}
	m.refreshViewport()
}

func (m *chatModel) purgeExpiredMessages() {
	cutoff := time.Now().Add(-history.MessageTTL)
	var valid []displayMsg
	changed := false
	for _, msg := range m.messages {
		if msg.timestamp.After(cutoff) {
			valid = append(valid, msg)
		} else {
			changed = true
		}
	}
	if changed {
		m.messages = valid
		m.refreshViewport()
	}
}

func (m *chatModel) refreshViewport() {
	var lines []string
	for _, msg := range m.messages {
		ts := msg.timestamp.Format("15:04")
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", ts, msg.sender, msg.text))
	}
	m.viewport.SetContent(strings.Join(lines, "\n"))
	m.viewport.GotoBottom()
}

func (m chatModel) View() string {
	headerStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	recStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
	permStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	header := headerStyle.Render(fmt.Sprintf("🔒 Encrypted Chat with %s (Messages & Audio self-destruct in 10m)", m.peerUser))

	typingStatus := ""
	if time.Since(m.peerTyping) < 2*time.Second {
		typingStatus = fmt.Sprintf("\n%s is typing...", m.peerUser)
	}

	var bottomBar string
	if m.askMicPerm {
		bottomBar = permStyle.Render("\n[🎙️ Permission] HeyChat needs microphone access for voice notes. Press ENTER to allow, or ESC to cancel.")
	} else if m.recording {
		elapsed := int(time.Since(m.recordStart).Seconds())
		bottomBar = recStyle.Render(fmt.Sprintf("\n[🔴 Recording... %02ds / %02ds] Press ENTER to send, ESC to cancel.", elapsed, MaxRecordSeconds))
	} else {
		bottomBar = "\n" + m.textInput.View()
	}

	return fmt.Sprintf("%s\n\n%s%s\n%s\n", header, m.viewport.View(), typingStatus, bottomBar)
}

func sendPacket(conn net.Conn, gcm cipher.AEAD, plain string) error {
	nonce := make([]byte, gcm.NonceSize())
	if _, err := io.ReadFull(rand.Reader, nonce); err != nil {
		return err
	}
	sealed := gcm.Seal(nonce, nonce, []byte(plain), nil)
	encoded := base64.StdEncoding.EncodeToString(sealed)
	_, err := fmt.Fprintln(conn, encoded)
	return err
}

func decryptPacket(gcm cipher.AEAD, encoded string) (string, error) {
	raw, err := base64.StdEncoding.DecodeString(strings.TrimSpace(encoded))
	if err != nil {
		return "", err
	}
	nonceSize := gcm.NonceSize()
	if len(raw) < nonceSize {
		return "", fmt.Errorf("ciphertext too short")
	}
	nonce, ciphertext := raw[:nonceSize], raw[nonceSize:]
	plain, err := gcm.Open(nil, nonce, ciphertext, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
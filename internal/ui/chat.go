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
	"strconv"
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

type playDoneMsg struct {
	seqID string
	err   error
}

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
	voiceCount  int
	lastVoiceID string

	playingID    string
	playStart    time.Time
	playDuration int
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
	ti.Placeholder = "Message | /voice | /play <1,2..> | /delete <1,2..>"
	ti.Focus()
	ti.CharLimit = 1000
	ti.Width = 70

	vp := viewport.New(80, 18)

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

	go func() {
		scanner := bufio.NewScanner(conn)
		scanner.Buffer(make([]byte, 64*1024), 10*1024*1024)
		for scanner.Scan() {
			plain, err := decryptPacket(gcm, scanner.Text())
			if err == nil {
				p.Send(netMsg(plain))
			}
		}
		_ = scanner.Err()
		p.Send(netErrMsg(io.EOF))
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

func playVoiceCmd(seqID string) tea.Cmd {
	filePath := GetVoiceNotePath(seqID)
	return func() tea.Msg {
		err := PlayAudioSync(filePath)
		return playDoneMsg{seqID: seqID, err: err}
	}
}

func (m chatModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - 6
		m.textInput.Width = msg.Width - 4
		m.refreshViewport()
		return m, nil

	case playDoneMsg:
		if m.playingID == msg.seqID {
			m.playingID = ""
		}
		if msg.err != nil {
			m.addMessage("", "System", fmt.Sprintf("Playback error (#%s): %v", msg.seqID, msg.err))
		}
		return m, nil

	case tickMsg:
		if m.recording && time.Since(m.recordStart) >= MaxRecordSeconds*time.Second {
			m.finishAndSendVoice()
		}
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
				seqID, b64Audio := parts[1], parts[2]
				if n, err := strconv.Atoi(seqID); err == nil && n > m.voiceCount {
					m.voiceCount = n
				}
				m.lastVoiceID = seqID

				if dur, err := SaveVoiceNote(seqID, b64Audio); err == nil {
					m.playingID = seqID
					m.playStart = time.Now()
					m.playDuration = dur
					m.addMessage(seqID, m.peerUser, fmt.Sprintf("🔊 [Voice Note #%s (%02ds)] (Type '/play %s' or '/delete %s')", seqID, dur, seqID, seqID))
					return m, playVoiceCmd(seqID)
				}
			}

		case strings.HasPrefix(raw, "RECALL|"):
			seqID := strings.TrimPrefix(raw, "RECALL|")
			DeleteVoiceNote(seqID)
			if m.playingID == seqID {
				m.playingID = ""
			}
			m.markDeleted(seqID)
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
			if m.askMicPerm {
				m.askMicPerm = false
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

			if m.recording {
				m.finishAndSendVoice()
				return m, nil
			}

			text := strings.TrimSpace(m.textInput.Value())
			if text == "" {
				return m, nil
			}
			m.textInput.Reset()

			// 1. /voice command
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

			// 2. /play or /play <number> command
			if text == "/play" || strings.HasPrefix(text, "/play ") {
				targetID := strings.TrimSpace(strings.TrimPrefix(text, "/play"))
				if targetID == "" {
					targetID = m.lastVoiceID
				}
				targetID = strings.TrimPrefix(targetID, "#")

				if targetID == "" {
					m.addMessage("", "System", "No voice note to play. Usage: /play 1")
					return m, nil
				}
				dur, err := GetVoiceNoteDuration(targetID)
				if err != nil {
					m.addMessage("", "System", err.Error())
					return m, nil
				}
				m.playingID = targetID
				m.playStart = time.Now()
				m.playDuration = dur
				return m, playVoiceCmd(targetID)
			}

			// 3. /delete or /delete <number> command
			if text == "/delete" || strings.HasPrefix(text, "/delete ") {
				targetID := strings.TrimSpace(strings.TrimPrefix(text, "/delete"))
				if targetID == "" {
					targetID = m.lastVoiceID
				}
				targetID = strings.TrimPrefix(targetID, "#")

				if targetID == "" {
					m.addMessage("", "System", "Specify a voice note to delete. Usage: /delete 1")
				} else {
					_ = sendPacket(m.conn, m.gcm, "RECALL|"+targetID)
					DeleteVoiceNote(targetID)
					if m.playingID == targetID {
						m.playingID = ""
					}
					m.markDeleted(targetID)
				}
				return m, nil
			}

			// 4. Normal text message
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
	b64, dur, peakPct, err := StopRecording(m.recordCmd, m.recordFile)
	m.recording = false
	if err != nil {
		m.addMessage("", "System", "Failed to capture audio: "+err.Error())
		return
	}

	if peakPct == 0 {
		m.addMessage("", "System", "⚠️ Microphone recorded 0% volume (silence). Check your hardware mic mute switch.")
		return
	}

	m.voiceCount++
	seqID := strconv.Itoa(m.voiceCount)

	if err := sendPacket(m.conn, m.gcm, "VOICE|"+seqID+"|"+b64); err == nil {
		_, _ = SaveVoiceNote(seqID, b64)
		m.lastVoiceID = seqID
		m.addMessage(seqID, "You", fmt.Sprintf("🎙️ [Voice Note #%s (%02ds)] (Type '/play %s' or '/delete %s')", seqID, dur, seqID, seqID))
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

func (m *chatModel) markDeleted(seqID string) {
	for i := range m.messages {
		if m.messages[i].id == seqID {
			m.messages[i].text = fmt.Sprintf("🚫 [Voice Note #%s deleted]", seqID)
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
	playStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
	permStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))

	header := headerStyle.Render(fmt.Sprintf("🔒 Encrypted Chat with %s (Messages & Audio self-destruct in 10m)", m.peerUser))

	statusLine := ""
	if m.playingID != "" {
		elapsed := int(time.Since(m.playStart).Seconds())
		if elapsed > m.playDuration {
			elapsed = m.playDuration
		}
		statusLine = "\n" + playStyle.Render(fmt.Sprintf("[▶️ Playing Voice Note #%s... %02ds / %02ds]", m.playingID, elapsed, m.playDuration))
	} else if time.Since(m.peerTyping) < 2*time.Second {
		statusLine = fmt.Sprintf("\n%s is typing...", m.peerUser)
	}

	var bottomBar string
	if m.askMicPerm {
		bottomBar = permStyle.Render("\n[🎙️ Permission] HeyChat needs microphone access for voice notes. Press ENTER to allow, or ESC to cancel.")
	} else if m.recording {
		elapsed := int(time.Since(m.recordStart).Seconds())
		bottomBar = recStyle.Render(fmt.Sprintf("\n[🔴 Recording Voice Note #%d... %02ds / %02ds] Press ENTER to send, ESC to cancel.", m.voiceCount+1, elapsed, MaxRecordSeconds))
	} else {
		bottomBar = "\n" + m.textInput.View()
	}

	return fmt.Sprintf("%s\n\n%s%s\n%s\n", header, m.viewport.View(), statusLine, bottomBar)
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
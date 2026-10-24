package ui

import (
	"bufio"
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
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

type msgKind int

const (
	kindText msgKind = iota
	kindVoice
	kindImage
	kindSystem
	kindDeleted
)

type displayMsg struct {
	id        string
	sender    string
	text      string
	kind      msgKind
	duration  int
	timestamp time.Time
}

type chatModel struct {
	conn       net.Conn
	gcm        cipher.AEAD
	peerUser   string
	width      int
	height     int
	viewport   viewport.Model
	textInput  textinput.Model
	messages   []displayMsg
	peerTyping time.Time
	tickCount  int

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

var (
	headerBarStyle = lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("255")).
			Background(lipgloss.Color("62")).
			Padding(0, 2)

	youNameStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("205"))
	 youBorder   = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("205")).
			PaddingLeft(1)

	peerNameStyle = lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("45"))
	peerBorder    = lipgloss.NewStyle().
			Border(lipgloss.ThickBorder(), false, false, false, true).
			BorderForeground(lipgloss.Color("45")).
			PaddingLeft(1)

	sysStyle = lipgloss.NewStyle().
			Foreground(lipgloss.Color("214")).
			Italic(true).
			PaddingLeft(1)

	timeStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	ttlStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("239"))

	voiceBoxStyle = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("99")).
			Padding(0, 1)

	voiceActiveBoxStyle = lipgloss.NewStyle().
				Border(lipgloss.RoundedBorder()).
				BorderForeground(lipgloss.Color("42")).
				Padding(0, 1)

	inputBoxNormal = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("62")).
			Padding(0, 1)

	inputBoxRecord = lipgloss.NewStyle().
			Border(lipgloss.RoundedBorder()).
			BorderForeground(lipgloss.Color("196")).
			Padding(0, 1)

	footerStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("241"))
)

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
	ti.Prompt = "❯ "
	ti.Placeholder = "Type a message, /voice, /play 1, /delete 1, or send <image_path>..."
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
			kind:      kindText,
			timestamp: s.Timestamp,
		})
	}

	m := chatModel{
		conn:      conn,
		gcm:       gcm,
		peerUser:  peerUser,
		width:     84,
		height:    26,
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
	var vpCmd tea.Cmd

	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport.Width = msg.Width - 2
		m.viewport.Height = msg.Height - 8
		if m.viewport.Height < 5 {
			m.viewport.Height = 5
		}
		m.textInput.Width = msg.Width - 8
		m.refreshViewport()
		return m, nil

	case playDoneMsg:
		if m.playingID == msg.seqID {
			m.playingID = ""
		}
		if msg.err != nil {
			m.addSystemMessage(fmt.Sprintf("Playback error (#%s): %v", msg.seqID, msg.err))
		}
		m.refreshViewport()
		return m, nil

	case tickMsg:
		m.tickCount++
		if m.recording && time.Since(m.recordStart) >= MaxRecordSeconds*time.Second {
			m.finishAndSendVoice()
		}
		m.purgeExpiredMessages()
		m.refreshViewport()
		return m, tickEverySecond()

	case netMsg:
		raw := string(msg)
		switch {
		case raw == "TYPING":
			m.peerTyping = time.Now()

		case strings.HasPrefix(raw, "TEXT|"):
			text := strings.TrimPrefix(raw, "TEXT|")
			m.addTextMessage(m.peerUser, text)

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
					m.addVoiceMessage(seqID, m.peerUser, dur)
					return m, playVoiceCmd(seqID)
				}
			}

		case strings.HasPrefix(raw, "IMG|"):
			parts := strings.SplitN(raw, "|", 3)
			if len(parts) == 3 {
				filename, b64Data := parts[1], parts[2]
				if imgBytes, err := base64.StdEncoding.DecodeString(b64Data); err == nil {
					if tempFile, err := os.CreateTemp("", "hey-*_"+filename); err == nil {
						tempPath := tempFile.Name()
						_, _ = tempFile.Write(imgBytes)
						_ = tempFile.Close()
						art := renderPixelArt(tempPath, 42)
						body := fmt.Sprintf("🖼️ %s\n%s\n📂 %s", filename, art, tempPath)
						m.addImageMessage(m.peerUser, body)
					}
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

		case tea.KeyPgUp, tea.KeyPgDown, tea.KeyUp, tea.KeyDown:
			m.viewport, vpCmd = m.viewport.Update(msg)
			return m, vpCmd

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
					m.addSystemMessage("Microphone error: " + err.Error())
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
					m.addSystemMessage("Microphone error: " + err.Error())
				}
				return m, nil
			}

			// 2. /play or /play <number>
			if text == "/play" || strings.HasPrefix(text, "/play ") {
				targetID := strings.TrimSpace(strings.TrimPrefix(text, "/play"))
				if targetID == "" {
					targetID = m.lastVoiceID
				}
				targetID = strings.TrimPrefix(targetID, "#")

				if targetID == "" {
					m.addSystemMessage("No voice note to play. Usage: /play 1")
					return m, nil
				}
				dur, err := GetVoiceNoteDuration(targetID)
				if err != nil {
					m.addSystemMessage(err.Error())
					return m, nil
				}
				m.playingID = targetID
				m.playStart = time.Now()
				m.playDuration = dur
				m.refreshViewport()
				return m, playVoiceCmd(targetID)
			}

			// 3. /delete or /delete <number>
			if text == "/delete" || strings.HasPrefix(text, "/delete ") {
				targetID := strings.TrimSpace(strings.TrimPrefix(text, "/delete"))
				if targetID == "" {
					targetID = m.lastVoiceID
				}
				targetID = strings.TrimPrefix(targetID, "#")

				if targetID == "" {
					m.addSystemMessage("Specify a voice note to delete. Usage: /delete 1")
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

			// 4. Inline image command: send <path> or $send <path>
			cmdClean := strings.TrimPrefix(text, "$")
			if strings.HasPrefix(cmdClean, "send ") {
				rawPath := strings.TrimSpace(strings.TrimPrefix(cmdClean, "send "))
				resolved := resolvePath(rawPath)
				fileData, err := os.ReadFile(resolved)
				if err != nil {
					m.addSystemMessage("Cannot read image file: " + err.Error())
					return m, nil
				}
				art := renderPixelArt(resolved, 42)
				if strings.HasPrefix(art, "[Error") {
					m.addSystemMessage("Unsupported image format (use PNG or JPG).")
					return m, nil
				}
				filename := filepath.Base(resolved)
				b64Data := base64.StdEncoding.EncodeToString(fileData)
				_ = sendPacket(m.conn, m.gcm, "IMG|"+filename+"|"+b64Data)
				body := fmt.Sprintf("🖼️ %s\n%s\n📂 %s", filename, art, resolved)
				m.addImageMessage("You", body)
				return m, nil
			}

			// 5. Normal text message
			_ = sendPacket(m.conn, m.gcm, "TEXT|"+text)
			m.addTextMessage("You", text)
			return m, nil

		default:
			if !m.recording && !m.askMicPerm {
				_ = sendPacket(m.conn, m.gcm, "TYPING")
			}
		}
	}

	var tiCmd tea.Cmd
	if !m.recording && !m.askMicPerm {
		m.textInput, tiCmd = m.textInput.Update(msg)
	}
	return m, tiCmd
}

func (m *chatModel) finishAndSendVoice() {
	b64, dur, peakPct, err := StopRecording(m.recordCmd, m.recordFile)
	m.recording = false
	if err != nil {
		m.addSystemMessage("Failed to capture audio: " + err.Error())
		return
	}

	if peakPct == 0 {
		m.addSystemMessage("Microphone recorded 0% volume (silence). Check your mic mute switch.")
		return
	}

	m.voiceCount++
	seqID := strconv.Itoa(m.voiceCount)

	if err := sendPacket(m.conn, m.gcm, "VOICE|"+seqID+"|"+b64); err == nil {
		_, _ = SaveVoiceNote(seqID, b64)
		m.lastVoiceID = seqID
		m.addVoiceMessage(seqID, "You", dur)
	}
}

func (m *chatModel) addTextMessage(sender, text string) {
	m.messages = append(m.messages, displayMsg{
		sender:    sender,
		text:      text,
		kind:      kindText,
		timestamp: time.Now(),
	})
	history.AppendMessage(m.peerUser, sender, text)
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *chatModel) addVoiceMessage(seqID, sender string, duration int) {
	label := fmt.Sprintf("🎙️ Voice Note #%s (%02ds) — /play %s | /delete %s", seqID, duration, seqID, seqID)
	m.messages = append(m.messages, displayMsg{
		id:        seqID,
		sender:    sender,
		text:      label,
		kind:      kindVoice,
		duration:  duration,
		timestamp: time.Now(),
	})
	history.AppendMessage(m.peerUser, sender, label)
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *chatModel) addImageMessage(sender, body string) {
	m.messages = append(m.messages, displayMsg{
		sender:    sender,
		text:      body,
		kind:      kindImage,
		timestamp: time.Now(),
	})
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *chatModel) addSystemMessage(text string) {
	m.messages = append(m.messages, displayMsg{
		sender:    "System",
		text:      text,
		kind:      kindSystem,
		timestamp: time.Now(),
	})
	m.refreshViewport()
	m.viewport.GotoBottom()
}

func (m *chatModel) markDeleted(seqID string) {
	for i := range m.messages {
		if m.messages[i].id == seqID {
			m.messages[i].kind = kindDeleted
			m.messages[i].text = fmt.Sprintf("🚫 Voice Note #%s was recalled", seqID)
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
	}
}

func formatRemainingTTL(ts time.Time) string {
	rem := time.Until(ts.Add(history.MessageTTL))
	if rem <= 0 {
		return "expiring"
	}
	mins := int(rem.Minutes())
	secs := int(rem.Seconds()) % 60
	return fmt.Sprintf("⏳ %dm%02ds", mins, secs)
}

func renderVoiceCard(seqID string, dur int, isPlaying bool, tick int) string {
	waves := []string{
		"▂▄▆█▄▂▇▃▅▂▄▇█▅▃▂",
		"▄▆█▂▄▃▇▅█▄▂▆▃▇",
		"▆▃▇▂▄▆█▄▂▇▃▅▂▄▆",
	}
	wave := waves[0]
	statusIcon := "🎙️"
	if isPlaying {
		wave = waves[tick%len(waves)]
		statusIcon = "🔊 PLAYING"
	} else {
		statusIcon = "🎙️ VOICE"
	}

	top := fmt.Sprintf("%s #%s   %s   00:%02d", statusIcon, seqID, wave, dur)
	hint := timeStyle.Render(fmt.Sprintf("↳ Type '/play %s' to listen  •  '/delete %s' to recall", seqID, seqID))
	content := top + "\n" + hint

	if isPlaying {
		return voiceActiveBoxStyle.Render(content)
	}
	return voiceBoxStyle.Render(content)
}

func (m *chatModel) refreshViewport() {
	if len(m.messages) == 0 {
		empty := lipgloss.NewStyle().
			Foreground(lipgloss.Color("241")).
			Italic(true).
			Render("\n  No messages yet. Say hello or type /voice to send a voice note!")
		m.viewport.SetContent(empty)
		return
	}

	var blocks []string
	for _, msg := range m.messages {
		ts := timeStyle.Render(msg.timestamp.Format("15:04:05"))
		ttl := ttlStyle.Render("(" + formatRemainingTTL(msg.timestamp) + ")")

		if msg.kind == kindSystem {
			blocks = append(blocks, sysStyle.Render(fmt.Sprintf("⚡ [%s] %s", msg.timestamp.Format("15:04:05"), msg.text)))
			continue
		}

		var body string
		switch msg.kind {
		case kindVoice:
			isPlaying := m.playingID != "" && m.playingID == msg.id
			body = renderVoiceCard(msg.id, msg.duration, isPlaying, m.tickCount)
		case kindDeleted:
			body = lipgloss.NewStyle().Foreground(lipgloss.Color("242")).Italic(true).Render(msg.text)
		default:
			body = msg.text
		}

		if msg.sender == "You" {
			header := fmt.Sprintf("%s  %s %s", youNameStyle.Render("● You"), ts, ttl)
			blocks = append(blocks, youBorder.Render(header+"\n"+body))
		} else {
			header := fmt.Sprintf("%s  %s %s", peerNameStyle.Render("● "+msg.sender), ts, ttl)
			blocks = append(blocks, peerBorder.Render(header+"\n"+body))
		}
	}

	m.viewport.SetContent(strings.Join(blocks, "\n\n"))
}

func renderProgressBar(elapsed, total, width int) string {
	if total <= 0 {
		total = 1
	}
	if elapsed > total {
		elapsed = total
	}
	filled := (elapsed * width) / total
	if filled > width {
		filled = width
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func (m chatModel) View() string {
	w := m.width
	if w < 50 {
		w = 80
	}

	// 1. Top Header Bar
	title := fmt.Sprintf("🔒 HEY E2E  │  💬 @%s  │  ⏳ 10m Self-Destruct  │  🎙️ %d Voice Notes", m.peerUser, m.voiceCount)
	header := headerBarStyle.Width(w).Render(title)

	// 2. Live Activity / Status Strip
	statusStrip := ""
	if m.playingID != "" {
		elapsed := int(time.Since(m.playStart).Seconds())
		bar := renderProgressBar(elapsed, m.playDuration, 18)
		playBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("42"))
		statusStrip = playBadge.Render(fmt.Sprintf("  🔊 Playing Voice Note #%s  [%s] %02ds / %02ds", m.playingID, bar, elapsed, m.playDuration))
	} else if time.Since(m.peerTyping) < 2*time.Second {
		dots := strings.Repeat(".", (m.tickCount%3)+1)
		typingBadge := lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Italic(true)
		statusStrip = typingBadge.Render(fmt.Sprintf("  ✍️  %s is typing%s", m.peerUser, dots))
	}

	// 3. Bottom Input / Recording Box
	var inputSection string
	if m.askMicPerm {
		permBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("214"))
		content := permBadge.Render("🎙️  Allow microphone access for voice notes?   [ENTER = Allow  •  ESC = Cancel]")
		inputSection = inputBoxRecord.Width(w - 4).Render(content)
	} else if m.recording {
		elapsed := int(time.Since(m.recordStart).Seconds())
		bar := renderProgressBar(elapsed, MaxRecordSeconds, 20)
		recBadge := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("196"))
		content := recBadge.Render(fmt.Sprintf("🔴 REC Voice Note #%d  [%s] %02ds/%02ds   [ENTER = Send • ESC = Cancel]", m.voiceCount+1, bar, elapsed, MaxRecordSeconds))
		inputSection = inputBoxRecord.Width(w - 4).Render(content)
	} else {
		inputSection = inputBoxNormal.Width(w - 4).Render(m.textInput.View())
	}

	// 4. Footer Command Hints
	footer := footerStyle.Render("  Enter: Send  •  /voice: Record  •  /play <#>: Listen  •  /delete <#>: Recall  •  send <img_path>  •  PgUp/PgDn: Scroll  •  Esc: Quit")

	return fmt.Sprintf("%s\n%s\n%s\n%s\n%s", header, m.viewport.View(), statusStrip, inputSection, footer)
}

func resolvePath(path string) string {
	path = strings.Trim(path, `"'`)
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	if abs, err := filepath.Abs(path); err == nil {
		return abs
	}
	return path
}

// renderPixelArt converts an image to ANSI half-blocks using pure Go scaling (zero external dependencies).
func renderPixelArt(path string, targetW int) string {
	f, err := os.Open(path)
	if err != nil {
		return "[Error opening image]"
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "[Error decoding image]"
	}

	b := img.Bounds()
	srcW, srcH := b.Dx(), b.Dy()
	if srcW <= 0 || srcH <= 0 {
		return "[Error invalid image dimensions]"
	}

	targetH := (srcH * targetW) / srcW
	if targetH < 2 {
		targetH = 2
	}
	if targetH%2 != 0 {
		targetH++
	}

	samplePixel := func(tx, ty int) (uint8, uint8, uint8) {
		sx := b.Min.X + (tx*srcW)/targetW
		sy := b.Min.Y + (ty*srcH)/targetH
		r, g, bl, _ := img.At(sx, sy).RGBA()
		return uint8(r >> 8), uint8(g >> 8), uint8(bl >> 8)
	}

	var sb strings.Builder
	for y := 0; y < targetH; y += 2 {
		for x := 0; x < targetW; x++ {
			tr, tg, tb := samplePixel(x, y)
			br, bg, bb := samplePixel(x, y+1)

			topHex := fmt.Sprintf("#%02x%02x%02x", tr, tg, tb)
			botHex := fmt.Sprintf("#%02x%02x%02x", br, bg, bb)

			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color(topHex)).
				Background(lipgloss.Color(botHex))
			sb.WriteString(style.Render("▀"))
		}
		if y+2 < targetH {
			sb.WriteString("\n")
		}
	}
	return sb.String()
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
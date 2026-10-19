package ui

import (
	"bufio"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"heychat/internal/auth"

	"github.com/charmbracelet/bubbles/textarea"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/nfnt/resize"
)

type msgReceipt string
type imgReceipt string
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
}

// renderPixelArt converts an image to high-quality ANSI half-blocks
func renderPixelArt(path string, width int) string {
	f, err := os.Open(path)
	if err != nil {
		return "[Error opening image]"
	}
	defer f.Close()

	img, _, err := image.Decode(f)
	if err != nil {
		return "[Error decoding image]"
	}

	bounds := img.Bounds()
	origW := float64(bounds.Dx())
	origH := float64(bounds.Dy())
	
	targetH := uint((origH / origW) * float64(width))

	resizedImg := resize.Resize(uint(width), targetH, img, resize.Lanczos3)
	resBounds := resizedImg.Bounds()

	var sb strings.Builder
	for y := resBounds.Min.Y; y < resBounds.Max.Y; y += 2 {
		for x := resBounds.Min.X; x < resBounds.Max.X; x++ {
			top := resizedImg.At(x, y)
			var bot color.Color = color.Transparent
			
			if y+1 < resBounds.Max.Y {
				bot = resizedImg.At(x, y+1)
			}

			tr, tg, tb, _ := top.RGBA()
			br, bg, bb, _ := bot.RGBA()

			topHex := fmt.Sprintf("#%02x%02x%02x", uint8(tr>>8), uint8(tg>>8), uint8(tb>>8))
			botHex := fmt.Sprintf("#%02x%02x%02x", uint8(br>>8), uint8(bg>>8), uint8(bb>>8))

			style := lipgloss.NewStyle().
				Foreground(lipgloss.Color(topHex)).
				Background(lipgloss.Color(botHex))

			sb.WriteString(style.Render("▀"))
		}
		sb.WriteString("\n")
	}
	return sb.String()
}

// resolvePath expands ~ and resolves relative paths
func resolvePath(path string) string {
	path = strings.Trim(path, `"'`) // Strip quotes if user dragged-and-dropped
	if strings.HasPrefix(path, "~/") {
		if home, err := os.UserHomeDir(); err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	abs, err := filepath.Abs(path)
	if err != nil {
		return path
	}
	return abs
}

func StartChat(conn net.Conn, sessionKey []byte, peerUsername string) error {
	ta := textarea.New()
	ta.Placeholder = "Type a message or 'send path/to/image.jpg'..."
	ta.Focus()
	ta.Prompt = "┃ "
	ta.CharLimit = 1000
	ta.SetWidth(65)
	ta.SetHeight(3)
	ta.FocusedStyle.CursorLine = lipgloss.NewStyle()
	ta.ShowLineNumbers = false

	vp := viewport.New(65, 20)
	vp.SetContent("Secure session established. Messages are E2E encrypted.\nTip: Type 'send <path>' or '$send <path>' to share images inline.\n")

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

	p := tea.NewProgram(m, tea.WithAltScreen(), tea.WithMouseCellMotion()) // <-- Add mouse support here

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
			} else if strings.HasPrefix(plaintext, "IMG:") {
				p.Send(imgReceipt(plaintext[4:]))
			}
		}
	}()

	finalModel, err := p.Run()
	if err != nil {
		return err
	}
	if fm, ok := finalModel.(model); ok {
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

	case tea.WindowSizeMsg:
		verticalMarginHeight := 5 // Space for textarea and typing indicator
		
		m.viewport.Width = msg.Width
		m.viewport.Height = msg.Height - verticalMarginHeight
		m.textarea.SetWidth(msg.Width)
		
		// Force the viewport to stay scrolled to the bottom on resize
		m.viewport.GotoBottom()
		return m, tea.Batch(tiCmd, vpCmd)
		
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC:
			m.conn.Close()
			return m, tea.Quit
		case tea.KeyEsc:
			return m, nil

		case tea.KeyEnter:
			raw := strings.TrimSpace(m.textarea.Value())
			if raw == "" {
				return m, nil
			}

			cmdText := strings.TrimPrefix(raw, "$")

			if strings.HasPrefix(cmdText, "send ") {
				rawPath := strings.TrimSpace(strings.TrimPrefix(cmdText, "send "))
				resolvedPath := resolvePath(rawPath)

				fileData, err := os.ReadFile(resolvedPath)
				if err != nil {
					errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
					m.messages = append(m.messages, errStyle.Render("Error reading file: ")+err.Error())
				} else {
					f, err := os.Open(resolvedPath)
					if err != nil {
						errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
						m.messages = append(m.messages, errStyle.Render("Error opening file: ")+err.Error())
					} else {
						_, _, decodeErr := image.DecodeConfig(f)
						f.Close()

						if decodeErr != nil {
							errStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("196")).Bold(true)
							m.messages = append(m.messages, errStyle.Render("Unsupported image format: ")+decodeErr.Error())
						} else {
							filename := filepath.Base(resolvedPath)
							b64Data := base64.StdEncoding.EncodeToString(fileData)
							payload := fmt.Sprintf("IMG:%s:%s", filename, b64Data)

							ciphertext, _ := auth.Encrypt(m.sessionKey, []byte(payload))
							encodedCipher := base64.StdEncoding.EncodeToString(ciphertext)
							m.conn.Write([]byte(encodedCipher + "\n"))

							preview := renderPixelArt(resolvedPath, 50)
							senderStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
							header := senderStyle.Render("You sent: ") + filename
							link := "📂 " + resolvedPath + " (Ctrl+Click path to open)"
							m.messages = append(m.messages, header+"\n"+preview+"\n"+link)
						}
					}
				}

				m.viewport.SetContent(strings.Join(m.messages, "\n"))
				m.textarea.Reset()
				m.viewport.GotoBottom()
				return m, tea.Batch(tiCmd, vpCmd)
			}

			style := lipgloss.NewStyle().Foreground(lipgloss.Color("205")).Bold(true)
			displayStr := style.Render("You: ") + raw
			m.messages = append(m.messages, displayStr)
			m.viewport.SetContent(strings.Join(m.messages, "\n"))
			m.textarea.Reset()
			m.viewport.GotoBottom()

			ciphertext, _ := auth.Encrypt(m.sessionKey, []byte("MSG:"+raw))
			encoded := base64.StdEncoding.EncodeToString(ciphertext)
			m.conn.Write([]byte(encoded + "\n"))

		default:
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

	case imgReceipt:
		m.peerTyping = false
		rawPayload := string(msg)
		parts := strings.SplitN(rawPayload, ":", 2)
		if len(parts) == 2 {
			filename := parts[0]
			b64Data := parts[1]
			imgBytes, err := base64.StdEncoding.DecodeString(b64Data)
			if err == nil {
				tempFile, err := os.CreateTemp("", "hey-*_"+filename)
				if err == nil {
					tempPath := tempFile.Name()
					tempFile.Write(imgBytes)
					tempFile.Close()

					pixelArt := renderPixelArt(tempPath, 50)
					link := "💾 " + tempPath + " (Ctrl+Click path to open)"

					peerStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("39")).Bold(true)
					header := peerStyle.Render(m.peerUsername+": sent an image (") + filename + ")"
					m.messages = append(m.messages, header+"\n"+pixelArt+"\n"+link)

					m.viewport.SetContent(strings.Join(m.messages, "\n"))
					m.viewport.GotoBottom()
				}
			}
		}

	case errMsg:
		m.err = msg
		return m, tea.Quit
	}

	return m, tea.Batch(tiCmd, vpCmd)
}

func (m model) View() string {
	typingIndicator := "\n"
	if m.peerTyping {
		typingIndicator = lipgloss.NewStyle().Foreground(lipgloss.Color("86")).Render(m.peerUsername + " is typing...\n")
	}

	return fmt.Sprintf(
		"%s\n%s\n%s",
		m.viewport.View(),
		typingIndicator,
		m.textarea.View(),
	)
}
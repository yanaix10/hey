# hey

Global encrypted terminal chat, inline image preview, and ephemeral voice notes application.

---

## Tech Stack & Utilities

- **Language:** Go (1.21+)
- **Terminal UI:** Charmbracelet Bubble Tea, Lip Gloss, Bubbles
- **Cryptography:** AES-256-GCM, SHA-256 Key Derivation, `crypto/rand`
- **Networking:** Direct TCP, TLS 1.3 Blind-Pipe Relay, Zeroconf / mDNS (`_heychat._tcp`)
- **Audio Processing:** 16kHz 16-bit mono PCM WAV, DC offset filtering, peak volume normalization
- **Audio Drivers:** PipeWire / PulseAudio / ALSA / FFmpeg (Linux), `afrecord`/`afplay` (macOS), WinMM / SoundPlayer (Windows)
- **Desktop & Shell:** D-Bus / Dunstify / Zenity (Linux), AppleScript (macOS), Win32 / PowerShell WPF (Windows), hooks for Bash, Zsh, Fish, PowerShell

---

## Features in Brief

- **Global Relay & Local P2P:** Auto-discovers peers on local Wi-Fi via mDNS, or routes through a cloud TLS relay across NATs and firewalls.
- **One-Time OTP Pairing:** First connection verifies with a 6-digit PIN; future chats connect instantly without prompts.
- **End-to-End Encryption:** AES-GCM encryption with fresh nonces per packet. The cloud relay acts as a zero-knowledge blind pipe.
- **10-Minute Ephemeral Storage:** All messages and audio recordings automatically self-destruct after 10 minutes.
- **Voice Notes & Recall:** Record up to 30s audio, boost volume, stream, playback, and remotely recall/delete audio on both ends.
- **Native OS Alerts:** Fires system desktop popups with Accept/Decline action buttons, auto-launching your favorite terminal emulator.
- **Shell Hooks:** Native `hey@<username>` terminal command trigger out of the box.

---

## System Architecture

```
+--------------------------------------------------------------------------+
|                               Client Host A                              |
|                                                                          |
|   +-----------------------+              +---------------------------+   |
|   |   hey (CLI Client)    |              |   heyd (Background Daemon)|   |
|   |  - Bubble Tea TUI     |              |  - Local Listener (:4242) |   |
|   |  - AES-GCM Encryption |              |  - Desktop Notifications  |   |
|   |  - Audio Engine (WAV) |              |  - Terminal Auto-Spawner  |   |
|   |  - Local Peers Cache  |              |  - 10-Min GC Pruning Loop |   |
|   +-----------+-----------+              +-------------+-------------+   |
|               |                                        |                 |
+---------------|----------------------------------------|-----------------+
                |                                        |
       Direct TCP / Session                              | Persistent TLS
       (or Bridged via Relay)                            | Connection
                |                                        |
                v                                        v
+--------------------------------------------------------------------------+
|                       Cloud Relay Service (hey-relay)                    |
|                                                                          |
|  - TLS 1.3 Blind Pipe Router (Port 443 / 4244)                           |
|  - In-Memory Daemon Registry: map[username]net.Conn                      |
|  - Session Matching: map[sessionID]pendingSession                        |
|  - Zero-Knowledge Bidirectional io.Copy Stream Bridging                  |
+--------------------------------------------------------------------------+
                ^                                        ^
                |                                        |
       Direct TCP / Session                              | Persistent TLS
       (or Bridged via Relay)                            | Connection
                |                                        |
+---------------|----------------------------------------|-----------------+
|               |                                        |                 |
|   +-----------+-----------+              +-------------+-------------+   |
|   |   hey (CLI Client)    |              |   heyd (Background Daemon)|   |
|   |  - Bubble Tea TUI     |              |  - Local Listener (:4242) |   |
|   |  - AES-GCM Encryption |              |  - Desktop Notifications  |   |
|   |  - Audio Engine (WAV) |              |  - Terminal Auto-Spawner  |   |
|   |  - Local Peers Cache  |              |  - 10-Min GC Pruning Loop |   |
|   +-----------------------+              +---------------------------+   |
|                               Client Host B                              |
+--------------------------------------------------------------------------+
```

### Connection Flow

1. **Discovery:** Host A checks local LAN via mDNS; falls back to cloud relay `REQUEST` if peer is remote.
2. **Prompt:** Host B daemon receives invitation and triggers native OS desktop popup.
3. **Handshake:** On accept, Host B terminal spawns; hosts verify 6-digit OTP (skipped if already paired) and derive AES-256 session keys.
4. **Transport:** Clients exchange AES-GCM encrypted base64 packets (text, audio, recall, typing) directly or through relay.

---

## Folder Structure

```
hey/
|-- .github/
|   \-- workflows/
|       \-- build.yml             # GitHub Actions CI workflow for Linux and Windows builds
|-- cmd/
|   |-- hey/
|   |   \-- main.go               # Main CLI client: command parsing, handshake, TUI launch
|   |-- hey-relay/
|   |   \-- main.go               # Standalone TLS cloud relay server for NAT traversal
|   |-- heyd/
|   |   \-- main.go               # Background daemon: mDNS/relay listener, popups, 10-min GC
|   \-- testchat/
|       \-- main.go               # Relay testing harness simulating multi-client communication
|-- internal/
|   |-- auth/
|   |   \-- auth.go               # Cryptographic primitives: PIN generation, SHA-256, AES-GCM
|   |-- chat/                     # Package directory reserved for chat protocol extensions
|   |-- discovery/
|   |   \-- mdns.go               # Local mDNS discovery and cloud relay dialer
|   |-- history/
|   |   \-- history.go            # Ephemeral chat storage and 10-minute TTL garbage collector
|   |-- notify/
|   |   |-- notify_darwin.go      # macOS native dialogs and Terminal.app activation
|   |   |-- notify_linux.go       # Linux D-Bus notifications and terminal emulator launcher
|   |   \-- notify_windows.go     # Windows Win32 / PowerShell message box and terminal runner
|   \-- ui/
|       |-- audio.go              # Cross-platform microphone capture, WAV normalizer, playback
|       \-- chat.go               # Bubble Tea TUI, message viewport, and packet encryption
|-- .gitignore                    # Git file ignore rules
|-- Dockerfile                    # Multi-stage Docker build for deploying hey-relay
|-- go.mod                        # Go module definition and direct/indirect dependencies
|-- go.sum                        # Cryptographic checksums of Go module dependencies
|-- image.png                     # Visual demonstration asset
|-- install.ps1                   # Windows PowerShell installer, registry startup, shell hook
|-- install.sh                    # Linux and macOS installer, systemd/launchd service, shell hooks
\-- README.md                     # Project documentation, guides, and architectural reference
```

---

## Installation

### Linux & macOS
```bash
git clone https://github.com/yanaix10/hey.git
cd hey
chmod +x install.sh
./install.sh
```
Reload shell: `source ~/.bashrc` (or `~/.zshrc`, `~/.config/fish/config.fish`).

### Windows
```powershell
git clone https://github.com/yanaix10/hey.git
cd hey
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force
.\install.ps1
```
Restart PowerShell after installation.

---

## Core Usage

- **First Connect:** Run `hey@<username>`. Peer receives OS desktop notification and clicks Accept. Caller reads 6-digit OTP to peer to pair.
- **Future Connect:** Run `hey@<username>`. Peer clicks Accept; chat opens instantly (no OTP needed).

---

## Commands

### In-Chat Commands

| Command | Action |
| :--- | :--- |
| `Type message & Enter` | Send encrypted text message |
| `send <path/to/image.jpg>` | Send inline pixel-art image preview and file |
| `/voice` | Record voice note (up to 30s; Enter to send, Esc to cancel) |
| `/play` or `/play <number>` | Play latest or specific voice note |
| `/delete` or `/delete <number>` | Recall/delete voice note from both endpoints |

### CLI Commands

| Command | Description |
| :--- | :--- |
| `hey <username>` or `hey@<username>` | Start or request a chat |
| `hey join <target>` | Manually connect to an incoming session |
| `hey peers` | List all saved trusted peers |
| `hey unpair <username>` | Remove a peer (forces OTP on next connection) |
| `hey clear` | Wipe all saved peers and keys |

---

## Updating

- **Linux / macOS:** `git pull && ./install.sh`
- **Windows:** `git pull; .\install.ps1`
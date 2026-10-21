# hey : Global Encrypted Terminal Chat & Voice Notes App

`hey` is a lightweight, cross-platform terminal chat application written in Go. It allows you to connect securely with anyone globally using a one-time 6-digit OTP, send pixelated inline images, and record/playback ephemeral voice notes.

---

## Features

- **Global Relay Routing:** Connects peers across different networks and NATs via a lightweight TCP relay—no local firewall rules or port forwarding required.
- **One-Time OTP Pairing:** Verify a new peer once using a 6-digit code. Paired credentials are saved locally (`~/.config/hey/peers.json` on Linux/macOS, `%APPDATA%\hey\peers.json` on Windows) so future chats connect instantly.
- **End-to-End Encryption:** Uses AES-GCM encryption derived from a secure cryptographic handshake; the relay server only blind-pipes encrypted bytes and cannot read your messages.
- **Ephemeral 10-Minute Self-Destruct:** All text messages, images, and voice notes automatically self-destruct from local storage after 10 minutes.
- **Voice Notes & Instant Recall:** Record up to 30-second voice notes with `/voice`, playback using `/play`, and instantly delete/recall them from both screens using `/delete`.
- **Native Desktop Pop-ups:** Triggers OS-native notifications on Linux (D-Bus), Windows, and macOS, bringing you straight into the chat when clicked.
- **Automatic Shell Hooks:** Intercepts `hey@<username>` directly out of the box in Fish, Bash, Zsh, and PowerShell.

---

## System Architecture

hey is designed as a decentralized, privacy-focused peer-to-peer communication system that operates across both local area networks (LAN) and the global internet. The architecture consists of three core executable components and five internal domain packages.

### Architectural Diagram

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

### Core Components

1. **CLI Client (`cmd/hey`)**:
   - Handles user interaction and command processing (`hey <username>`, `hey join`, `hey peers`, `hey unpair`, `hey clear`).
   - Automatically checks if the background daemon (`heyd`) is running on port 4242 and starts it if necessary.
   - Evaluates connectivity path: queries local mDNS resolution first; if the peer is not discovered on the local network, dials the cloud relay.
   - Manages cryptographic handshakes: generates 6-digit numeric PINs, exchanges salts, verifies challenge digests, and derives shared session keys.
   - Stores trusted peer pairings in local storage (`peers.json`) to skip manual PIN entry for repeat conversations.
   - Runs the interactive Bubble Tea terminal user interface with scrolling viewports, typing indicators, and voice note controls.

2. **Background Daemon (`cmd/heyd`)**:
   - Runs persistently in the user background session (managed via systemd user services on Linux, launchd on macOS, or Windows Registry run keys).
   - Listens on TCP port 4242 for direct peer discovery invitations on the local network.
   - Maintains a long-lived TLS connection to the cloud relay (`hey-relay`) using `LISTEN <username>` with automated reconnection logic.
   - Triggers native desktop confirmation dialogs using platform-specific mechanisms (Dunstify, Zenity, KDialog, or D-Bus on Linux; AppleScript on macOS; Win32 / PowerShell WPF message boxes on Windows).
   - Spawns terminal emulator windows on connection acceptance, supporting Kitty, Alacritty, Foot, Wezterm, GNOME Terminal, Konsole, XFCE4 Terminal, Xterm, Apple Terminal, and PowerShell.
   - Runs a periodic background garbage collection worker (every 60 seconds) to purge messages and voice recordings older than 10 minutes.

3. **Cloud Relay Server (`cmd/hey-relay`)**:
   - Standalone TLS server deployed to facilitate global connectivity across restrictive NATs and firewalls.
   - Maintains an in-memory registry of online daemons and active pending sessions.
   - Coordinates session establishment using simple wire commands (`REGISTER`, `REQUEST`, `PING`, `DECISION`, `BRIDGE`).
   - Acts as a zero-knowledge blind pipe: relays raw encrypted bytes bidirectionally between clients without access to encryption keys or message content.

4. **Internal Subsystems**:
   - `internal/auth`: Cryptographic security primitives. Implements cryptographically secure 6-digit PIN generation via `crypto/rand`, key derivation via SHA-256 with salt, and AES-GCM authenticated encryption and decryption.
   - `internal/discovery`: Network discovery handlers. Handles local zero-configuration service advertisement and resolution via mDNS/DNS-SD (`_heychat._tcp`), as well as secure TLS dialing to the cloud relay.
   - `internal/notify`: Platform-specific desktop notification engines and terminal emulator launchers tailored for Linux, macOS, and Windows.
   - `internal/history`: Ephemeral file-based persistence layer stored under the user config directory (`~/.config/hey/history` or `%APPDATA%\hey\history`). Enforces a strict 10-minute Time-To-Live (TTL) on all chat transcripts and audio files.
   - `internal/ui`: Full-screen terminal user interface powered by Bubble Tea and Lip Gloss. Integrates a custom cross-platform audio engine supporting native recording (`pw-record`, `parecord`, `arecord`, `ffmpeg`, `afrecord`, or `winmm.dll`), WAV normalization, DC offset filtering, peak volume boosting, synchronized playback, and message/audio recall.

### Protocol and Connection Lifecycle

1. **Discovery and Signaling**:
   - Host A initiates a session to Host B.
   - Host A queries mDNS for Host B on the local subnet.
   - If Host B is found locally, Host A connects to Host B's daemon on TCP port 4242 and proposes a chat port (SessionPort 4243).
   - If Host B is not on the local network, Host A sends `REQUEST <target> <sender>` to the cloud relay. The relay forwards a `PING <sessionID> <sender>` over the persistent TLS connection to Host B's daemon.

2. **User Prompting and Acceptance**:
   - Host B's daemon detects the incoming request and triggers an OS-native desktop dialog.
   - If Host B accepts, the daemon signals acceptance and spawns a new terminal window running `hey join <address>` (or `hey join relay:<sessionID>`).
   - If Host B rejects or the request times out, Host A receives a decline status and closes the attempt.

3. **Cryptographic Handshake and Authentication**:
   - Once connected (either directly or bridged through the relay), Host A generates a 16-byte cryptographically secure random salt and sends it to Host B.
   - If the peers are paired, the stored shared secret PIN is used; otherwise, Host A displays a generated 6-digit PIN which Host B inputs manually.
   - Host B computes `SHA-256(PIN + salt)` and sends the 32-byte digest back to Host A.
   - Host A verifies the digest against its own computed value. If mismatched, the connection immediately terminates.
   - Both hosts derive the 32-byte session key using `SHA-256(PIN + salt)`.
   - Both hosts exchange usernames and save the peer to their local trusted peer cache (`peers.json`).

4. **Encrypted Data Transfer**:
   - All text messages (`TEXT|<content>`), typing indicators (`TYPING`), voice notes (`VOICE|<id>|<base64-wav>`), and deletion notices (`RECALL|<id>`) are encrypted using AES-GCM with a fresh random nonce per packet.
   - Payloads are base64-encoded and transmitted line-delimited over the TCP socket.
   - The relay server blind-pipes the raw bytes between the two endpoints, ensuring complete end-to-end encryption.

---

## Folder Structure

The repository follows standard Go project layout conventions, separating CLI entry points, daemon commands, internal domain packages, and deployment configurations:

```
hey/
├── .github/
│   └── workflows/
│       └── build.yml             # GitHub Actions CI workflow for Linux and Windows builds
├── cmd/
│   ├── hey/
│   │   └── main.go               # Main CLI client: command parsing, handshake, TUI launch
│   ├── hey-relay/
│   │   └── main.go               # Standalone TLS cloud relay server for NAT traversal
│   ├── heyd/
│   │   └── main.go               # Background daemon: mDNS/relay listener, popups, 10-min GC
│   └── testchat/
│       └── main.go               # Relay testing harness simulating multi-client communication
├── internal/
│   ├── auth/
│   │   └── auth.go               # Cryptographic primitives: PIN generation, SHA-256, AES-GCM
│   ├── chat/                     # Package directory reserved for chat protocol extensions
│   ├── discovery/
│   │   └── mdns.go               # Local mDNS discovery and cloud relay dialer
│   ├── history/
│   │   └── history.go            # Ephemeral chat storage and 10-minute TTL garbage collector
│   ├── notify/
│   │   ├── notify_darwin.go      # macOS native dialogs and Terminal.app activation
│   │   ├── notify_linux.go       # Linux D-Bus notifications and terminal emulator launcher
│   │   └── notify_windows.go     # Windows Win32 / PowerShell message box and terminal runner
│   └── ui/
│       ├── audio.go              # Cross-platform microphone capture, WAV normalizer, playback
│       └── chat.go               # Bubble Tea TUI, message viewport, and packet encryption
├── .gitignore                    # Git file ignore rules
├── Dockerfile                    # Multi-stage Docker build for deploying hey-relay
├── go.mod                        # Go module definition and direct/indirect dependencies
├── go.sum                        # Cryptographic checksums of Go module dependencies
├── image.png                     # Visual demonstration asset
├── install.ps1                   # Windows PowerShell installer, registry startup, shell hook
├── install.sh                    # Linux and macOS installer, systemd/launchd service, shell hooks
└── README.md                     # Project documentation, guides, and architectural reference
```

### Directory and Component Details

- `cmd/hey`: Contains the main command-line binary. Coordinates user commands (`hey <user>`, `hey join`, `hey peers`, `hey unpair`, `hey clear`), local peer caching in `peers.json`, connection dispatch, and terminal UI initialization.
- `cmd/heyd`: Contains the daemon binary designed to run continuously in the background. Listens on TCP port 4242 for local connections, maintains a persistent TLS connection to `hey-relay`, invokes platform-specific dialogs, spawns terminal sessions, and runs the 10-minute history garbage collection routine.
- `cmd/hey-relay`: Contains the cloud relay server binary. Can be deployed on any server or container. Accepts incoming TLS connections, matches caller and callee sessions, and pipes encrypted streams bidirectionally.
- `cmd/testchat`: Contains an automated test script used to verify relay registration, session bridging, and message flow between simulated endpoints.
- `internal/auth`: Implements security operations including cryptographically random 6-digit numeric PIN generation, key derivation using SHA-256 with salt, and AES-GCM encryption/decryption with randomized nonces.
- `internal/discovery`: Encapsulates peer discovery mechanisms. Uses Zeroconf / mDNS (`_heychat._tcp`) to detect peers on the same local network and TLS client connections to reach the global relay server.
- `internal/history`: Manages local ephemeral chat history stored in JSON format under the user's config directory. Implements automatic pruning for messages older than 10 minutes.
- `internal/notify`: Provides OS-specific abstractions for desktop notifications and terminal spawning across Linux, macOS, and Windows.
- `internal/ui`: Houses the terminal user interface and the cross-platform audio recording/playback engine. Handles keyboard events, viewport scrolling, audio device detection, WAV sample rate conversion, peak boosting, and playback execution.
- Root Scripts and Configs:
  - `install.sh`: Automated setup script for Linux and macOS that compiles binaries, installs user-level daemon services (systemd / launchd), and configures shell integration for Bash, Zsh, and Fish.
  - `install.ps1`: Automated setup script for Windows that builds binaries, registers startup tasks in the registry, and adds command-not-found hooks to PowerShell.
  - `Dockerfile`: Multi-stage Alpine-based container specification for building and running `hey-relay`.

---

## Prerequisites

- **Go 1.21+** installed (`go version`)
- **Git** installed

---

## Out-of-the-Box Installation Guides

### 1. Linux & macOS (Fish / Zsh / Bash)
Open your terminal and run:
```bash
git clone https://github.com/yanaix10/hey.git
cd hey
chmod +x install.sh
./install.sh
```
*(This builds the binaries, moves them to `~/.local/bin`, installs `heyd` as a background user service, and hooks the `hey@username` command into your shell)*.

Reload your shell config:
- **Fish:** `source ~/.config/fish/config.fish`
- **Zsh:** `source ~/.zshrc`
- **Bash:** `source ~/.bashrc`

### 2. Windows (PowerShell)
Open PowerShell as your user and run:
```powershell
git clone https://github.com/yanaix10/hey.git
cd hey
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force
.\install.ps1
```
*(This compiles `hey.exe` and `heyd.exe`, registers the background daemon to run on user login, and injects the `hey@username` hook into your PowerShell profile)*.

Restart your PowerShell window after installation.

---

## Usage Guide: How Two Users Connect

### 1. First-Time Connection (One-Time OTP)
When chatting with someone for the very first time:
1. **Initiate:** Type `hey@<friend_username>` in your terminal.
2. **Accept Pop-up:** Your friend receives an OS-native desktop notification and clicks **Accept**.
3. **Enter OTP:** Your terminal displays a **6-digit OTP**. Tell your friend this code (over Discord, WhatsApp, etc.), and they enter it into their terminal window.
4. **Paired:** The chat unlocks, and both computers permanently trust each other. **You will never need an OTP for this friend again**.

### 2. Future Connections (Instant Pop-up & Chat)
Once paired, starting a chat is instantaneous:
1. Type `hey@<friend_username>` in your terminal.
2. Your friend clicks **Accept** on their incoming desktop notification.
3. Their terminal opens straight into the encrypted chat session.

---

## Chat Commands & Media Guide

Once you are inside the encrypted terminal chat UI, you can use these commands:

| Command | Action |
| :--- | :--- |
| `Type message & Enter` | Send a standard end-to-end encrypted text message |
| `send <path/to/image.jpg>` | Render a high-resolution pixel art preview inline and share the file |
| `/voice` | Start recording a voice note (max 30 seconds; Press **Enter** to send, **Esc** to cancel) |
| `/play` or `/play <number>` | Replay the latest or a specific numbered voice note (e.g., `/play 1`) |
| `/delete` or `/delete <number>` | Instantly wipe/recall the latest or a specific voice note from both screens |

---

## Utility Commands

| Command | Description |
| :--- | :--- |
| `hey peers` | List all trusted/paired friends saved on your machine |
| `hey unpair <username>` | Remove a saved friend and require a new OTP verification next time |
| `hey clear` | Wipe all saved peer data |

---

## Updating

To pull the latest updates and rebuild:
- **Linux / macOS:** `git pull && ./install.sh`
- **Windows:** `git pull; .\install.ps1`
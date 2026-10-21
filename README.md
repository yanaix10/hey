# 💬 hey : Global Encrypted Terminal Chat & Voice Notes App

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

##  Prerequisites

- **Go 1.21+** installed (`go version`)
- **Git** installed

---

##  Out-of-the-Box Installation Guides

### 1. Linux & macOS (Fish / Zsh / Bash)
Open your terminal and run:
```bash
git clone [https://github.com/yanaix10/hey.git](https://github.com/yanaix10/hey.git)
cd hey
chmod +x install.sh
./install.sh
```
*(This builds the binaries, moves them to `~/.local/bin`, installs `heyd` as a background user service, and hooks the `hey@username` command into your shell)*[cite: 3].

Reload your shell config[cite: 3]:
- **Fish:** `source ~/.config/fish/config.fish`[cite: 3]
- **Zsh:** `source ~/.zshrc`[cite: 3]
- **Bash:** `source ~/.bashrc`[cite: 3]

### 2. Windows (PowerShell)
Open PowerShell as your user and run[cite: 3]:
```powershell
git clone [https://github.com/yanaix10/hey.git](https://github.com/yanaix10/hey.git)
cd hey
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force
.\install.ps1
```
*(This compiles `hey.exe` and `heyd.exe`, registers the background daemon to run on user login, and injects the `hey@username` hook into your PowerShell profile)*[cite: 4].

Restart your PowerShell window after installation[cite: 4].

---

## Usage Guide: How Two Users Connect

### 1. First-Time Connection (One-Time OTP)
When chatting with someone for the very first time[cite: 4]:
1. **Initiate:** Type `hey@<friend_username>` in your terminal[cite: 4].
2. **Accept Pop-up:** Your friend receives an OS-native desktop notification and clicks **Accept**[cite: 4].
3. **Enter OTP:** Your terminal displays a **6-digit OTP**. Tell your friend this code (over Discord, WhatsApp, etc.), and they enter it into their terminal window[cite: 4].
4. **Paired:** The chat unlocks, and both computers permanently trust each other. **You will never need an OTP for this friend again**[cite: 4].

### 2. Future Connections (Instant Pop-up & Chat)
Once paired, starting a chat is instantaneous[cite: 4]:
1. Type `hey@<friend_username>` in your terminal[cite: 4].
2. Your friend clicks **Accept** on their incoming desktop notification[cite: 4].
3. Their terminal opens straight into the encrypted chat session[cite: 4].

---

##  Chat Commands & Media Guide

Once you are inside the encrypted terminal chat UI, you can use these commands[cite: 5]:

| Command | Action |
| :--- | :--- |
| `Type message & Enter` | Send a standard end-to-end encrypted text message[cite: 5] |
| `send <path/to/image.jpg>` | Render a high-resolution pixel art preview inline and share the file[cite: 5] |
| `/voice` | Start recording a voice note (max 30 seconds; Press **Enter** to send, **Esc** to cancel)[cite: 5] |
| `/play` or `/play <number>` | Replay the latest or a specific numbered voice note (e.g., `/play 1`)[cite: 5] |
| `/delete` or `/delete <number>` | Instantly wipe/recall the latest or a specific voice note from both screens[cite: 5] |

---

##  Utility Commands

| Command | Description |
| :--- | :--- |
| `hey peers` | List all trusted/paired friends saved on your machine[cite: 5, 6] |
| `hey unpair <username>` | Remove a saved friend and require a new OTP verification next time[cite: 5, 6] |
| `hey clear` | Wipe all saved peer data[cite: 5, 6] |

---

##  Updating

To pull the latest updates and rebuild:
- **Linux / macOS:** `git pull && ./install.sh`[cite: 6]
- **Windows:** `git pull; .\install.ps1`[cite: 6]
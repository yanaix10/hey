# hey — Global Encrypted Terminal Chat

A cross-platform, end-to-end encrypted terminal chat utility written in Go. Type `hey@username` directly in your shell to send an instant desktop pop-up to any peer globally, pair once with a 6-digit one-time OTP, and chat in an encrypted TUI—clicking the pop-up on future connections drops you straight into the chat.

## Architecture & Features

- **Global Relay Routing (Zero Firewall Setup):** Connects peers across different networks and NATs via a blind TCP relay—no port forwarding, LAN requirement, or local firewall rules needed.
- **One-Time OTP Pairing:** Verify a new peer once using a 6-digit OTP. Paired credentials and keys are stored locally (`~/.config/hey/peers.json` on Linux/macOS, `%APPDATA%\hey\peers.json` on Windows) so future chats connect immediately upon clicking the pop-up.
- **End-to-End Encryption:** Uses a 16-byte random salt challenge-response handshake to derive a 256-bit **AES-GCM** session key; the relay server only pipes encrypted bytes and cannot read messages.
- **Native OS Alerts:** Triggers desktop notifications via D-Bus on Linux, Win32 modal dialogs on Windows, and AppleScript dialogs on macOS.
- **Interactive TUI:** Built with Bubble Tea, featuring live typing indicators and auto-scrolling viewports.
- **Automatic Shell Hook Integration:** Intercepts `hey@<username>` directly in Fish, Bash, Zsh, and PowerShell out of the box.

---

## Prerequisites

- **Go 1.21+** installed (`go version`)
- **Git**

---

## Out-of-the-Box Setup Guides

### 1. Linux & macOS (Fish / Zsh / Bash)

The installer compiles `hey` and `heyd`, registers `heyd` as a background user service (`systemd` on Linux, `launchd` on macOS), and automatically adds the `hey@username` hook to your shell config.

```bash
git clone [https://github.com/yanaix10/hey.git](https://github.com/yanaix10/hey.git)
cd hey
chmod +x install.sh
./install.sh
```

Reload your shell (or open a new terminal tab) after installation:
```bash
# Fish
source ~/.config/fish/config.fish

# Zsh
source ~/.zshrc

# Bash
source ~/.bashrc
```

---

### 2. Windows (PowerShell)

Open PowerShell and run:

```powershell
git clone [https://github.com/yanaix10/hey.git](https://github.com/yanaix10/hey.git)
cd hey
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned -Force
.\install.ps1
```

*(This compiles `hey.exe` and `heyd.exe`, installs them to `~\.local\bin`, starts the `heyd` background daemon automatically on login, and registers the `hey@username` hook in your PowerShell `$PROFILE`).*

Restart PowerShell after the script finishes.

---

## Usage: How Two Users Connect

### 1. First-Time Connection (One-Time OTP)
When connecting with someone for the first time:

1. **Initiate:** Type `hey@<peer_username>` in your terminal.
2. **Pop-up Alert:** Your peer receives a native desktop pop-up (`Accept` / `Decline`).
3. **One-Time OTP Verification:**
   - Clicking **Accept** opens their terminal and prompts for a **6-digit OTP**.
   - Share the 6-digit OTP displayed on your screen with your peer.
   - Once entered, the encrypted session starts and both devices permanently save the pairing locally.

### 2. Future Connections (Instant Pop-up to Chat)
Once paired with a user, **you never need an OTP again**:

1. Type `hey@<peer_username>` in your terminal.
2. Your peer clicks **Accept** on the incoming desktop pop-up.
3. Their terminal opens directly into the live encrypted chat session.

### 3. Exit
Press `Esc` or `Ctrl+C` at any time to close the chat and wipe transient session keys from memory.

---

## Managing Peers & Commands

| Command | Description |
| :--- | :--- |
| `hey@<username>` | Ring a peer (prompts for one-time OTP on first chat, instant thereafter) |
| `hey peers` | List all trusted/paired peers saved on your machine |
| `hey unpair <username>` | Remove a saved peer and require OTP verification again |
| `hey whoami` | Display your registered username on the relay |

---

## Updating

Pull the latest changes and re-run the installer for your OS:

- **Linux / macOS:** `git pull && ./install.sh`
- **Windows:** `git pull; .\install.ps1`
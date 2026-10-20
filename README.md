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

## 📖 How to Connect (The Real-World Flow)

Because `hey` is a private, blind-relay chat, there is no public user directory. You connect with friends the same way you share a phone number.

### 1. The First-Time Pairing (One-Time OTP)
If you want to connect with a friend you are talking to on Discord, Slack, or WhatsApp:

1. **Alice** runs `./hey host` in her terminal. The server gives her a one-time OTP: `849201`.
2. **Alice** messages Bob on Discord: *"My terminal chat OTP is 849201."*
3. **Bob** runs `./hey pair 849201` in his terminal.

### 2. Automatic Trust & Username Exchange
The moment Bob enters the OTP, the encrypted bridge connects them. In the background, the app automatically exchanges their local system usernames and saves them to `peers.json`. **You never need an OTP for this friend again.**

### 3. Future Chats (Zero OTP & Instant Pop-ups)
The next day, Bob wants to message Alice again. 

* If Bob forgets Alice's username, he types `hey peers` to see his trusted friends list.
* Bob types `hey@alice` directly in his terminal.
* Alice immediately gets a native OS desktop notification. She clicks **Accept**, and her terminal opens straight into the chat!

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
# hey — P2P Encrypted Terminal Chat

A peer-to-peer, serverless LAN terminal chat utility written in Go. Type `hey@username` directly in your shell to send an instant desktop pop-up to a peer on the same Wi-Fi, verify identity with a 6-digit one-time PIN, and chat in an end-to-end encrypted TUI.

## Architecture & Features

* **Zero Central Server (mDNS):** Automatically discovers peers on the local network over UDP port `5353` using Zeroconf (`_heychat._tcp`).
* **Native OS Alerts:** Triggers desktop notifications via D-Bus on Linux, Win32 modal dialogs on Windows, and AppleScript dialogs on macOS.
* **PIN Handshake & E2E Encryption:** Uses a 6-digit one-time PIN with a 16-byte random salt challenge-response to derive a 256-bit **AES-GCM** session key.
* **Interactive TUI:** Built with Bubble Tea, featuring live typing indicators and auto-scrolling viewports.
* **Shell Hook Integration:** Intercepts `hey@<username>` directly from Fish, Bash, Zsh, or PowerShell.

---

## Prerequisites

* **Go 1.21+** installed (`go version`)
* Both devices connected to the **same local network (Wi-Fi/LAN)**

---

## Setup Guides

### 1. Linux (Arch / Ubuntu / Fedora)

**Requirements:** `avahi-daemon` running for mDNS discovery and a notification daemon (`dunst`, `mako`, GNOME/KDE notifications).

```bash
# Arch Linux: Ensure Avahi is active
sudo systemctl enable --now avahi-daemon.service
```

**Install Binaries & Background Service:**
```bash
git clone [https://github.com/](https://github.com/)<your-username>/hey.git
cd hey
chmod +x install.sh
./install.sh
```

**Enable `hey@username` Shell Syntax:**

* **Fish (`~/.config/fish/config.fish`):**
  ```fish
  function __fish_command_not_found_handler --on-event fish_command_not_found
      if string match -r '^hey@(.+)$' -- $argv[1]
          set target (string replace -r '^hey@' '' -- $argv[1])
          hey $target
      else
          __fish_default_command_not_found_handler $argv
      end
  end
  ```
* **Bash (`~/.bashrc`):**
  ```bash
  command_not_found_handle() {
      if [[ "$1" =~ ^hey@(.+)$ ]]; then
          hey "${BASH_REMATCH[1]}"
      else
          printf "%s: command not found\n" "$1" >&2
          return 127
      fi
  }
  ```
* **Zsh (`~/.zshrc`):**
  ```zsh
  command_not_found_handler() {
      if [[ "$1" =~ ^hey@(.+)$ ]]; then
          hey "${match[1]}"
      else
          printf "zsh: command not found: %s\n" "$1" >&2
          return 127
      fi
  }
  ```

**Firewall (If `ufw` or `firewalld` is active):**
```bash
sudo ufw allow 4242,4243/tcp
sudo ufw allow 5353/udp
```

---

### 2. Windows (PowerShell)

**1. Clone and Run Installer:**
Open PowerShell and run:
```powershell
git clone [https://github.com/](https://github.com/)<your-username>/hey.git
cd hey
Set-ExecutionPolicy -Scope CurrentUser -ExecutionPolicy RemoteSigned
.\install.ps1
```
*(This compiles `hey.exe` and `heyd.exe`, installs them to `~\.local\bin`, starts the background daemon on boot, and registers the `hey@username` hook in your PowerShell profile).*

**2. Allow Ports in Windows Firewall (Run once as Administrator):**
```powershell
New-NetFirewallRule -DisplayName "HeyChat" -Direction Inbound -Protocol TCP -LocalPort 4242,4243 -Action Allow
New-NetFirewallRule -DisplayName "HeyChat-mDNS" -Direction Inbound -Protocol UDP -LocalPort 5353 -Action Allow
```

**3. Restart PowerShell** to load the updated profile.

---

### 3. macOS (Zsh / Bash)

**1. Build and Install Binaries:**
```bash
git clone [https://github.com/](https://github.com/)<your-username>/hey.git
cd hey
go build -o hey ./cmd/hey
go build -o heyd ./cmd/heyd
mkdir -p ~/.local/bin
mv hey heyd ~/.local/bin/
export PATH="$HOME/.local/bin:$PATH"
```

**2. Start the Background Daemon:**
```bash
nohup ~/.local/bin/heyd >/dev/null 2>&1 &
```

**3. Enable `hey@username` in Zsh (`~/.zshrc`):**
```zsh
export PATH="$HOME/.local/bin:$PATH"

command_not_found_handler() {
    if [[ "$1" =~ ^hey@(.+)$ ]]; then
        hey "${match[1]}"
    else
        printf "zsh: command not found: %s\n" "$1" >&2
        return 127
    fi
}
```
Reload your shell with `source ~/.zshrc`.

---

## Usage

1. **Find your peer's username:** The daemon broadcasts the OS username (`$USER` on Linux/macOS, `$env:USERNAME` on Windows).
2. **Initiate a chat:**
   ```bash
   hey@<username>
   ```
3. **Accept & Verify:**
   * The receiver clicks **Accept** on the incoming pop-up, which automatically spawns a new terminal window.
   * The sender shares the displayed **6-digit PIN** with the receiver.
   * The receiver enters the PIN to verify the handshake and unlock the encrypted chat session.
4. **Exit:** Press `Esc` or `Ctrl+C` at any time to terminate the session and wipe keys from memory.

---

## Updating

When new changes are pushed to the repository, pull and re-run the installer:

* **Linux:** `git pull && ./install.sh`
* **Windows:** `git pull; .\install.ps1`
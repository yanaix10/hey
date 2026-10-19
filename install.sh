#!/usr/bin/env bash
set -e

BIN_DIR="$HOME/.local/bin"
CONFIG_DIR="$HOME/.config/hey"
mkdir -p "$BIN_DIR" "$CONFIG_DIR"

echo "[1/4] Building hey and heyd..."
go build -o "$BIN_DIR/hey" ./cmd/hey
go build -o "$BIN_DIR/heyd" ./cmd/heyd

echo "[2/4] Configuring background daemon (heyd)..."
OS="$(uname -s)"
if [ "$OS" = "Linux" ]; then
    mkdir -p "$HOME/.config/systemd/user"
    cat <<EOF > "$HOME/.config/systemd/user/heyd.service"
[Unit]
Description=Hey Chat Background Daemon
After=network-online.target

[Service]
ExecStart=$BIN_DIR/heyd
Restart=always
RestartSec=3

[Install]
WantedBy=default.target
EOF
    systemctl --user daemon-reload
    systemctl --user enable --now heyd.service
elif [ "$OS" = "Darwin" ]; then
    PLIST="$HOME/Library/LaunchAgents/com.yanaix10.heyd.plist"
    mkdir -p "$HOME/Library/LaunchAgents"
    cat <<EOF > "$PLIST"
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key><string>com.yanaix10.heyd</string>
    <key>ProgramArguments</key><array><string>$BIN_DIR/heyd</string></array>
    <key>RunAtLoad</key><true/>
    <key>KeepAlive</key><true/>
</dict>
</plist>
EOF
    launchctl unload "$PLIST" 2>/dev/null || true
    launchctl load "$PLIST"
fi

echo "[3/4] Installing hey@username shell hooks..."

# Fish Hook
if [ -d "$HOME/.config/fish" ] || command -v fish >/dev/null 2>&1; then
    mkdir -p "$HOME/.config/fish"
    FISH_RC="$HOME/.config/fish/config.fish"
    if ! grep -q "hey@" "$FISH_RC" 2>/dev/null; then
        cat << 'EOF' >> "$FISH_RC"

# >>> hey chat hook >>>
fish_add_path ~/.local/bin
function __fish_command_not_found_handler --on-event fish_command_not_found
    if string match -r '^hey@(.+)$' -- $argv[1]
        set target (string replace -r '^hey@' '' -- $argv[1])
        hey $target
    else
        __fish_default_command_not_found_handler $argv
    end
end
# <<< hey chat hook <<<
EOF
    fi
fi

# Bash Hook
if [ -f "$HOME/.bashrc" ]; then
    if ! grep -q "hey@" "$HOME/.bashrc" 2>/dev/null; then
        cat << 'EOF' >> "$HOME/.bashrc"

# >>> hey chat hook >>>
export PATH="$HOME/.local/bin:$PATH"
command_not_found_handle() {
    if [[ "$1" =~ ^hey@(.+)$ ]]; then
        hey "${BASH_REMATCH[1]}"
    else
        printf "%s: command not found\n" "$1" >&2
        return 127
    fi
}
# <<< hey chat hook <<<
EOF
    fi
fi

# Zsh Hook
if [ -f "$HOME/.zshrc" ]; then
    if ! grep -q "hey@" "$HOME/.zshrc" 2>/dev/null; then
        cat << 'EOF' >> "$HOME/.zshrc"

# >>> hey chat hook >>>
export PATH="$HOME/.local/bin:$PATH"
command_not_found_handler() {
    if [[ "$1" =~ ^hey@(.+)$ ]]; then
        hey "${match[1]}"
    else
        printf "zsh: command not found: %s\n" "$1" >&2
        return 127
    fi
}
# <<< hey chat hook <<<
EOF
    fi
fi

echo "[4/4] Done! Restart your terminal and type: hey@<username>"
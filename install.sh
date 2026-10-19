#!/bin/bash

echo "Building binaries..."
go build -o hey ./cmd/hey
go build -o heyd ./cmd/heyd

echo "Installing to ~/.local/bin..."
mkdir -p ~/.local/bin
mv hey heyd ~/.local/bin/

# Detect OS
OS="$(uname -s)"

if [ "$OS" = "Linux" ]; then
    echo "Creating systemd user service for Linux..."
    mkdir -p ~/.config/systemd/user
    cat <<EOF > ~/.config/systemd/user/heyd.service
[Unit]
Description=Hey P2P Chat Daemon
After=network.target

[Service]
ExecStart=%h/.local/bin/heyd
Restart=always
RestartSec=3
Environment="PATH=%h/.local/bin:/usr/local/bin:/usr/bin"

[Install]
WantedBy=default.target
EOF

    systemctl --user daemon-reload
    systemctl --user enable --now heyd.service
    systemctl --user restart heyd.service

elif [ "$OS" = "Darwin" ]; then
    echo "Creating launchd plist for macOS..."
    mkdir -p ~/Library/LaunchAgents
    cat <<EOF > ~/Library/LaunchAgents/com.hey.heyd.plist
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.hey.heyd</string>
    <key>ProgramArguments</key>
    <array>
        <string>$HOME/.local/bin/heyd</string>
    </array>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
EOF

    launchctl unload ~/Library/LaunchAgents/com.hey.heyd.plist 2>/dev/null
    launchctl load ~/Library/LaunchAgents/com.hey.heyd.plist
fi

# 1. Configure Zsh if ~/.zshrc exists (Default for macOS)
if [ -f "$HOME/.zshrc" ] && ! grep -q "command_not_found_handler" "$HOME/.zshrc"; then
cat << 'EOF' >> "$HOME/.zshrc"

export PATH="$HOME/.local/bin:$PATH"
command_not_found_handler() {
    if [[ "$1" =~ ^hey@(.+)$ ]]; then
        hey "${match[1]}"
    else
        printf "zsh: command not found: %s\n" "$1" >&2
        return 127
    fi
}
EOF
fi

# 2. Configure Bash if ~/.bashrc exists
if [ -f "$HOME/.bashrc" ] && ! grep -q "command_not_found_handle" "$HOME/.bashrc"; then
cat << 'EOF' >> "$HOME/.bashrc"

export PATH="$HOME/.local/bin:$PATH"
command_not_found_handle() {
    if [[ "$1" =~ ^hey@(.+)$ ]]; then
        hey "${BASH_REMATCH[1]}"
    else
        printf "%s: command not found\n" "$1" >&2
        return 127
    fi
}
EOF
fi

echo "Installation complete! Restart your terminal and type hey@username"
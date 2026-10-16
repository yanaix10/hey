#!/bin/bash

echo "Building binaries..."
go build -o hey ./cmd/hey
go build -o heyd ./cmd/heyd

echo "Installing to ~/.local/bin..."
mkdir -p ~/.local/bin
mv hey heyd ~/.local/bin/

echo "Creating systemd user service..."
mkdir -p ~/.config/systemd/user
cat <<EOF > ~/.config/systemd/user/heyd.service
[Unit]
Description=Hey P2P Chat Daemon
After=network.target

[Service]
ExecStart=%h/.local/bin/heyd
Restart=always
RestartSec=3
Environment="PATH=%h/.local/bin:/usr/bin"
Environment="TERMINAL=alacritty" 

[Install]
WantedBy=default.target
EOF

echo "Enabling and starting heyd service..."
systemctl --user daemon-reload
systemctl --user enable --now heyd.service

echo "Installation complete!"
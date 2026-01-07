#!/usr/bin/env bash
set -euo pipefail

echo "[*] Installing Asterisk (Ubuntu/Debian)..."

sudo apt update

# Install Asterisk and required packages
sudo apt install -y \
  asterisk \
  asterisk-core-sounds-en \
  asterisk-moh-opsound-wav \
  asterisk-modules \
  net-tools \
  tcpdump

sudo systemctl enable --now asterisk

echo "[*] Asterisk installed successfully"

# Check Asterisk version
asterisk -V

echo "[*] Asterisk service will be managed by systemd"

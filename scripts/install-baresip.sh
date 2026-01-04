#!/usr/bin/env bash
set -euo pipefail

echo "[*] Installing dependencies (Ubuntu/Debian)..."

sudo apt update

# Ubuntu 24.04 (noble) notes:
# - libasound2 is a virtual package; use libasound2t64
# - baresip-modules is not a package; use baresip-core (+ optional extras)
sudo apt install -y \
  baresip \
  baresip-core \
  libasound2t64 \
  libpulse0 \
  pulseaudio \
  alsa-utils \
  net-tools

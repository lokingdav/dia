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

# Optional media extras (uncomment if you want)
# sudo apt install -y baresip-ffmpeg

echo "[*] Creating baresip config directory..."

BARE_DIR="$HOME/.baresip"
mkdir -p "$BARE_DIR"

# ---- modules ----
cat > "$BARE_DIR/modules" <<'EOF'
# Audio
alsa
pulse

# Control interface
ctrl_tcp

# Optional (safe defaults)
uuid
EOF

# ---- config ----
cat > "$BARE_DIR/config" <<'EOF'
# Audio (prefer Pulse; ALSA still available)
audio_player        pulse
audio_source        pulse
audio_alert         pulse
audio_srate         48000
audio_channels      2

# SIP
sip_listen          0.0.0.0:5060
sip_transports      udp,tcp
rtp_timeout         60
rtp_ports           10000-20000

# ctrl_tcp for sidecar controller
ctrl_tcp_listen     127.0.0.1:4444

# Logging (useful for debugging)
log_level           2
EOF

# ---- accounts placeholder ----
ACCOUNTS_FILE="$BARE_DIR/accounts"
if [[ ! -f "$ACCOUNTS_FILE" ]]; then
  cat > "$ACCOUNTS_FILE" <<'EOF'
# Example:
# <sip:user@domain>;auth_user=user;auth_pass=pass;outbound=sip:proxy;regint=600
EOF
  echo "[!] Edit $ACCOUNTS_FILE and add your SIP account before running."
fi

echo "[*] Checking audio devices..."
aplay -l || true
arecord -l || true

echo "[*] Starting PulseAudio (if not running)..."
pulseaudio --check || pulseaudio --start

echo "[*] Starting baresip..."
echo "    ctrl_tcp listening on 127.0.0.1:4444"
echo "    config dir: $BARE_DIR"
echo

baresip -f "$BARE_DIR"

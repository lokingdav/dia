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

# # ---- accounts placeholder ----
# ACCOUNTS_FILE="$BARE_DIR/accounts"
# if [[ ! -f "$ACCOUNTS_FILE" ]]; then
#   cat > "$ACCOUNTS_FILE" <<'EOF'
# # Example:
# # <sip:user@domain>;auth_user=user;auth_pass=pass;outbound=sip:proxy;regint=600
# EOF
#   echo "[!] Edit $ACCOUNTS_FILE and add your SIP account before running."
# fi

# echo "[*] Checking audio devices..."
# aplay -l || true
# arecord -l || true

# echo "[*] Starting PulseAudio (if not running)..."
# pulseaudio --check || pulseaudio --start

# echo "[*] Starting baresip..."
# echo "    ctrl_tcp listening on 127.0.0.1:4444"
# echo "    config dir: $BARE_DIR"
# echo

# # baresip -f "$BARE_DIR"

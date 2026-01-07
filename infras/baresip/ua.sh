#!/usr/bin/env bash
set -euo pipefail

send_ctrl() {
  local json="$1"
  printf "%s:%s," "${#json}" "$json" | nc -w 1 127.0.0.1 4444 | sed -E 's/^[0-9]+://; s/,$//'
  echo
}

# Check arguments
if [ $# -lt 1 ]; then
  echo "Usage: $0 <command> [account]"
  echo "  command: dial|accept|hangup|etc"
  echo "  account: user@ip or SIP URI (required for dial)"
  echo ""
  echo "Examples:"
  echo "  $0 dial 1002@192.168.1.10"
  echo "  $0 accept"
  echo "  $0 hangup"
  exit 1
fi

COMMAND="$1"
ACCOUNT="${2:-}"

# Create JSON based on command
case "$COMMAND" in
  dial)
    if [ -z "$ACCOUNT" ]; then
        echo "Error: 'dial' requires a target"
        exit 1
    fi

    # If user already passed sip:, don't double-prefix it.
    if [[ "$ACCOUNT" == sip:* ]]; then
        TARGET="$ACCOUNT"
    else
        TARGET="sip:$ACCOUNT"
    fi

    JSON="{\"command\":\"dial\",\"params\":\"$TARGET\"}"
    ;;
  accept|recv)
    JSON="{\"command\":\"accept\",\"params\":{}}"
    ;;
  hangup)
    JSON="{\"command\":\"hangup\",\"params\":{}}"
    ;;
  *)
    if [ -n "$ACCOUNT" ]; then
      JSON="{\"command\":\"${COMMAND}\",\"params\":{\"uri\":\"sip:${ACCOUNT}\"}}"
    else
      JSON="{\"command\":\"${COMMAND}\",\"params\":{}}"
    fi
    ;;
esac

echo "Sending: $JSON"
send_ctrl "$JSON"

#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOSTS_YML="$ROOT_DIR/infras/hosts.yml"

BARESIP_PORT="${BARESIP_PORT:-4444}"
RELAY_PORT="${RELAY_PORT:-50052}"
DEFAULT_ODA_ATTRS="${ODA_ATTRS:-name,issuer}"

usage() {
  cat <<EOF
Usage:
  $0 ips

  # Interactive controller (REPL + logs)
  $0 it <account> [-- <extra sipcontroller flags>]

  # Recipient-only (no REPL needed; just logs/behavior)
  $0 recv <account> [-- <extra sipcontroller flags>]

  # Recipient baseline: auto-answer immediately (no DIA/ODA)
  $0 recv-base <account>

  # Recipient integrated: DIA gate, answer after verified, then ODA after CALL_ANSWERED, then hangup
  $0 recv-int-oda <account> [attrs]

  # Caller batch experiments (prints JSONL results)
  $0 call-base <account> <phone_or_uri> [runs] [concurrency]
  $0 call-int  <account> <phone_or_uri> [runs] [concurrency]

  # Start two interactive controllers (tries tmux; otherwise prints commands)
  $0 pair <accountA> <accountB>

Notes:
  - Account selects client automatically: 1XXX -> client-1, 2XXX -> client-2
  - Env file is inferred as: .env.<account> (e.g. .env.1001)
  - Addresses are read from: infras/hosts.yml

Examples:
  $0 it 1001
  $0 recv-base 2002
  $0 recv-int-oda 2002
  $0 call-base 1001 +15551234567 50 5
  $0 call-int  1001 +15551234567 50 5
  $0 pair 1001 2002
EOF
}

die() {
  echo "Error: $*" >&2
  exit 1
}

require_file() {
  [[ -f "$1" ]] || die "missing file: $1"
}

# Extract ansible_host for a given host name from hosts.yml.
# This assumes the current shallow structure:
#   <name>:
#     ansible_host: <ip>
get_ansible_host() {
  local host_name="$1"
  awk -v host="$host_name" '
    $1 == host":" { in_block=1; next }
    in_block && $1 == "ansible_host:" { print $2; exit }
    in_block && $1 ~ /^[a-zA-Z0-9_-]+:$/ { in_block=0 }
  ' "$HOSTS_YML"
}

resolve_client_from_account() {
  local account="$1"
  if [[ ! "$account" =~ ^[0-9]{4}$ ]]; then
    die "account must be 4 digits (e.g. 1001, 2002)"
  fi
  case "${account:0:1}" in
    1) echo "client-1" ;;
    2) echo "client-2" ;;
    *) die "account must start with 1 or 2 (1XXX->client-1, 2XXX->client-2)" ;;
  esac
}

sipcontroller_cmd_base() {
  local account="$1"

  require_file "$HOSTS_YML"

  local env_file="$ROOT_DIR/.env.${account}"
  require_file "$env_file"

  local client
  client="$(resolve_client_from_account "$account")"

  local client_ip server_ip
  client_ip="$(get_ansible_host "$client")"
  server_ip="$(get_ansible_host server)"

  [[ -n "$client_ip" ]] || die "failed to resolve $client ansible_host from $HOSTS_YML"
  [[ -n "$server_ip" ]] || die "failed to resolve server ansible_host from $HOSTS_YML"

  local baresip_addr relay_addr
  baresip_addr="${client_ip}:${BARESIP_PORT}"
  relay_addr="${server_ip}:${RELAY_PORT}"

  echo "go run ./cmd/sipcontroller/main.go -env \"$env_file\" -baresip \"$baresip_addr\" -relay \"$relay_addr\""
}

split_passthrough() {
  # Splits args into: positional args before --, and pass-through after --.
  # Prints two lines: BEFORE and AFTER (space-joined).
  local before=() after=() seen=0
  for a in "$@"; do
    if [[ "$a" == "--" && $seen -eq 0 ]]; then
      seen=1
      continue
    fi
    if [[ $seen -eq 0 ]]; then
      before+=("$a")
    else
      after+=("$a")
    fi
  done
  printf '%s\n' "${before[*]}" "${after[*]}"
}

cmd=${1:-}
shift || true

case "$cmd" in
  ""|"-h"|"--help"|"help")
    usage
    exit 0
    ;;

  ips)
    require_file "$HOSTS_YML"
    c1="$(get_ansible_host client-1)" || true
    c2="$(get_ansible_host client-2)" || true
    srv="$(get_ansible_host server)" || true
    echo "client-1: ${c1:-<missing>} (baresip: ${c1:-?}:${BARESIP_PORT})"
    echo "client-2: ${c2:-<missing>} (baresip: ${c2:-?}:${BARESIP_PORT})"
    echo "server:   ${srv:-<missing>} (relay:   ${srv:-?}:${RELAY_PORT})"
    ;;

  it)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 it <account> [-- extra flags]"
    shift || true
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    echo "[controller.sh] account=$account mode=interactive" >&2
    echo "[controller.sh] cmd: $base_cmd $before $after" >&2
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    eval "$base_cmd $before $after"
    ;;

  recv)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv <account> [-- extra flags]"
    shift || true
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    echo "[controller.sh] account=$account mode=recv" >&2
    echo "[controller.sh] cmd: $base_cmd $before $after" >&2
    cd "$ROOT_DIR"
    # Run with stdin closed so it behaves like a daemon-ish log sink.
    # shellcheck disable=SC2086
    eval "$base_cmd $before $after" < /dev/null
    ;;

  recv-base)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-base <account>"
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    eval "$base_cmd -incoming-mode baseline" < /dev/null
    ;;

  recv-int-oda)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-int-oda <account> [attrs]"
    attrs=${2:-$DEFAULT_ODA_ATTRS}
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    eval "$base_cmd -incoming-mode integrated -oda-after-answer -oda-attrs \"$attrs\"" < /dev/null
    ;;

  call-base)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-base <account> <phone_or_uri> [runs] [concurrency]"
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    eval "$base_cmd -experiment baseline -phone \"$phone\" -runs $runs -concurrency $conc"
    ;;

  call-int)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-int <account> <phone_or_uri> [runs] [concurrency]"
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    eval "$base_cmd -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc"
    ;;

  pair)
    a=${1:-}
    b=${2:-}
    [[ -n "$a" && -n "$b" ]] || die "usage: $0 pair <accountA> <accountB>"

    cmd_a="$(sipcontroller_cmd_base "$a")"
    cmd_b="$(sipcontroller_cmd_base "$b")"

    if command -v tmux >/dev/null 2>&1; then
      session="sipctl-${a}-${b}"
      cd "$ROOT_DIR"
      tmux new-session -d -s "$session" "bash -lc '$cmd_a'"
      tmux split-window -h -t "$session" "bash -lc '$cmd_b'"
      tmux select-layout -t "$session" even-horizontal
      tmux attach -t "$session"
    else
      echo "tmux not found; run these in two terminals:" >&2
      echo "  (A) cd $ROOT_DIR && $cmd_a" >&2
      echo "  (B) cd $ROOT_DIR && $cmd_b" >&2
      exit 1
    fi
    ;;

  *)
    die "unknown command '$cmd' (try '$0 help')"
    ;;
esac

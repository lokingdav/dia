#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
HOSTS_YML="$ROOT_DIR/infras/hosts.yml"

BARESIP_PORT="${BARESIP_PORT:-4444}"
RELAY_PORT="${RELAY_PORT:-50052}"
RELAY_ADDR="${RELAY_ADDR:-localhost:${RELAY_PORT}}"
DEFAULT_ODA_ATTRS="${ODA_ATTRS:-name,issuer}"
DEFAULT_CSV_DIR="${CSV_DIR:-$ROOT_DIR/results}"
DEFAULT_INTER_ATTEMPT_MS="${INTER_ATTEMPT_MS:-1000}"

# Redis-backed peer-session cache defaults (used by *-cache commands)
REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
REDIS_USER="${REDIS_USER:-}"
REDIS_PASS="${REDIS_PASS:-}"
REDIS_DB="${REDIS_DB:-0}"
REDIS_PREFIX="${REDIS_PREFIX:-denseid:dia:peer_session:v1}"
PEER_SESSION_TTL="${PEER_SESSION_TTL:-0}"

usage() {
  cat <<EOF
Usage:
  $0 ips

  # Interactive controller (REPL + logs)
  $0 it <account> [-- <extra sipcontroller flags>]
  $0 it-cache <account> [-- <extra sipcontroller flags>]

  # Recipient-only (no REPL needed; just logs/behavior)
  # NOTE: recv defaults to integrated.
  $0 recv <account> [-- <extra sipcontroller flags>]
  $0 recv-cache <account> [-- <extra sipcontroller flags>]

  # Recipient baseline: auto-answer immediately (no DIA/ODA)
  $0 recv-base <account>

  # Recipient integrated ODA: DIA gate, answer after verified, then ODA after CALL_ANSWERED, then hangup
  $0 recv-oda <account> [attrs] [-- <extra sipcontroller flags>]
  $0 recv-oda-cache <account> [attrs] [-- <extra sipcontroller flags>]

  # Caller batch experiments (prints JSONL results)
  # NOTE: call defaults to integrated.
  $0 call <account> <phone_or_uri> [runs] [concurrency] [-- <extra sipcontroller flags>]
  $0 call-cache <account> <phone_or_uri> [runs] [concurrency] [-- <extra sipcontroller flags>]
  $0 call-base <account> <phone_or_uri> [runs] [concurrency] [-- <extra sipcontroller flags>]
  $0 call-oda <account> <phone_or_uri> [runs] [concurrency] [oda_delay_after_rua_sec] [-- <extra sipcontroller flags>]
  $0 call-oda-cache <account> <phone_or_uri> [runs] [concurrency] [oda_delay_after_rua_sec] [-- <extra sipcontroller flags>]

  # Start two interactive controllers (tries tmux; otherwise prints commands)
  $0 pair <accountA> <accountB>

Notes:
  - Account selects client automatically: 1XXX -> client-1, 2XXX -> client-2
  - Env file is inferred as: .env.<account> (e.g. .env.1001)
  - Baresip addresses are read from: infras/hosts.yml
  - Relay address defaults to: localhost:50052 (override via env var RELAY_ADDR)
  - call-* and recv-int-oda write CSV by default to: results/dia_<account>_<timestamp>.csv (override with -csv)
  - *-cache commands enable DIA peer-session caching via Redis (sipcontroller flag: -cache)
  - Redis defaults can be overridden via env vars: REDIS_ADDR, REDIS_USER, REDIS_PASS, REDIS_DB, REDIS_PREFIX, PEER_SESSION_TTL

Examples:
  $0 it 1001
  $0 it-cache 1001
  $0 recv-base 2002
  $0 recv-oda 2002
  $0 recv-cache 2002
  $0 call-base 1001 +15551234567 50 5
  $0 call  1001 +15551234567 50 5
  $0 call-cache  1001 +15551234567 50 5
  $0 call-oda 1001 +15551234567 50 5 2
  $0 pair 1001 2002
EOF
}

die() {
  echo "Error: $*" >&2
  exit 1
}

run_allow_sigint() {
  # When you Ctrl+C a bash script, bash itself will often exit with 130 even if
  # the child handles SIGINT gracefully. Run the child in the background and
  # forward SIGINT so we return the child's real exit code.
  local cmd="$1"
  local pid rc interrupted=0

  set +e
  eval "$cmd" &
  pid=$!

  trap 'interrupted=1; kill -INT "$pid" 2>/dev/null' INT

  # Wait for the child to exit. If Ctrl+C was pressed, the trap above will
  # signal the child; we still wait to collect its exit code.
  wait "$pid"
  rc=$?

  # With `go run`, the go toolchain may exit 130 on SIGINT even if the actual
  # program handled SIGINT gracefully and printed its normal shutdown logs.
  # Treat that as a clean exit for our wrapper purposes.
  if [[ $interrupted -eq 1 && $rc -eq 130 ]]; then
    rc=0
  fi

  trap - INT
  set -e
  return $rc
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

resolve_relay_host() {
  # Deprecated: relay host is no longer inferred from hosts.yml.
  # Keep this function only to avoid breaking older scripts sourcing it.
  return 1
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

  local env_file="$ROOT_DIR/.env.${account}"
  require_file "$env_file"

  local client
  client="$(resolve_client_from_account "$account")"

  local client_ip
  client_ip=""
  if [[ -f "$HOSTS_YML" ]]; then
    client_ip="$(get_ansible_host "$client" || true)"
  else
    echo "[controller.sh] warning: $HOSTS_YML not found; falling back to localhost" >&2
  fi

  if [[ -z "$client_ip" ]]; then
    echo "[controller.sh] warning: missing '$client.ansible_host' in $HOSTS_YML; using localhost" >&2
    client_ip="localhost"
  fi
  local baresip_addr relay_addr
  baresip_addr="${client_ip}:${BARESIP_PORT}"
  relay_addr="$RELAY_ADDR"

  echo "go run ./cmd/sipcontroller/main.go -account \"$account\" -env \"$env_file\" -baresip \"$baresip_addr\" -relay \"$relay_addr\""
}

cache_flags() {
  local out
  out="-cache -redis \"$REDIS_ADDR\" -redis-db $REDIS_DB -redis-prefix \"$REDIS_PREFIX\" -peer-session-ttl $PEER_SESSION_TTL"
  if [[ -n "$REDIS_USER" ]]; then
    out+=" -redis-user \"$REDIS_USER\""
  fi
  if [[ -n "$REDIS_PASS" ]]; then
    out+=" -redis-pass \"$REDIS_PASS\""
  fi
  echo "$out"
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

has_csv_flag() {
  for a in "$@"; do
    case "$a" in
      -csv|-csv=*)
        return 0
        ;;
    esac
  done
  return 1
}

has_inter_attempt_flag() {
  for a in "$@"; do
    case "$a" in
      -inter-attempt-ms|-inter-attempt-ms=*)
        return 0
        ;;
    esac
  done
  return 1
}

has_oda_attrs_flag() {
  for a in "$@"; do
    case "$a" in
      -oda-attrs|-oda-attrs=*)
        return 0
        ;;
    esac
  done
  return 1
}

default_csv_path() {
  local mode="$1"
  local account="$2"
  local ts
  ts="$(date +%Y%m%d-%H%M%S)"
  echo "$DEFAULT_CSV_DIR/dia_${account}_${ts}.csv"
}

cmd=${1:-}
shift || true

case "$cmd" in
  ""|"-h"|"--help"|"help")
    usage
    exit 0
    ;;

  ips)
    if [[ -f "$HOSTS_YML" ]]; then
      c1="$(get_ansible_host client-1)" || true
      c2="$(get_ansible_host client-2)" || true
    else
      c1=""
      c2=""
      echo "[controller.sh] warning: $HOSTS_YML not found" >&2
    fi
    echo "client-1: ${c1:-<missing>} (baresip: ${c1:-?}:${BARESIP_PORT})"
    echo "client-2: ${c2:-<missing>} (baresip: ${c2:-?}:${BARESIP_PORT})"
    echo "relay:    $RELAY_ADDR"
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
    run_allow_sigint "$base_cmd $before $after"
    ;;

  it-cache)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 it-cache <account> [-- extra flags]"
    shift || true
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cache_arg="$(cache_flags)"
    echo "[controller.sh] account=$account mode=interactive-cache" >&2
    echo "[controller.sh] cmd: $base_cmd $cache_arg $before $after" >&2
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd $cache_arg $before $after"
    ;;

  recv)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv <account> [-- extra flags]"
    shift || true
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    echo "[controller.sh] account=$account mode=recv(integrated)" >&2
    echo "[controller.sh] cmd: $base_cmd -incoming-mode integrated $before $after" >&2
    cd "$ROOT_DIR"
    # Run with stdin closed so it behaves like a daemon-ish log sink.
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -incoming-mode integrated $before $after" < /dev/null
    ;;

  recv-cache)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-cache <account> [-- extra flags]"
    shift || true
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cache_arg="$(cache_flags)"
    echo "[controller.sh] account=$account mode=recv-cache(integrated)" >&2
    echo "[controller.sh] cmd: $base_cmd -incoming-mode integrated $cache_arg $before $after" >&2
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -incoming-mode integrated $cache_arg $before $after" < /dev/null
    ;;

  recv-base)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-base <account>"
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -incoming-mode baseline" < /dev/null
    ;;

  recv-oda)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-oda <account> [attrs] [-- extra flags]"
    shift || true

    attrs="$DEFAULT_ODA_ATTRS"
    if [[ ${1:-} != "" && ${1:-} != "--" ]]; then
      attrs="$1"
      shift || true
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path oda "$account")\""
    fi
    run_allow_sigint "$base_cmd -incoming-mode integrated -oda-attrs \"$attrs\" $csv_arg $before $after" < /dev/null
    ;;

  recv-oda-cache)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-oda-cache <account> [attrs] [-- extra flags]"
    shift || true

    attrs="$DEFAULT_ODA_ATTRS"
    if [[ ${1:-} != "" && ${1:-} != "--" ]]; then
      attrs="$1"
      shift || true
    fi
    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cache_arg="$(cache_flags)"

    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path oda "$account")\""
    fi
    run_allow_sigint "$base_cmd -incoming-mode integrated $cache_arg -oda-attrs \"$attrs\" $csv_arg $before $after" < /dev/null
    ;;

  call-oda)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    oda_delay_sec=${5:-0}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-oda <account> <phone_or_uri> [runs] [concurrency] [oda_delay_after_rua_sec] [-- extra flags]"
    if [[ $# -ge 5 ]]; then shift 5; else shift $#; fi

    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path integrated "$account")\""
    fi

    delay_arg=""
    if ! has_inter_attempt_flag "$@"; then
      delay_arg="-inter-attempt-ms $DEFAULT_INTER_ATTEMPT_MS"
    fi

    oda_attrs_arg=""
    if ! has_oda_attrs_flag "$@"; then
      oda_attrs_arg="-oda-attrs \"$DEFAULT_ODA_ATTRS\""
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc -outgoing-oda $oda_delay_sec $oda_attrs_arg $delay_arg $csv_arg $before $after"
    ;;

  call-oda-cache)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    oda_delay_sec=${5:-0}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-oda-cache <account> <phone_or_uri> [runs] [concurrency] [oda_delay_after_rua_sec] [-- extra flags]"
    if [[ $# -ge 5 ]]; then shift 5; else shift $#; fi

    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path integrated "$account")\""
    fi

    delay_arg=""
    if ! has_inter_attempt_flag "$@"; then
      delay_arg="-inter-attempt-ms $DEFAULT_INTER_ATTEMPT_MS"
    fi

    oda_attrs_arg=""
    if ! has_oda_attrs_flag "$@"; then
      oda_attrs_arg="-oda-attrs \"$DEFAULT_ODA_ATTRS\""
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cache_arg="$(cache_flags)"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd $cache_arg -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc -outgoing-oda $oda_delay_sec $oda_attrs_arg $delay_arg $csv_arg $before $after"
    ;;

  call-base)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-base <account> <phone_or_uri> [runs] [concurrency] [-- extra flags]"
    if [[ $# -ge 4 ]]; then shift 4; else shift $#; fi
    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path baseline "$account")\""
    fi

    delay_arg=""
    if ! has_inter_attempt_flag "$@"; then
      delay_arg="-inter-attempt-ms $DEFAULT_INTER_ATTEMPT_MS"
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -experiment baseline -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after"
    ;;

  call)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call <account> <phone_or_uri> [runs] [concurrency] [-- extra flags]"
    if [[ $# -ge 4 ]]; then shift 4; else shift $#; fi
    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path integrated "$account")\""
    fi

    delay_arg=""
    if ! has_inter_attempt_flag "$@"; then
      delay_arg="-inter-attempt-ms $DEFAULT_INTER_ATTEMPT_MS"
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after"
    ;;

  call-cache)
    account=${1:-}
    phone=${2:-}
    runs=${3:-1}
    conc=${4:-1}
    [[ -n "$account" && -n "$phone" ]] || die "usage: $0 call-cache <account> <phone_or_uri> [runs] [concurrency] [-- extra flags]"
    if [[ $# -ge 4 ]]; then shift 4; else shift $#; fi
    csv_arg=""
    if ! has_csv_flag "$@"; then
      csv_arg="-csv \"$(default_csv_path integrated "$account")\""
    fi

    delay_arg=""
    if ! has_inter_attempt_flag "$@"; then
      delay_arg="-inter-attempt-ms $DEFAULT_INTER_ATTEMPT_MS"
    fi

    read -r before after < <(split_passthrough "$@")
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cache_arg="$(cache_flags)"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd $cache_arg -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after"
    ;;

  # Deprecated aliases (kept for compatibility)
  recv-int)
    echo "[controller.sh] warning: 'recv-int' is deprecated; use 'recv'" >&2
    exec "$0" recv "$@"
    ;;
  recv-int-cache)
    echo "[controller.sh] warning: 'recv-int-cache' is deprecated; use 'recv-cache'" >&2
    exec "$0" recv-cache "$@"
    ;;
  recv-int-oda)
    echo "[controller.sh] warning: 'recv-int-oda' is deprecated; use 'recv-oda'" >&2
    exec "$0" recv-oda "$@"
    ;;
  recv-int-oda-cache)
    echo "[controller.sh] warning: 'recv-int-oda-cache' is deprecated; use 'recv-oda-cache'" >&2
    exec "$0" recv-oda-cache "$@"
    ;;
  call-int)
    echo "[controller.sh] warning: 'call-int' is deprecated; use 'call'" >&2
    exec "$0" call "$@"
    ;;
  call-int-cache)
    echo "[controller.sh] warning: 'call-int-cache' is deprecated; use 'call-cache'" >&2
    exec "$0" call-cache "$@"
    ;;
  call-int-oda)
    echo "[controller.sh] warning: 'call-int-oda' is deprecated; use 'call-oda'" >&2
    exec "$0" call-oda "$@"
    ;;
  call-int-oda-cache)
    echo "[controller.sh] warning: 'call-int-oda-cache' is deprecated; use 'call-oda-cache'" >&2
    exec "$0" call-oda-cache "$@"
    ;;
  caller-int-oda)
    echo "[controller.sh] warning: 'caller-int-oda' is deprecated; use 'call-oda'" >&2
    exec "$0" call-oda "$@"
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

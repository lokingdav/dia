#!/usr/bin/env bash
set -euo pipefail

ROOT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
CTRL_ENV="$ROOT_DIR/.env.ctrl"

# Auto-generate .env.ctrl if missing
if [[ ! -f "$CTRL_ENV" ]]; then
  echo "[controller.sh] .env.ctrl not found, generating..." >&2
  "$ROOT_DIR/scripts/create-ctrl-env.sh"
fi

# Load controller environment
if [[ -f "$CTRL_ENV" ]]; then
  # shellcheck disable=SC1090
  source "$CTRL_ENV"
else
  echo "[controller.sh] Error: failed to load $CTRL_ENV" >&2
  exit 1
fi

# Allow environment overrides for key variables
RELAY_ADDR="${RELAY_ADDR:-localhost:50052}"
REDIS_ADDR="${REDIS_ADDR:-localhost:6379}"
REDIS_USER="${REDIS_USER:-}"
REDIS_PASS="${REDIS_PASS:-}"
REDIS_DB="${REDIS_DB:-0}"
REDIS_PREFIX="${REDIS_PREFIX:-denseid:dia:peer_session:v1}"
PEER_SESSION_TTL="${PEER_SESSION_TTL:-0}"
DEFAULT_ODA_ATTRS="${DEFAULT_ODA_ATTRS:-name,issuer}"
DEFAULT_CSV_DIR="${DEFAULT_CSV_DIR:-$ROOT_DIR/results}"
DEFAULT_INTER_ATTEMPT_MS="${DEFAULT_INTER_ATTEMPT_MS:-1000}"

usage() {
  cat <<EOF
Usage:
  $0 ips

  # Cache utilities
  $0 clear-cache

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
  - Baresip/relay addresses loaded from: .env.ctrl (auto-generated from infras/hosts.yml)
  - To regenerate .env.ctrl: ./scripts/create-ctrl-env.sh --force
  - call-* and recv-int-oda write CSV by default to: results/dia_<account>_<timestamp>.csv (override with -csv)
  - *-cache commands that auto-write CSV use: results/dia_cache_<account>_<timestamp>.csv (override with -csv)
  - *-cache commands enable DIA peer-session caching via Redis (sipcontroller flag: -cache)
  - Override env vars: RELAY_ADDR, REDIS_ADDR, REDIS_USER, REDIS_PASS, REDIS_DB, REDIS_PREFIX, PEER_SESSION_TTL

Examples:
  $0 it 1001
  $0 it-cache 1001
  $0 clear-cache
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

  # Select baresip address based on client
  local baresip_addr
  case "$client" in
    client-1) baresip_addr="${CLIENT_1_ADDR:-localhost:4444}" ;;
    client-2) baresip_addr="${CLIENT_2_ADDR:-localhost:4444}" ;;
    *) baresip_addr="localhost:4444" ;;
  esac

  echo "go run ./cmd/sipcontroller/main.go -account \"$account\" -env \"$env_file\" -baresip \"$baresip_addr\" -relay \"$RELAY_ADDR\""
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
  local -a before
  local -a after
  local seen=0
  before=()
  after=()
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
  # Use default expansions to stay safe under `set -u`.
  printf '%s\n' "${before[*]-}" "${after[*]-}"
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
  local cache_on="${3:-0}"
  local ts
  ts="$(date +%Y%m%d-%H%M%S)"
  if [[ "$cache_on" == "1" ]]; then
    echo "$DEFAULT_CSV_DIR/dia_cache_${account}_${ts}.csv"
  else
    echo "$DEFAULT_CSV_DIR/dia_${account}_${ts}.csv"
  fi
}

cmd=${1:-}
shift || true

case "$cmd" in
  ""|"-h"|"--help"|"help")
    usage
    exit 0
    ;;

  ips)
    echo "client-1: ${CLIENT_1_ADDR:-<missing>}"
    echo "client-2: ${CLIENT_2_ADDR:-<missing>}"
    echo "relay:    ${RELAY_ADDR:-<missing>}"
    echo ""
    echo "(Loaded from $CTRL_ENV)"
    ;;

  clear-cache)
    cache_arg="$(cache_flags)"
    echo "[controller.sh] action=clear-cache" >&2
    echo "[controller.sh] cmd: go run ./cmd/sipcontroller/main.go -clear-cache $cache_arg" >&2
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "go run ./cmd/sipcontroller/main.go -clear-cache $cache_arg"
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
    run_allow_sigint "$base_cmd -incoming-mode integrated $before $after 2>&1 | tee \"$ROOT_DIR/recipient.log\"" < /dev/null
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
    run_allow_sigint "$base_cmd -incoming-mode integrated $cache_arg $before $after 2>&1 | tee \"$ROOT_DIR/recipient.log\"" < /dev/null
    ;;

  recv-base)
    account=${1:-}
    [[ -n "$account" ]] || die "usage: $0 recv-base <account>"
    base_cmd="$(sipcontroller_cmd_base "$account")"
    cd "$ROOT_DIR"
    # shellcheck disable=SC2086
    run_allow_sigint "$base_cmd -incoming-mode baseline 2>&1 | tee \"$ROOT_DIR/recipient.log\"" < /dev/null
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
    run_allow_sigint "$base_cmd -incoming-mode integrated -oda-attrs \"$attrs\" $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/recipient.log\"" < /dev/null
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
      csv_arg="-csv \"$(default_csv_path oda "$account" 1)\""
    fi
    run_allow_sigint "$base_cmd -incoming-mode integrated $cache_arg -oda-attrs \"$attrs\" $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/recipient.log\"" < /dev/null
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
    run_allow_sigint "$base_cmd -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc -outgoing-oda $oda_delay_sec $oda_attrs_arg $delay_arg $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/caller.log\""
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
      csv_arg="-csv \"$(default_csv_path integrated "$account" 1)\""
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
    run_allow_sigint "$base_cmd $cache_arg -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc -outgoing-oda $oda_delay_sec $oda_attrs_arg $delay_arg $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/caller.log\""
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
    run_allow_sigint "$base_cmd -experiment baseline -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/caller.log\""
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
    run_allow_sigint "$base_cmd -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/caller.log\""
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
      csv_arg="-csv \"$(default_csv_path integrated "$account" 1)\""
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
    run_allow_sigint "$base_cmd $cache_arg -experiment integrated -phone \"$phone\" -runs $runs -concurrency $conc $delay_arg $csv_arg $before $after 2>&1 | tee \"$ROOT_DIR/caller.log\""
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

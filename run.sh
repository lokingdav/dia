#!/usr/bin/env bash
set -euo pipefail

cmd=${1:-}
shift || true

profile=${1:-}

# list of valid profiles
PROFILES=(es rs expctrl)

usage() {
  echo "Usage:"
  echo "  $0 {up|down|restart} [all|${PROFILES[*]}]"
  echo "  $0 {alice|bob|alice-cache|bob-cache} [-- extra callauth flags]"
  echo "  $0 clear-cache [-- extra redis flags]"
  exit 1
}

# build array of “--profile X” for all defined profiles
ALL_PROFILE_ARGS=()
for p in "${PROFILES[@]}"; do
  ALL_PROFILE_ARGS+=(--profile "$p")
done

case "$cmd" in
  up)
    if [[ -z "$profile" ]]; then
      echo "Error: no profile specified."
      usage
    fi

    shift || true

    if [[ "$profile" == "all" ]]; then
      docker compose "${ALL_PROFILE_ARGS[@]}" up -d --build
    else
      # check it's one of the allowed profiles
      if [[ ! " ${PROFILES[*]} " =~ " $profile " ]]; then
        echo "Error: unknown profile '$profile'."
        usage
      fi
      docker compose --profile "$profile" up -d --build
    fi
    ;;

  down)
    # “all” or empty => full shutdown; otherwise shut down only that profile
    if [[ -z "$profile" || "$profile" == "all" ]]; then
      docker compose "${ALL_PROFILE_ARGS[@]}" down
    else
      if [[ ! " ${PROFILES[*]} " =~ " $profile " ]]; then
        echo "Error: unknown profile '$profile'."
        usage
      fi
      docker compose --profile "$profile" down
    fi
    ;;

  restart)
    if [[ -z "$profile" ]]; then
      echo "Error: no profile specified."
      usage
    fi

    shift || true

    # First, bring down the services
    if [[ "$profile" == "all" ]]; then
      docker compose "${ALL_PROFILE_ARGS[@]}" down
    else
      # check it's one of the allowed profiles
      if [[ ! " ${PROFILES[*]} " =~ " $profile " ]]; then
        echo "Error: unknown profile '$profile'."
        usage
      fi
      docker compose --profile "$profile" down
    fi

    # Then, bring them back up
    if [[ "$profile" == "all" ]]; then
      docker compose "${ALL_PROFILE_ARGS[@]}" up -d --build
    else
      docker compose --profile "$profile" up -d --build
    fi
    ;;

  alice)
    go run cmd/client/callauth/main.go --dial 2001 --env .env.1001 --relay localhost:50052 "$@"
    ;;
  alice-cache)
    go run cmd/client/callauth/main.go --dial 2001 --env .env.1001 --relay localhost:50052 \
      --cache --self 1001 \
      "$@"
    ;;
  bob)
    go run cmd/client/callauth/main.go --receive 1001 --env .env.2001 --relay localhost:50052 "$@"
    ;;
  bob-cache)
    go run cmd/client/callauth/main.go --receive 1001 --env .env.2001 --relay localhost:50052 \
      --cache --self 2001 \
      "$@"
    ;;

  clear-cache)
    # One-shot: clears *all* peer-session cache entries under the configured Redis prefix.
    # Defaults: redis=localhost:6379 db=0 prefix=denseid:dia:peer_session:v1
    go run cmd/sipcontroller/main.go --clear-cache "$@"
    ;;
  *)
    echo "Error: unknown command '$cmd'."
    usage
    ;;
esac

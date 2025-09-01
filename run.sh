#!/usr/bin/env bash
set -euo pipefail

cmd=${1:-}
profile=${2:-}

# list of valid profiles
PROFILES=(es ks rv rs)

usage() {
  echo "Usage: $0 {up|down} [all|${PROFILES[*]}]"
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

  alice)
    go run cmd/client/callauth/main.go --dial bob --env .env.alice
    ;;
  bob)
    go run cmd/client/callauth/main.go --receive alice --env .env.bob
    ;;
  *)
    echo "Error: unknown command '$cmd'."
    usage
    ;;
esac

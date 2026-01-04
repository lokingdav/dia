#!/usr/bin/env bash

set -e

CMD="$1"
IMG="$2"

MAIN_IMAGE="kofidahmed/dia"
CONTROL_IMAGE="dia-ctrl"

DF_DIA="dia"
DF_CTRL="ctrl"

push_image() {
  echo "Pushing $MAIN_IMAGE to Docker Hub..."
  docker push "$MAIN_IMAGE"
}

build_image() {
  local img="$1"
  local push="$2"

  case "$img" in
    dia)
      echo "Building $MAIN_IMAGE"
      docker build -f "Dockerfile.$DF_DIA" -t "$MAIN_IMAGE" . --no-cache
      ;;
    ctrl)
      echo "Building $CONTROL_IMAGE"
      docker build -f "Dockerfile.$DF_CTRL" -t "$CONTROL_IMAGE" .
      ;;
    *)
      echo "Building All Images: $MAIN_IMAGE, $CONTROL_IMAGE, $DIND_VM_IMAGE"
      docker build -f "Dockerfile.$DF_DIA" -t "$MAIN_IMAGE" .
      docker build -f "Dockerfile.$DF_CTRL" -t "$CONTROL_IMAGE" .
      ;;
  esac

  if [[ "$push" == "--push" ]]; then
    push_image
  fi
}

case "$CMD" in
  build)
    build_image "$IMG" "$3"
    ;;
  push)
    push_image
    ;;
  *)
    echo "Unknown command: $CMD"
    echo "Usage: {build|push} [image] [--push]"
    echo "Available images: $DF_DIA, $DF_CTRL"
    exit 1
    ;;
esac

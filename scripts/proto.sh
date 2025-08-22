#!/bin/bash

# Exit immediately if a command fails.
set -e

# The profile name (e.g., "enrollment"), passed as the first argument.
profile=$1

# Check if the profile name was provided.
if [ -z "$profile" ]; then
  echo "Error: No profile name specified." >&2
  echo "Usage: $0 <profile_name>" >&2
  exit 1
fi

# Construct the full path using the profile name.
proto_path="api/proto/${profile}/v1/${profile}.proto"

# Verify the constructed file path exists before trying to compile.
if [ ! -f "$proto_path" ]; then
    echo "Error: File not found at '$proto_path'" >&2
    exit 1
fi

# Run the protoc compiler.
protoc --proto_path=api/proto \
    --go_out=api/go --go_opt=paths=source_relative \
    --go-grpc_out=api/go --go-grpc_opt=paths=source_relative \
    "$proto_path"

echo "Successfully generated Go code for $proto_path"
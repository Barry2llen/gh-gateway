#!/usr/bin/env bash

set -euo pipefail

: "${GH_TOKEN:?GH_TOKEN is required}"
: "${PRERELEASE:?PRERELEASE is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"

args=(
  release create "${RELEASE_TAG}"
  dist/gh-gateway-windows-amd64.exe
  dist/gh-gateway-windows-arm64.exe
  dist/SHA256SUMS
  --verify-tag
  --title "${RELEASE_TAG}"
  --generate-notes
)
if [[ "${PRERELEASE}" == "true" ]]; then
  args+=(--prerelease)
fi

gh "${args[@]}"

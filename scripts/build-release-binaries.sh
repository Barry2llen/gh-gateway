#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"

output_dir="${OUTPUT_DIR:-dist}"
mkdir -p "${output_dir}"

ldflags="-s -w -X gh-gateway/internal/version.Version=${RELEASE_TAG} -X gh-gateway/internal/version.Commit=${GITHUB_SHA}"
for arch in amd64 arm64; do
  output="${output_dir}/gh-gateway-windows-${arch}.exe"
  CGO_ENABLED=0 GOOS=windows GOARCH="${arch}" go build -trimpath -ldflags="${ldflags}" -o "${output}" ./cmd/gh-gateway
done

(
  cd "${output_dir}"
  sha256sum gh-gateway-windows-amd64.exe gh-gateway-windows-arm64.exe > SHA256SUMS
)

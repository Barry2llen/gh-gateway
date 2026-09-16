#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${RELEASE_TAG:?RELEASE_TAG is required}"

git tag -a "${RELEASE_TAG}" "${GITHUB_SHA}" -m "Release ${RELEASE_TAG}"
git push origin "refs/tags/${RELEASE_TAG}"

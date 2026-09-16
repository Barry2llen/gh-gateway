#!/usr/bin/env bash

set -euo pipefail

: "${GITHUB_REF:?GITHUB_REF is required}"
: "${GITHUB_SHA:?GITHUB_SHA is required}"
: "${GITHUB_OUTPUT:?GITHUB_OUTPUT is required}"
: "${IMAGE_NAME:?IMAGE_NAME is required}"
: "${RELEASE_TAG_INPUT:?RELEASE_TAG_INPUT is required}"

if [[ "${GITHUB_REF}" != "refs/heads/main" ]]; then
  echo "Releases must be dispatched from the main branch, got ${GITHUB_REF}." >&2
  exit 1
fi

tag="${RELEASE_TAG_INPUT}"
semver_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)(-((0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*)(\.(0|[1-9][0-9]*|[0-9A-Za-z-]*[A-Za-z-][0-9A-Za-z-]*))*))?$'
stable_re='^v(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)$'
if [[ ! "${tag}" =~ ${semver_re} ]]; then
  echo "Tag must be a semantic version such as v0.1.0 or v0.1.0-rc.1: ${tag}" >&2
  exit 1
fi

tags="${IMAGE_NAME}:${tag}"
prerelease=true
if [[ "${tag}" =~ ${stable_re} ]]; then
  version="${tag#v}"
  major="${version%%.*}"
  remainder="${version#*.}"
  minor="${remainder%%.*}"
  tags="${tags}"$'\n'"${IMAGE_NAME}:${major}.${minor}"$'\n'"${IMAGE_NAME}:latest"
  prerelease=false
fi

tag_exists=false
if existing_commit="$(git rev-parse -q --verify "refs/tags/${tag}^{commit}")"; then
  if [[ "${existing_commit}" != "${GITHUB_SHA}" ]]; then
    echo "Tag ${tag} already points to ${existing_commit}, not ${GITHUB_SHA}." >&2
    exit 1
  fi
  tag_exists=true
fi

{
  echo "tag=${tag}"
  echo "prerelease=${prerelease}"
  echo "tag_exists=${tag_exists}"
  echo "image_tags<<EOF"
  echo "${tags}"
  echo "EOF"
} >> "${GITHUB_OUTPUT}"

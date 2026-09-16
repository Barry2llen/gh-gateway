#!/bin/sh
set -eu

token_file="${GITEA_RUNNER_REGISTRATION_TOKEN_FILE:-/state/runner.token}"
attempts=0
until [ -s "${token_file}" ]; do
  attempts=$((attempts + 1))
  [ "${attempts}" -lt 120 ] || { echo "runner token was not created" >&2; exit 1; }
  sleep 1
done

mkdir -p /data
cd /data
if [ ! -s .runner ]; then
  act_runner register --no-interactive \
    --instance "${GITEA_INSTANCE_URL}" \
    --token "$(cat "${token_file}")" \
    --name "${GITEA_RUNNER_NAME:-gh-gateway-e2e}" \
    --labels "${GITEA_RUNNER_LABELS:-gh-gateway-e2e:host}"
fi

exec act_runner daemon

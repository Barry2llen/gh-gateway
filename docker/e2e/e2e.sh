#!/bin/sh
set -eu

fail() {
  echo "E2E failure: $*" >&2
  exit 1
}

wait_for_file() {
  path="$1"
  attempts=0
  until [ -s "${path}" ]; do
    attempts=$((attempts + 1))
    [ "${attempts}" -lt 60 ] || fail "timed out waiting for ${path}"
    sleep 1
  done
}

wait_for_file /state/gateway.token
wait_for_file /state/forker.token
wait_for_file /caddy-data/caddy/pki/authorities/local/root.crt

cp /caddy-data/caddy/pki/authorities/local/root.crt /usr/local/share/ca-certificates/caddy-local.crt
update-ca-certificates >/dev/null

gateway_token="$(cat /state/gateway.token)"
forker_token="$(cat /state/forker.token)"
gitea_url="http://gitea:3000/api/v1"

create_status="$(curl -sS -o /tmp/create-repository.json -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"bar","private":false}' \
  "${gitea_url}/user/repos")"
case "${create_status}" in
  201|409|422) ;;
  *) cat /tmp/create-repository.json >&2; fail "repository creation returned HTTP ${create_status}" ;;
esac

fork_status="$(curl -sS -o /tmp/create-fork.json -w '%{http_code}' \
  -H "Authorization: token ${forker_token}" \
  -H 'Content-Type: application/json' \
  -d '{"name":"bar"}' \
  "${gitea_url}/repos/gateway/bar/forks")"
case "${fork_status}" in
  202|201|409|422) ;;
  *) cat /tmp/create-fork.json >&2; fail "fork creation returned HTTP ${fork_status}" ;;
esac

attempts=0
until curl -fsS -H "Authorization: token ${forker_token}" "${gitea_url}/repos/forker/bar" >/dev/null; do
  attempts=$((attempts + 1))
  [ "${attempts}" -lt 60 ] || fail "timed out waiting for fork repository"
  sleep 1
done

gh --version

mkdir -p /tmp/nonfork /tmp/fork
git -C /tmp/nonfork init -q
git -C /tmp/nonfork remote add origin https://git.example.test/gateway/bar.git
git -C /tmp/fork init -q
git -C /tmp/fork remote add origin https://git.example.test/forker/bar.git

export GH_HOST=git.example.test
export GH_PROMPT_DISABLED=1

export GH_ENTERPRISE_TOKEN="${gateway_token}"
nonfork_json="$(cd /tmp/nonfork && gh repo view --json nameWithOwner,parent)"
echo "non-fork: ${nonfork_json}"
echo "${nonfork_json}" | jq -e '.nameWithOwner == "gateway/bar" and .parent == null' >/dev/null \
  || fail "unexpected non-fork gh output"

export GH_ENTERPRISE_TOKEN="${forker_token}"
fork_json="$(cd /tmp/fork && gh repo view --json nameWithOwner,parent)"
echo "fork: ${fork_json}"
echo "${fork_json}" | jq -e '
  .nameWithOwner == "forker/bar" and
  .parent.name == "bar" and
  .parent.owner.login == "gateway" and
  (.parent.id | type) == "string" and
  (.parent.owner.id | type) == "string"
' >/dev/null || fail "unexpected fork gh output"

wrong_path_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: token ${forker_token}" \
  -H 'Content-Type: application/json' \
  -d '{"query":"query RepositoryInfo { repository(owner: \"forker\", name: \"bar\") { nameWithOwner } }"}' \
  https://git.example.test/graphql)"
[ "${wrong_path_status}" = "404" ] || fail "/graphql returned HTTP ${wrong_path_status}, want 404"

echo "Docker Compose E2E passed."

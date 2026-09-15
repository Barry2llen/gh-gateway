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

initial_content="$(printf '%s\n' '# gateway e2e' | base64 | tr -d '\n')"
initial_body="$(jq -nc --arg content "${initial_content}" '{message:"initialize main",content:$content,branch:"main"}')"
initial_status="$(curl -sS -o /tmp/create-main.json -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Content-Type: application/json' \
  -d "${initial_body}" \
  "${gitea_url}/repos/gateway/bar/contents/README.md")"
case "${initial_status}" in
  201|422) ;;
  *) cat /tmp/create-main.json >&2; fail "main initialization returned HTTP ${initial_status}" ;;
esac

branch_status="$(curl -sS -o /tmp/create-branch.json -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Content-Type: application/json' \
  -d '{"new_branch_name":"feature","old_branch_name":"main"}' \
  "${gitea_url}/repos/gateway/bar/branches")"
case "${branch_status}" in
  201|409|422) ;;
  *) cat /tmp/create-branch.json >&2; fail "feature branch creation returned HTTP ${branch_status}" ;;
esac

feature_content="$(printf '%s\n' 'feature branch change' | base64 | tr -d '\n')"
feature_body="$(jq -nc --arg content "${feature_content}" '{message:"add feature",content:$content,branch:"feature"}')"
feature_status="$(curl -sS -o /tmp/create-feature.json -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Content-Type: application/json' \
  -d "${feature_body}" \
  "${gitea_url}/repos/gateway/bar/contents/feature.txt")"
case "${feature_status}" in
  201|422) ;;
  *) cat /tmp/create-feature.json >&2; fail "feature commit creation returned HTTP ${feature_status}" ;;
esac

pr_status="$(curl -sS -o /tmp/create-pr.json -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Content-Type: application/json' \
  -d '{"title":"Gateway E2E PR","head":"feature","base":"main","body":"Created by the gh-gateway E2E suite."}' \
  "${gitea_url}/repos/gateway/bar/pulls")"
case "${pr_status}" in
  201|409) ;;
  *) cat /tmp/create-pr.json >&2; fail "pull request creation returned HTTP ${pr_status}" ;;
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
caddy_access_log="/caddy-data/access.log"
access_log_lines=0
if [ -f "${caddy_access_log}" ]; then
  access_log_lines="$(wc -l < "${caddy_access_log}")"
fi
authenticated_user_json="$(gh api user)"
echo "authenticated user: ${authenticated_user_json}"
echo "${authenticated_user_json}" | jq -e '.login == "gateway"' >/dev/null \
  || fail "unexpected authenticated user gh output"

attempts=0
until tail -n "+$((access_log_lines + 1))" "${caddy_access_log}" 2>/dev/null \
  | jq -s -e 'any(.[]; .request.method == "GET" and .request.uri == "/api/v3/user")' >/dev/null 2>&1; do
  attempts=$((attempts + 1))
  [ "${attempts}" -lt 30 ] || fail "Caddy access log did not record GET /api/v3/user"
  sleep 1
done
echo "authenticated user path: GET /api/v3/user"

wrong_user_path_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Accept: application/vnd.github+json' \
  https://git.example.test/user)"
[ "${wrong_user_path_status}" = "404" ] \
  || fail "/user returned HTTP ${wrong_user_path_status}, want 404"

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

git clone -q --branch feature "http://gateway:${gateway_token}@gitea:3000/gateway/bar.git" /tmp/pr
git -C /tmp/pr remote set-url origin https://git.example.test/gateway/bar.git
export GH_ENTERPRISE_TOKEN="${gateway_token}"
pr_json="$(cd /tmp/pr && gh pr view --json number,url,state)"
echo "pull request: ${pr_json}"
echo "${pr_json}" | jq -e '
  .number == 1 and
  .state == "OPEN" and
  .url == "https://git.example.test/gateway/bar/pulls/1"
' >/dev/null || fail "unexpected pull request gh output"

head_sha="$(git -C /tmp/pr rev-parse HEAD)"
commit_pulls_json="$(cd /tmp/pr && gh api -H "Accept: application/vnd.github+json" \
  "repos/gateway/bar/commits/${head_sha}/pulls")"
echo "commit pull requests: ${commit_pulls_json}"
echo "${commit_pulls_json}" | jq -e '
  length == 1 and
  .[0].number == 1 and
  .[0].state == "open" and
  .[0].html_url == "https://git.example.test/gateway/bar/pulls/1"
' >/dev/null || fail "unexpected commit pull request gh output"

main_sha="$(curl -fsS \
  -H "Authorization: token ${gateway_token}" \
  "${gitea_url}/repos/gateway/bar/branches/main" | jq -er '.commit.id')"
empty_commit_pulls_json="$(cd /tmp/pr && gh api -H "Accept: application/vnd.github+json" \
  "repos/gateway/bar/commits/${main_sha}/pulls")"
echo "unassociated commit pull requests: ${empty_commit_pulls_json}"
echo "${empty_commit_pulls_json}" | jq -e 'type == "array" and length == 0' >/dev/null \
  || fail "unexpected unassociated commit gh output"

wrong_path_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: token ${forker_token}" \
  -H 'Content-Type: application/json' \
  -d '{"query":"query RepositoryInfo { repository(owner: \"forker\", name: \"bar\") { nameWithOwner } }"}' \
  https://git.example.test/graphql)"
[ "${wrong_path_status}" = "404" ] || fail "/graphql returned HTTP ${wrong_path_status}, want 404"

wrong_rest_path_status="$(curl -sS -o /dev/null -w '%{http_code}' \
  -H "Authorization: token ${gateway_token}" \
  -H 'Accept: application/vnd.github+json' \
  "https://git.example.test/repos/gateway/bar/commits/${head_sha}/pulls")"
[ "${wrong_rest_path_status}" = "404" ] \
  || fail "/repos commit lookup returned HTTP ${wrong_rest_path_status}, want 404"

echo "Docker Compose E2E passed."

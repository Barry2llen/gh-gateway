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

collaborator_status="$(curl -sS -o /tmp/add-collaborator.json -w '%{http_code}' \
  -X PUT -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' \
  -d '{"permission":"write"}' "${gitea_url}/repos/gateway/bar/collaborators/forker")"
[ "${collaborator_status}" = "204" ] || fail "collaborator creation returned HTTP ${collaborator_status}"

issue_comment_status="$(curl -sS -o /tmp/create-issue-comment.json -w '%{http_code}' \
  -H "Authorization: token ${forker_token}" -H 'Content-Type: application/json' \
  -d '{"body":"Conversation feedback from forker."}' "${gitea_url}/repos/gateway/bar/issues/1/comments")"
[ "${issue_comment_status}" = "201" ] || fail "issue comment creation returned HTTP ${issue_comment_status}"

review_status="$(curl -sS -o /tmp/create-review.json -w '%{http_code}' \
  -H "Authorization: token ${forker_token}" -H 'Content-Type: application/json' \
  -d '{"event":"COMMENT","body":"Published review from forker.","comments":[{"path":"feature.txt","body":"Inline feedback from forker.","new_position":1}]}' \
  "${gitea_url}/repos/gateway/bar/pulls/1/reviews")"
[ "${review_status}" = "200" ] || { cat /tmp/create-review.json >&2; fail "review creation returned HTTP ${review_status}"; }

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

for status_spec in 'success ci/success' 'failure ci/failure' 'pending ci/pending'; do
  status_state="${status_spec%% *}"
  status_context="${status_spec#* }"
  status_body="$(jq -nc --arg state "${status_state}" --arg context "${status_context}" '{state:$state,context:$context,description:("E2E " + $state),target_url:("https://ci.example.test/" + $context)}')"
  create_commit_status="$(curl -sS -o /tmp/create-status.json -w '%{http_code}' \
    -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' -d "${status_body}" \
    "${gitea_url}/repos/gateway/bar/statuses/${head_sha}")"
  [ "${create_commit_status}" = "201" ] || { cat /tmp/create-status.json >&2; fail "commit status creation returned HTTP ${create_commit_status}"; }
done

expanded_pr_json="$(cd /tmp/pr && gh pr view 1 --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision)"
echo "expanded pull request: ${expanded_pr_json}"
echo "${expanded_pr_json}" | jq -e --arg sha "${head_sha}" '
  .number == 1 and .state == "OPEN" and .headRefName == "feature" and .headRefOid == $sha and
  .headRepository.nameWithOwner == "gateway/bar" and .headRepositoryOwner.login == "gateway" and
  (.mergeable == "UNKNOWN" or .mergeable == "MERGEABLE") and .mergeStateStatus == "UNKNOWN" and .reviewDecision == ""
' >/dev/null || fail "unexpected expanded pull request output"

conversation_json="$(gh api 'repos/gateway/bar/issues/1/comments?per_page=100&page=1')"
echo "conversation comments: ${conversation_json}"
echo "${conversation_json}" | jq -e 'length == 1 and .[0].user.login == "forker" and .[0].author_association == "COLLABORATOR"' >/dev/null \
  || fail "unexpected conversation comments output"

reviews_json="$(gh api 'repos/gateway/bar/pulls/1/reviews?per_page=100&page=1')"
echo "reviews: ${reviews_json}"
echo "${reviews_json}" | jq -e 'length == 1 and .[0].user.login == "forker" and .[0].state == "COMMENTED" and .[0].author_association == "COLLABORATOR"' >/dev/null \
  || fail "unexpected reviews output"

inline_json="$(gh api 'repos/gateway/bar/pulls/1/comments?per_page=100&page=1')"
echo "inline review comments: ${inline_json}"
echo "${inline_json}" | jq -e 'length == 1 and .[0].user.login == "forker" and .[0].path == "feature.txt" and .[0].pull_request_review_id == 1 and .[0].author_association == "COLLABORATOR"' >/dev/null \
  || fail "unexpected inline review comments output"

checks_json="$(gh -R gateway/bar pr checks 1 --json name,state,bucket,link,workflow,event,startedAt,completedAt)"
echo "checks 2.95.0: ${checks_json}"
echo "${checks_json}" | jq -e '[.[].bucket] | (map(select(. == "pass"))|length)==1 and (map(select(. == "fail"))|length)==1 and (map(select(. == "pending"))|length)==1' >/dev/null \
  || fail "unexpected gh 2.95.0 checks output"

gh_p1a=/opt/gh-2.100.0/bin/gh
"${gh_p1a}" --version
expanded_pr_210_json="$(cd /tmp/pr && "${gh_p1a}" pr view 1 --json number,url,state,mergedAt,closedAt,headRefName,headRefOid,headRepository,headRepositoryOwner,mergeable,mergeStateStatus,reviewDecision)"
echo "expanded pull request 2.100.0: ${expanded_pr_210_json}"
echo "${expanded_pr_210_json}" | jq -e --arg sha "${head_sha}" '.number == 1 and .headRefOid == $sha and .mergeStateStatus == "UNKNOWN"' >/dev/null \
  || fail "unexpected gh 2.100.0 expanded PR output"
checks_210_json="$(GH_DEBUG=api "${gh_p1a}" -R gateway/bar pr checks 1 --json name,state,bucket,link,workflow,event,startedAt,completedAt 2>/tmp/gh-2.100-checks-debug.log)"
echo "checks 2.100.0: ${checks_210_json}"
echo "${checks_210_json}" | jq -e 'length == 3' >/dev/null || fail "unexpected gh 2.100.0 checks output"
for operation in PullRequestByNumber PullRequest_fields PullRequest_fields2 PullRequestStatusChecks; do
  grep -q "${operation}" /tmp/gh-2.100-checks-debug.log || fail "gh 2.100.0 debug log missing ${operation}"
done
echo "checks 2.100.0 operations: PullRequestByNumber, PullRequest_fields, PullRequest_fields2, PullRequestStatusChecks"

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

watch_log_lines="$(wc -l < "${caddy_access_log}")"
set +e
PATH="/opt/gh-2.100.0/bin:${PATH}" python3 /opt/babysit-pr/gh_pr_watch.py --once --pr 1 --repo gateway/bar --state-file /tmp/p1a-watcher-state.json >/tmp/watcher-output.json 2>/tmp/watcher-error.txt
watcher_status="$?"
set -e
[ "${watcher_status}" -ne 0 ] || fail "babysit-pr unexpectedly completed despite unsupported Actions"
echo "babysit-pr --once stderr: $(tr '\n' ' ' </tmp/watcher-error.txt)"
attempts=0
until tail -n "+$((watch_log_lines + 1))" "${caddy_access_log}" | jq -s -e '
  any(.[]; .request.uri == "/api/v3/user") and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/issues/1/comments")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/pulls/1/reviews")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/pulls/1/comments")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs"))
' >/dev/null 2>&1; do attempts=$((attempts+1)); [ "${attempts}" -lt 30 ] || fail "watcher request sequence was not recorded"; sleep 1; done
tail -n "+$((watch_log_lines + 1))" "${caddy_access_log}" | jq -s -e 'all(.[]; (.request.uri | contains("/jobs") or contains("/logs") or contains("/rerun")) | not)' >/dev/null \
  || fail "watcher crossed beyond the first unsupported Actions request"
echo "babysit-pr --once reached unsupported P1-B Actions after all P1-A requests, as expected."

echo "Docker Compose E2E passed."

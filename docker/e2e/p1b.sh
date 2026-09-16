#!/bin/sh
set -eu

gateway_token="$1"
gitea_url="$2"
gh_p1b="$3"
caddy_access_log="$4"

fail() {
  echo "P1-B E2E failure: $*" >&2
  exit 1
}

line_count() {
  [ -f "$1" ] && wc -l < "$1" || echo 0
}

api_status="$(curl -sS -o /tmp/enable-actions.json -w '%{http_code}' \
  -X PATCH -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' \
  -d '{"has_actions":true}' "${gitea_url}/repos/gateway/bar")"
[ "${api_status}" = "200" ] || { cat /tmp/enable-actions.json >&2; fail "enable Actions returned HTTP ${api_status}"; }

attempts=0
until curl -fsS -H "Authorization: token ${gateway_token}" \
  "${gitea_url}/admin/actions/runners" \
  | jq -e 'any(.runners[]?; .status == "online")' >/dev/null; do
  attempts=$((attempts + 1))
  [ "${attempts}" -lt 120 ] || fail "act_runner did not become online"
  sleep 1
done

run_marker="$(date +%s)"
workflow_yaml="name: Gateway Failing Matrix
on:
  push:
    branches: [feature]
jobs:
  matrix:
    name: matrix
    runs-on: gh-gateway-e2e
    strategy:
      fail-fast: false
      matrix:
        case: [one, two]
    steps:
      - name: fail predictably
        run: |
          echo GH_GATEWAY_E2E_\${{ matrix.case }}
          echo run-${run_marker}
          exit 1
"
workflow_content="$(printf '%s' "${workflow_yaml}" | base64 | tr -d '\n')"
write_workflow() {
  branch="$1"
  workflow_body="$(jq -nc --arg content "${workflow_content}" --arg branch "${branch}" '{message:("add deterministic failing workflow on " + $branch),content:$content,branch:$branch}')"
  workflow_status="$(curl -sS -o "/tmp/create-workflow-${branch}.json" -w '%{http_code}' \
    -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' \
    -d "${workflow_body}" "${gitea_url}/repos/gateway/bar/contents/.gitea/workflows/failing.yml")"
  if [ "${workflow_status}" = "422" ]; then
    workflow_sha="$(curl -fsS -H "Authorization: token ${gateway_token}" \
      "${gitea_url}/repos/gateway/bar/contents/.gitea/workflows/failing.yml?ref=${branch}" | jq -er '.sha')"
    workflow_body="$(jq -nc --arg content "${workflow_content}" --arg branch "${branch}" --arg sha "${workflow_sha}" '{message:("refresh deterministic failing workflow on " + $branch),content:$content,branch:$branch,sha:$sha}')"
    workflow_status="$(curl -sS -o "/tmp/update-workflow-${branch}.json" -w '%{http_code}' \
      -X PUT -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' \
      -d "${workflow_body}" "${gitea_url}/repos/gateway/bar/contents/.gitea/workflows/failing.yml")"
  fi
  [ "${workflow_status}" = "201" ] || fail "workflow write on ${branch} returned HTTP ${workflow_status}"
}

# Gitea resolves workflow metadata from the default branch. Keep the same file there,
# then create the feature-branch commit that actually triggers this workflow.
write_workflow main
write_workflow feature

head_sha="$(curl -fsS -H "Authorization: token ${gateway_token}" \
  "${gitea_url}/repos/gateway/bar/branches/feature" | jq -er '.commit.id')"

attempts=0
real_run_json=""
until real_run_json="$(curl -fsS -H "Authorization: token ${gateway_token}" \
  "${gitea_url}/repos/gateway/bar/actions/runs?head_sha=${head_sha}&page=1&limit=100")" \
  && echo "${real_run_json}" | jq -e 'any(.workflow_runs[]?; .status == "completed" and .conclusion == "failure")' >/dev/null; do
  attempts=$((attempts + 1))
  [ "${attempts}" -lt 180 ] || fail "real Gitea workflow did not complete with failure"
  sleep 1
done
real_run_id="$(echo "${real_run_json}" | jq -er '.workflow_runs[] | select(.status == "completed" and .conclusion == "failure") | .id' | head -n 1)"

for status_spec in 'success ci/success' 'failure ci/failure'; do
  status_state="${status_spec%% *}"
  status_context="${status_spec#* }"
  status_body="$(jq -nc --arg state "${status_state}" --arg context "${status_context}" '{state:$state,context:$context,description:("P1-B " + $state)}')"
  status_code="$(curl -sS -o /tmp/p1b-status.json -w '%{http_code}' \
    -H "Authorization: token ${gateway_token}" -H 'Content-Type: application/json' -d "${status_body}" \
    "${gitea_url}/repos/gateway/bar/statuses/${head_sha}")"
  [ "${status_code}" = "201" ] || fail "terminal commit status returned HTTP ${status_code}"
done

export GH_ENTERPRISE_TOKEN="${gateway_token}"
export GH_PROMPT_DISABLED=1

# Stable protocol E2E: real watcher + real gh + real Gateway, fixture only at the Actions provider boundary.
export GH_HOST=fixture.example.test
fixture_lines="$(line_count /state/actions-fixture.log)"
protocol_runs_json="$("${gh_p1b}" api repos/gateway/bar/actions/runs -X GET -f head_sha="${head_sha}" -f per_page=100)"
echo "${protocol_runs_json}" | jq -e '.workflow_runs | length == 5 and any(.[]; .id == 101 and .conclusion == "failure")' >/dev/null \
  || fail "unexpected protocol runs response"
protocol_jobs_json="$("${gh_p1b}" api repos/gateway/bar/actions/runs/101/jobs -X GET -f per_page=100)"
echo "${protocol_jobs_json}" | jq -e '.jobs | length == 2 and (.[0].id != .[1].id) and (.[0].name == .[1].name)' >/dev/null \
  || fail "protocol matrix jobs were not preserved"
protocol_log="$("${gh_p1b}" api repos/gateway/bar/actions/jobs/201/logs)"
[ "${protocol_log}" = "fixture matrix one failed" ] || fail "unexpected protocol job log"

protocol_watch_log_lines="$(line_count "${caddy_access_log}")"
PATH="/opt/gh-2.100.0/bin:${PATH}" python3 /opt/babysit-pr/gh_pr_watch.py \
  --once --pr 1 --repo gateway/bar --state-file /tmp/p1b-protocol-once-state.json \
  >/tmp/p1b-protocol-once.json
jq -e '
  .checks.all_terminal == true and .checks.failed_count >= 1 and
  any(.failed_runs[]; .run_id == 101 and .conclusion == "failure") and
  ([.failed_jobs[].job_id] | sort == [201,202]) and
  (.actions | index("diagnose_ci_failure") != null) and
  (.actions | index("retry_failed_checks") != null)
' /tmp/p1b-protocol-once.json >/dev/null || fail "protocol watcher snapshot was incomplete"

protocol_trace="$(tail -n "+$((protocol_watch_log_lines + 1))" "${caddy_access_log}")"
echo "${protocol_trace}" | jq -s -e '
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs?")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs/101/jobs?")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs/104/jobs?")) and
  all(.[]; ((.request.uri | contains("/actions/runs/102/jobs")) or (.request.uri | contains("/actions/runs/103/jobs")) or (.request.uri | contains("/actions/runs/105/jobs")) or (.request.uri | contains("/logs")) or (.request.uri | contains("/rerun-failed-jobs"))) | not)
' >/dev/null || fail "protocol --once request trace was incorrect"

retry_log_lines="$(line_count "${caddy_access_log}")"
set +e
PATH="/opt/gh-2.100.0/bin:${PATH}" python3 /opt/babysit-pr/gh_pr_watch.py \
  --retry-failed-now --pr 1 --repo gateway/bar --state-file /tmp/p1b-protocol-retry-state.json \
  >/tmp/p1b-protocol-retry.json 2>/tmp/p1b-protocol-retry.err
protocol_retry_status="$?"
set -e
[ "${protocol_retry_status}" -ne 0 ] || fail "unsupported protocol rerun unexpectedly succeeded"
grep -q 'Failed-only workflow rerun is not supported' /tmp/p1b-protocol-retry.err \
  || fail "protocol rerun did not expose the compatibility boundary"
retry_trace="$(tail -n "+$((retry_log_lines + 1))" "${caddy_access_log}")"
echo "${retry_trace}" | jq -s -e '
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs/101?exclude_pull_requests=true")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/workflows/")) and
  any(.[]; .request.method == "POST" and (.request.uri | endswith("/actions/runs/101/rerun-failed-jobs")))
' >/dev/null || fail "protocol retry trace did not reach the unsupported POST"
tail -n "+$((fixture_lines + 1))" /state/actions-fixture.log | jq -s -e 'all(.[]; .method == "GET")' >/dev/null \
  || fail "protocol fixture received an Actions mutation"

# Standalone gh compatibility fixture: 201 must contain valid JSON.
export GH_HOST=rerun-success.example.test
"${gh_p1b}" -R compat/repo run rerun 101 --failed
set +e
"${gh_p1b}" -R compat/repo run rerun 102 --failed >/tmp/rerun-empty.out 2>/tmp/rerun-empty.err
empty_status="$?"
set -e
[ "${empty_status}" -ne 0 ] && grep -q 'unexpected end of JSON input' /tmp/rerun-empty.err \
  || fail "gh did not preserve the 201 empty-body incompatibility"

# Full real-Gitea Actions E2E.
export GH_HOST=git.example.test
gateway_runs_json="$("${gh_p1b}" api repos/gateway/bar/actions/runs -X GET -f head_sha="${head_sha}" -f per_page=100)"
echo "${gateway_runs_json}" | jq -e --argjson run_id "${real_run_id}" 'any(.workflow_runs[]; .id == $run_id and .status == "completed" and .conclusion == "failure")' >/dev/null \
  || fail "Gateway did not expose the real failed Gitea run"
gateway_jobs_json="$("${gh_p1b}" api "repos/gateway/bar/actions/runs/${real_run_id}/jobs" -X GET -f per_page=100)"
echo "${gateway_jobs_json}" | jq -e '[.jobs[] | select(.conclusion == "failure")] as $failed | ($failed|length) == 2 and ($failed[0].id != $failed[1].id)' >/dev/null \
  || fail "Gateway did not expose both real matrix job IDs"
real_job_id="$(echo "${gateway_jobs_json}" | jq -er '.jobs[] | select(.conclusion == "failure") | .id' | head -n 1)"
"${gh_p1b}" api "repos/gateway/bar/actions/jobs/${real_job_id}/logs" >/tmp/p1b-real-job.log
grep -q 'GH_GATEWAY_E2E_' /tmp/p1b-real-job.log || fail "real Gitea job log marker was missing"

real_watch_lines="$(line_count "${caddy_access_log}")"
PATH="/opt/gh-2.100.0/bin:${PATH}" python3 /opt/babysit-pr/gh_pr_watch.py \
  --once --pr 1 --repo gateway/bar --state-file /tmp/p1b-real-once-state.json \
  >/tmp/p1b-real-once.json
jq -e --argjson run_id "${real_run_id}" '
  .checks.all_terminal == true and .checks.failed_count >= 1 and
  any(.failed_runs[]; .run_id == $run_id and .conclusion == "failure") and
  ([.failed_jobs[].job_id] | unique | length) >= 2 and
  (.actions | index("diagnose_ci_failure") != null) and
  (.actions | index("retry_failed_checks") != null)
' /tmp/p1b-real-once.json >/dev/null || fail "real-Gitea watcher snapshot was incomplete"
real_once_trace="$(tail -n "+$((real_watch_lines + 1))" "${caddy_access_log}")"
echo "${real_once_trace}" | jq -s -e 'all(.[]; ((.request.uri | contains("/logs")) or (.request.uri | contains("/rerun-failed-jobs"))) | not)' >/dev/null \
  || fail "real-Gitea --once requested logs or rerun"

real_state_before="$(curl -fsS -H "Authorization: token ${gateway_token}" "${gitea_url}/repos/gateway/bar/actions/runs/${real_run_id}" | jq -S '{id,status,conclusion,started_at,completed_at}')"
real_jobs_before="$(curl -fsS -H "Authorization: token ${gateway_token}" "${gitea_url}/repos/gateway/bar/actions/runs/${real_run_id}/jobs?page=1&limit=100" | jq -S '[.jobs[] | {id,status,conclusion}]')"
real_retry_lines="$(line_count "${caddy_access_log}")"
set +e
PATH="/opt/gh-2.100.0/bin:${PATH}" python3 /opt/babysit-pr/gh_pr_watch.py \
  --retry-failed-now --pr 1 --repo gateway/bar --state-file /tmp/p1b-real-retry-state.json \
  >/tmp/p1b-real-retry.json 2>/tmp/p1b-real-retry.err
real_retry_status="$?"
set -e
[ "${real_retry_status}" -ne 0 ] || fail "real-Gitea unsupported rerun unexpectedly succeeded"
grep -q 'Failed-only workflow rerun is not supported' /tmp/p1b-real-retry.err \
  || fail "real-Gitea retry did not expose the compatibility boundary"
real_retry_trace="$(tail -n "+$((real_retry_lines + 1))" "${caddy_access_log}")"
echo "${real_retry_trace}" | jq -s -e --arg run_id "${real_run_id}" '
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/runs/" + $run_id + "?exclude_pull_requests=true")) and
  any(.[]; .request.uri | startswith("/api/v3/repos/gateway/bar/actions/workflows/")) and
  any(.[]; .request.method == "POST" and (.request.uri | endswith("/actions/runs/" + $run_id + "/rerun-failed-jobs")))
' >/dev/null || fail "real-Gitea retry trace did not reach the unsupported POST"
real_state_after="$(curl -fsS -H "Authorization: token ${gateway_token}" "${gitea_url}/repos/gateway/bar/actions/runs/${real_run_id}" | jq -S '{id,status,conclusion,started_at,completed_at}')"
real_jobs_after="$(curl -fsS -H "Authorization: token ${gateway_token}" "${gitea_url}/repos/gateway/bar/actions/runs/${real_run_id}/jobs?page=1&limit=100" | jq -S '[.jobs[] | {id,status,conclusion}]')"
[ "${real_state_before}" = "${real_state_after}" ] && [ "${real_jobs_before}" = "${real_jobs_after}" ] \
  || fail "real Gitea workflow state changed after unsupported rerun"

echo "P1-B protocol E2E passed."
echo "P1-B real-Gitea Actions E2E passed."
echo "P1-B failed-only retry compatibility: UNSUPPORTED at the explicit Gateway boundary."

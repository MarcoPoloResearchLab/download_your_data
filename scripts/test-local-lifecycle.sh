#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 2 ]] || {
  echo "usage: scripts/test-local-lifecycle.sh <lifecycle-script> <absolute-binary-path>" >&2
  exit 2
}

readonly lifecycle_script="$1"
readonly binary_path="$2"
readonly repository_root="$(cd "$(dirname "${lifecycle_script}")/.." && pwd)"
readonly working_directory="$(mktemp -d -t download-your-data-lifecycle.XXXXXX)"
readonly state_directory="${working_directory}/state"
readonly data_directory="${working_directory}/data"
readonly server_log="${working_directory}/server.log"
readonly state_file="${state_directory}/download-your-data-server.state"
readonly local_environment="${working_directory}/.env"
readonly port_pair="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as application_listener:
    with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as gateway_listener:
        application_listener.bind(("127.0.0.1", 0))
        gateway_listener.bind(("127.0.0.1", 0))
        print(
            application_listener.getsockname()[1],
            gateway_listener.getsockname()[1],
        )
PY
)"
readonly application_port="${port_pair%% *}"
readonly gateway_port="${port_pair##* }"
readonly address="127.0.0.1:${application_port}"
readonly application_origin="http://${address}"
readonly gateway_address="127.0.0.1:${gateway_port}"
readonly base_url="http://${gateway_address}"
readonly environment_execution_sentinel="${working_directory}/environment-executed"
readonly signing_key_marker="GENERATE_ON_FIRST_MAKE_UP"
readonly docker_log="${working_directory}/docker.log"
readonly gateway_log="${working_directory}/gateway.log"
readonly gateway_pid_file="${working_directory}/gateway.pid"

export PATH="${repository_root}/scripts/test-fixtures/local-stack:${PATH}"
export DOWNLOAD_YOUR_DATA_TEST_DOCKER_LOG="${docker_log}"
export DOWNLOAD_YOUR_DATA_TEST_GATEWAY_ADDRESS="${gateway_address}"
export DOWNLOAD_YOUR_DATA_TEST_GATEWAY_LOG="${gateway_log}"
export DOWNLOAD_YOUR_DATA_TEST_GATEWAY_PID_FILE="${gateway_pid_file}"
export DOWNLOAD_YOUR_DATA_TEST_GATEWAY_SCRIPT="${repository_root}/scripts/test-fixtures/local-stack/gateway.py"
export DOWNLOAD_YOUR_DATA_TEST_APP_ORIGIN="${application_origin}"

grep -F "image: ghcr.io/tyemirov/tauth:latest" \
  "${repository_root}/configs/docker-compose.local.yml" >/dev/null
grep -F "image: ghcr.io/tyemirov/ghttp:latest" \
  "${repository_root}/configs/docker-compose.local.yml" >/dev/null
grep -F "/auth=http://tauth:8080" \
  "${repository_root}/configs/docker-compose.local.yml" >/dev/null
grep -F "/=\${DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM-}" \
  "${repository_root}/configs/docker-compose.local.yml" >/dev/null
grep -F 'tenant_origins:' \
  "${repository_root}/configs/tauth.local.yml" >/dev/null
grep -F 'allow_insecure_http: true' \
  "${repository_root}/configs/tauth.local.yml" >/dev/null

{
  printf '%s\n' \
    "DOWNLOAD_YOUR_DATA_ADDRESS=${address}" \
    "DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM=http://host.docker.internal:${application_port}" \
    "DOWNLOAD_YOUR_DATA_DATA_DIR=${data_directory}" \
    "DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN=${base_url}" \
    "DOWNLOAD_YOUR_DATA_API_ORIGIN=${base_url}" \
    "DOWNLOAD_YOUR_DATA_TAUTH_URL=${base_url}" \
    "DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID=download-your-data-test" \
    "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=${signing_key_marker}" \
    "DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME=app_session_dyd_test" \
    "DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME=app_refresh_dyd_test" \
    "DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=\$(touch ${environment_execution_sentinel}).apps.googleusercontent.com"
} >"${local_environment}"
chmod 644 "${local_environment}"

server_wrapper_pid=""
unrelated_pid=""

assert_environment_rejected() {
  local environment_path="$1"
  local expected_error="$2"
  local rejection_log="${environment_path}.log"

  if "${lifecycle_script}" up "${binary_path}" "${state_directory}" \
    "${environment_path}" >"${rejection_log}" 2>&1; then
    echo "local server unexpectedly accepted ${environment_path}" >&2
    exit 1
  fi
  if ! grep -F "${expected_error}" "${rejection_log}" >/dev/null; then
    sed -n '1,120p' "${rejection_log}" >&2
    exit 1
  fi
  [[ ! -e "${state_file}" ]]
}

cleanup() {
  "${lifecycle_script}" down "${binary_path}" "${state_directory}" >/dev/null 2>&1 || true
  if [[ -n "${server_wrapper_pid}" ]]; then
    kill "${server_wrapper_pid}" >/dev/null 2>&1 || true
    wait "${server_wrapper_pid}" >/dev/null 2>&1 || true
  fi
  if [[ -n "${unrelated_pid}" ]]; then
    kill "${unrelated_pid}" >/dev/null 2>&1 || true
    wait "${unrelated_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${working_directory}"
}
trap cleanup EXIT

assert_environment_rejected \
  "${working_directory}/missing.env" \
  "missing private local environment"

grep -v '^DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=' \
  "${local_environment}" >"${working_directory}/missing-required.env"
assert_environment_rejected \
  "${working_directory}/missing-required.env" \
  "must define DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID"

grep -v '^DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM=' \
  "${local_environment}" >"${working_directory}/missing-upstream.env"
assert_environment_rejected \
  "${working_directory}/missing-upstream.env" \
  "must define DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM"

sed "s#^DOWNLOAD_YOUR_DATA_API_ORIGIN=.*#DOWNLOAD_YOUR_DATA_API_ORIGIN=http://127.0.0.1:1#" \
  "${local_environment}" >"${working_directory}/split-origin.env"
assert_environment_rejected \
  "${working_directory}/split-origin.env" \
  "must use one same-origin front door"

cp "${local_environment}" "${working_directory}/duplicate.env"
printf '%s\n' "DOWNLOAD_YOUR_DATA_API_ORIGIN=${base_url}" \
  >>"${working_directory}/duplicate.env"
assert_environment_rejected \
  "${working_directory}/duplicate.env" \
  "duplicates DOWNLOAD_YOUR_DATA_API_ORIGIN"

cp "${local_environment}" "${working_directory}/unsupported.env"
printf '%s\n' "UNRELATED_APPLICATION_VALUE=not-allowed" \
  >>"${working_directory}/unsupported.env"
assert_environment_rejected \
  "${working_directory}/unsupported.env" \
  "defines unsupported variable UNRELATED_APPLICATION_VALUE"

cp "${local_environment}" "${working_directory}/malformed.env"
printf '%s\n' "DOWNLOAD_YOUR_DATA_MALFORMED" \
  >>"${working_directory}/malformed.env"
assert_environment_rejected \
  "${working_directory}/malformed.env" \
  "must use NAME=VALUE"

sed 's/^DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=.*/DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=/' \
  "${local_environment}" >"${working_directory}/empty.env"
assert_environment_rejected \
  "${working_directory}/empty.env" \
  "leaves DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID empty"

cp "${local_environment}" "${working_directory}/duplicate-marker.env"
printf '%s\n' \
  "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=${signing_key_marker}" \
  >>"${working_directory}/duplicate-marker.env"
assert_environment_rejected \
  "${working_directory}/duplicate-marker.env" \
  "contains more than one signing-key bootstrap marker"

DOWNLOAD_YOUR_DATA_ADDRESS="127.0.0.1:1" \
  "${lifecycle_script}" up "${binary_path}" "${state_directory}" \
  "${local_environment}" \
  >"${server_log}" 2>&1 &
server_wrapper_pid="$!"

for _ in $(seq 1 100); do
  if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1 &&
    grep -q "Local stack ready at ${base_url}" "${server_log}"; then
    break
  fi
  if ! kill -0 "${server_wrapper_pid}" >/dev/null 2>&1; then
    sed -n '1,160p' "${server_log}" >&2
    exit 1
  fi
  sleep 0.1
done

health="$(curl --fail --silent --show-error "${base_url}/api/health")"
grep -q '"status":"ready"' <<<"${health}"
grep -q "Local stack ready at ${base_url}" "${server_log}"
[[ -f "${state_file}" ]]
[[ ! -e "${environment_execution_sentinel}" ]]
! grep -F "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=${signing_key_marker}" \
  "${local_environment}" >/dev/null
generated_signing_key="$(
  sed -n 's/^DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=//p' \
    "${local_environment}"
)"
[[ "${#generated_signing_key}" -eq 96 ]]
environment_mode="$(
  stat -f '%Lp' "${local_environment}" 2>/dev/null ||
    stat -c '%a' "${local_environment}"
)"
[[ "${environment_mode}" == "600" ]]
grep -F "config --quiet" "${docker_log}" >/dev/null
grep -F "up --detach --wait tauth" "${docker_log}" >/dev/null
grep -F "up --detach --no-deps gateway" "${docker_log}" >/dev/null

if "${lifecycle_script}" up "${binary_path}" "${state_directory}" \
  "${local_environment}" \
  >"${working_directory}/duplicate.log" 2>&1; then
  echo "duplicate local server start unexpectedly succeeded" >&2
  exit 1
fi
grep -q 'already running' "${working_directory}/duplicate.log"

"${lifecycle_script}" down "${binary_path}" "${state_directory}"
if ! wait "${server_wrapper_pid}"; then
  sed -n '1,160p' "${server_log}" >&2
  exit 1
fi
server_wrapper_pid=""

[[ ! -e "${state_file}" ]]
[[ ! -e "${gateway_pid_file}" ]]
grep -F "down --remove-orphans" "${docker_log}" >/dev/null
if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1; then
  echo "local server remained reachable after down" >&2
  exit 1
fi

rm -f "${local_environment}"
"${lifecycle_script}" down "${binary_path}" "${state_directory}" \
  "${local_environment}" \
  >"${working_directory}/already-down.log"
grep -q 'not running' "${working_directory}/already-down.log"

mkdir -p "${state_directory}"
sleep 30 &
unrelated_pid="$!"
unrelated_start="$(
  LC_ALL=C ps -p "${unrelated_pid}" -o lstart= |
    sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
)"
printf '%s\n%s\n' "${unrelated_pid}" "${unrelated_start}" >"${state_file}"

if "${lifecycle_script}" down "${binary_path}" "${state_directory}" \
  >"${working_directory}/ownership-mismatch.log" 2>&1; then
  echo "down unexpectedly accepted an unrelated process" >&2
  exit 1
fi
grep -q 'no process was stopped' "${working_directory}/ownership-mismatch.log"
kill -0 "${unrelated_pid}"

kill "${unrelated_pid}"
wait "${unrelated_pid}" >/dev/null 2>&1 || true
unrelated_pid=""
rm -f "${state_file}"

echo "Local server lifecycle test passed."

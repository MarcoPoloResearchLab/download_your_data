#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 2 ]] || {
  echo "usage: scripts/test-local-lifecycle.sh <lifecycle-script> <absolute-binary-path>" >&2
  exit 2
}

readonly lifecycle_script="$1"
readonly binary_path="$2"
readonly working_directory="$(mktemp -d -t download-your-data-lifecycle.XXXXXX)"
readonly state_directory="${working_directory}/state"
readonly data_directory="${working_directory}/data"
readonly server_log="${working_directory}/server.log"
readonly state_file="${state_directory}/download-your-data-server.state"
readonly port="$(python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.bind(("127.0.0.1", 0))
    print(listener.getsockname()[1])
PY
)"
readonly address="127.0.0.1:${port}"
readonly base_url="http://${address}"

server_wrapper_pid=""
unrelated_pid=""

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

DOWNLOAD_YOUR_DATA_ADDRESS="${address}" \
DOWNLOAD_YOUR_DATA_DATA_DIR="${data_directory}" \
  "${lifecycle_script}" up "${binary_path}" "${state_directory}" \
  >"${server_log}" 2>&1 &
server_wrapper_pid="$!"

for _ in $(seq 1 100); do
  if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1; then
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
[[ -f "${state_file}" ]]

if DOWNLOAD_YOUR_DATA_ADDRESS="${address}" \
  DOWNLOAD_YOUR_DATA_DATA_DIR="${data_directory}" \
  "${lifecycle_script}" up "${binary_path}" "${state_directory}" \
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
if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1; then
  echo "local server remained reachable after down" >&2
  exit 1
fi

"${lifecycle_script}" down "${binary_path}" "${state_directory}" \
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

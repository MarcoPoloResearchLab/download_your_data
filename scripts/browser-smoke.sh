#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 1 ]] || {
  echo "usage: scripts/browser-smoke.sh <download-your-data-binary>" >&2
  exit 2
}

readonly binary_path="$1"
if [[ -n "${DOWNLOAD_YOUR_DATA_BROWSER_TEST_ADDRESS:-}" ]]; then
  address="${DOWNLOAD_YOUR_DATA_BROWSER_TEST_ADDRESS}"
else
  address="$(
    python3 - <<'PY'
import socket

with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{listener.getsockname()[1]}")
PY
  )"
fi
readonly address
readonly base_url="http://${address}"
readonly session_name="download-your-data-ci-$$"
readonly playwright_version="${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION is required}"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly repository_directory="$(cd "${script_directory}/.." && pwd)"
readonly valid_csv="${repository_directory}/internal/providers/netflix/testdata/viewing_activity.csv"
readonly invalid_csv="${repository_directory}/internal/providers/netflix/testdata/invalid_viewing_activity.csv"
readonly server_log="$(mktemp -t download-your-data-server.XXXXXX.log)"
readonly data_directory="$(mktemp -d -t download-your-data-data.XXXXXX)"

[[ -x "${binary_path}" ]] || {
  echo "browser smoke binary is not executable: ${binary_path}" >&2
  exit 1
}

server_pid=""

run_playwright() {
  npx --yes --package "@playwright/cli@${playwright_version}" \
    playwright-cli "-s=${session_name}" "$@"
}

cleanup() {
  run_playwright close >/dev/null 2>&1 || true
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" >/dev/null 2>&1 || true
  fi
  rm -f "${server_log}"
  rm -rf "${data_directory}"
}
trap cleanup EXIT

DOWNLOAD_YOUR_DATA_ADDRESS="${address}" \
DOWNLOAD_YOUR_DATA_DATA_DIR="${data_directory}" \
"${binary_path}" serve >"${server_log}" 2>&1 &
server_pid=$!

for _ in $(seq 1 100); do
  if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1; then
    break
  fi
  if ! kill -0 "${server_pid}" >/dev/null 2>&1; then
    sed -n '1,160p' "${server_log}" >&2
    exit 1
  fi
  sleep 0.1
done

curl --fail --silent --show-error "${base_url}/api/health" >/dev/null

scenario="$(<"${script_directory}/browser-smoke.playwright.js")"
scenario="${scenario/__BASE_URL__/${base_url}}"
scenario="${scenario/__VALID_CSV__/${valid_csv}}"
scenario="${scenario/__INVALID_CSV__/${invalid_csv}}"

run_playwright open about:blank >/dev/null
scenario_output="$(run_playwright run-code "${scenario}")"
if [[ "${scenario_output}" == *"### Error"* ]]; then
  printf '%s\n' "${scenario_output}" >&2
  exit 1
fi

echo "Browser smoke test passed at ${base_url}"

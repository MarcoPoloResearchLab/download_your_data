#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 1 ]] || { echo 'usage: scripts/password-merger-browser.sh <download-your-data-binary>' >&2; exit 2; }
readonly binary_path="$1"
readonly playwright_version="${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION is required}"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly address="$(python3 - <<'PY'
import socket
with socket.socket(socket.AF_INET, socket.SOCK_STREAM) as listener:
    listener.bind(("127.0.0.1", 0))
    print(f"127.0.0.1:{listener.getsockname()[1]}")
PY
)"
readonly base_url="http://${address}"
readonly session_name="password-merger-ci-$$"
readonly work_directory="$(mktemp -d -t password-merger-browser.XXXXXX)"
readonly data_directory="$(mktemp -d -t password-merger-data.XXXXXX)"
server_pid=""
run_playwright() {
  (cd "${work_directory}"; npx --yes --package "@playwright/cli@${playwright_version}" playwright-cli "-s=${session_name}" "$@")
}
cleanup() {
  run_playwright close >/dev/null 2>&1 || true
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${work_directory}" "${data_directory}"
}
trap cleanup EXIT
DOWNLOAD_YOUR_DATA_ADDRESS="${address}" \
DOWNLOAD_YOUR_DATA_DATA_DIR="${data_directory}" \
DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN="${base_url}" \
DOWNLOAD_YOUR_DATA_API_ORIGIN="${base_url}" \
DOWNLOAD_YOUR_DATA_TAUTH_URL="${base_url}" \
DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID="password-merger-browser" \
DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY="password-merger-browser-synthetic-signing-key" \
DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME="password_merger_session" \
DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME="password_merger_refresh" \
DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID="test.apps.googleusercontent.com" \
"${binary_path}" serve >"${work_directory}/server.log" 2>&1 &
server_pid=$!
for _ in $(seq 1 100); do
  if curl --fail --silent "${base_url}/api/health" >/dev/null 2>&1; then break; fi
  if ! kill -0 "${server_pid}" >/dev/null 2>&1; then cat "${work_directory}/server.log" >&2; exit 1; fi
  sleep 0.1
done
curl --fail --silent --show-error "${base_url}/api/health" >/dev/null
scenario="$(<"${script_directory}/password-merger-browser.playwright.js")"
scenario="${scenario//__BASE_URL__/${base_url}}"
readonly fixture_directory="${script_directory}/../testdata/password-merger"
scenario="${scenario//__FIXTURE_DIR__/${fixture_directory}}"
run_playwright open about:blank >/dev/null
scenario_output="$(run_playwright run-code "${scenario}")"
if [[ "${scenario_output}" == *"### Error"* ]]; then printf '%s\n' "${scenario_output}" >&2; exit 1; fi
echo "Password merger browser test passed at ${base_url}"

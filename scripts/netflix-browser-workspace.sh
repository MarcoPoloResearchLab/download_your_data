#!/usr/bin/env bash
set -euo pipefail

readonly base_url="${DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL:?DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL is required}"
readonly viewing_csv="${DOWNLOAD_YOUR_DATA_BROWSER_CSV:?DOWNLOAD_YOUR_DATA_BROWSER_CSV is required}"
readonly playwright_version="${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION is required}"
readonly session_name="download-your-data-netflix-$$"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"

run_playwright() {
  npx --yes --package "@playwright/cli@${playwright_version}" \
    playwright-cli "-s=${session_name}" "$@"
}

cleanup() {
  run_playwright close >/dev/null 2>&1 || true
}
trap cleanup EXIT

curl --fail --silent --show-error "${base_url}/api/health" >/dev/null

scenario="$(<"${script_directory}/netflix-browser-workspace.playwright.js")"
scenario="${scenario/__BASE_URL__/${base_url}}"
scenario="${scenario/__VIEWING_CSV__/${viewing_csv}}"

run_playwright open about:blank >/dev/null
scenario_output="$(run_playwright run-code "${scenario}")"
if [[ "${scenario_output}" == *"### Error"* ]]; then
  printf '%s\n' "${scenario_output}" >&2
  exit 1
fi

echo "Deterministic Netflix browser lifecycle passed at ${base_url}"

#!/usr/bin/env bash
set -euo pipefail
readonly base_url="${DOWNLOAD_YOUR_DATA_BROWSER_BASE_URL:?}"
readonly viewing_csv="${DOWNLOAD_YOUR_DATA_BROWSER_CSV:?}"
readonly prime_zip="${DOWNLOAD_YOUR_DATA_BROWSER_PRIME:?}"
readonly session_cookie="${DOWNLOAD_YOUR_DATA_BROWSER_SESSION_COOKIE:?}"
readonly session_token="${DOWNLOAD_YOUR_DATA_BROWSER_SESSION_TOKEN:?}"
readonly playwright_version="${PLAYWRIGHT_CLI_VERSION:?}"
readonly session_name="download-your-data-media-$$"
readonly script_directory="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
readonly playwright_directory="$(mktemp -d -t download-your-data-media.XXXXXX)"
readonly screenshot_root="${script_directory}/../output/playwright"
mkdir -p "${screenshot_root}"
run_playwright() {
  (cd "${playwright_directory}"; npx --yes --package "@playwright/cli@${playwright_version}" playwright-cli "-s=${session_name}" "$@")
}
cleanup() {
  run_playwright close >/dev/null 2>&1 || true
  rm -rf "${playwright_directory}"
}
trap cleanup EXIT
scenario="$(<"${script_directory}/media-browser-workspace.playwright.js")"
scenario="${scenario/__BASE_URL__/${base_url}}"
scenario="${scenario/__VIEWING_CSV__/${viewing_csv}}"
scenario="${scenario//__PRIME_ZIP__/${prime_zip}}"
scenario="${scenario/__SESSION_COOKIE__/${session_cookie}}"
scenario="${scenario/__SESSION_TOKEN__/${session_token}}"
scenario="${scenario//__SCREENSHOT_ROOT__/${screenshot_root}}"
boundary="$(<"${script_directory}/shared-ui-boundary.js")"
boundary="${boundary//__BASE_URL__/${base_url}}"
scenario="${scenario/async page => \{/async page => \{${boundary}}"
run_playwright open about:blank >/dev/null
output="$(run_playwright run-code "${scenario}")"
if [[ "${output}" == *"### Error"* ]]; then printf '%s\n' "${output}" >&2; exit 1; fi
echo "Shared viewing history browser contract passed"

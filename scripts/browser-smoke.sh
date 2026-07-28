#!/usr/bin/env bash
set -euo pipefail

readonly address="${DOWNLOAD_YOUR_DATA_BROWSER_TEST_ADDRESS:-127.0.0.1:18787}"
readonly base_url="http://${address}"
readonly session_name="download-your-data-ci-$$"
readonly playwright_version="${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION is required}"
readonly server_log="$(mktemp -t download-your-data-server.XXXXXX.log)"
readonly data_directory="$(mktemp -d -t download-your-data-data.XXXXXX)"

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
go run . >"${server_log}" 2>&1 &
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
run_playwright open "${base_url}" >/dev/null
run_playwright resize 1440 1000 >/dev/null

assert_locale() {
  local locale="$1"
  local expected_text="$2"
  run_playwright click "button[data-lang=\"${locale}\"]" >/dev/null
  local snapshot
  snapshot="$(run_playwright snapshot)"
  grep -Fq "${expected_text}" <<<"${snapshot}"
  assert_direct_routes
}

assert_direct_routes() {
  run_playwright run-code 'async page => {
    const directRoutes = [
      ["#youtube", "https://takeout.google.com/settings/takeout/custom/youtube"],
      ["#google", "https://takeout.google.com"]
    ];
    for (const [section, href] of directRoutes) {
      const matchingLinks = await page.locator(`${section} a[href="${href}"]`).count();
      if (matchingLinks !== 1) {
        throw new Error(`${section} direct route count ${matchingLinks}; want 1`);
      }
    }
  }' >/dev/null
}

assert_screenshot_contract() {
  run_playwright run-code 'async page => {
    const images = await page.locator("img.instruction-screenshot").all();
    if (images.length !== 12) throw new Error(`screenshot count ${images.length}; want 12`);
    for (const image of images) {
      await image.scrollIntoViewIfNeeded();
      await image.evaluate((element) => element.decode());
    }
    const records = await page.locator("img.instruction-screenshot").evaluateAll((elements) =>
      elements.map((element) => ({
        alt: element.alt,
        complete: element.complete,
        id: element.dataset.screenshotId,
        naturalHeight: element.naturalHeight,
        naturalWidth: element.naturalWidth,
        path: new URL(element.src).pathname,
        width: element.getBoundingClientRect().width
      }))
    );
    if (new Set(records.map((record) => record.id)).size !== 12) {
      throw new Error("screenshot ids must be unique");
    }
    for (const record of records) {
      if (!record.alt || !record.complete || record.naturalWidth < 480 || record.naturalHeight < 220) {
        throw new Error(`invalid screenshot ${JSON.stringify(record)}`);
      }
      if (!record.path.startsWith("/images/instructions/") || !record.path.endsWith(".png")) {
        throw new Error(`invalid screenshot path ${record.path}`);
      }
      if (record.width > window.innerWidth) {
        throw new Error(`screenshot ${record.id} overflows viewport`);
      }
    }
    if (await page.locator("#tiktok img").count() !== 0) {
      throw new Error("TikTok must remain text-only until I011");
    }
    if (await page.locator(".ratio").count() !== 0) {
      throw new Error("placeholder screenshot tiles remain");
    }
  }' >/dev/null
}

assert_locale "en" "Facebook Accounts Center: Your information and permissions"
assert_locale "es" "Centro de cuentas de Facebook: Tu información y permisos"
assert_locale "fr" "Espace Comptes Facebook : Vos informations et autorisations"
assert_locale "ru" "Центр аккаунтов Facebook: раздел «Ваша информация и разрешения»"
assert_screenshot_contract

run_playwright resize 393 852 >/dev/null
assert_locale "en" "Facebook Accounts Center: Your information and permissions"
assert_locale "es" "Centro de cuentas de Facebook: Tu información y permisos"
assert_locale "fr" "Espace Comptes Facebook : Vos informations et autorisations"
assert_locale "ru" "Центр аккаунтов Facebook: раздел «Ваша информация и разрешения»"
assert_screenshot_contract

echo "Browser smoke test passed at ${base_url}"

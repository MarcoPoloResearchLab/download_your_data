#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 0 ]] || {
  echo "usage: scripts/release/test_application_artifact.sh" >&2
  exit 2
}

for required_command in cmp git python3 shasum tar; do
  command -v "${required_command}" >/dev/null 2>&1 || {
    echo "error: ${required_command} is required" >&2
    exit 1
  }
done

repository_root="$(git rev-parse --show-toplevel)"
version="v0.0.0-test"
source_commit="$(git -C "${repository_root}" rev-parse HEAD)"
release_timestamp="2026-01-01T00:00:00+00:00"
first_artifact_directory="$(mktemp -d)"
second_artifact_directory="$(mktemp -d)"
application_extract_directory="$(mktemp -d)"
pages_extract_directory="$(mktemp -d)"

cleanup() {
  rm -rf \
    "${first_artifact_directory}" \
    "${second_artifact_directory}" \
    "${application_extract_directory}" \
    "${pages_extract_directory}"
}
trap cleanup EXIT

prepare_staging() {
  artifact_directory="$1"
  mkdir -p "${artifact_directory}/payloads"
  python3 - \
    "${artifact_directory}/staging.json" \
    "${version}" \
    "${source_commit}" \
    "${release_timestamp}" <<'PY'
import json
from pathlib import Path
import sys

path, version, source_commit, release_timestamp = sys.argv[1:]
staging = {
    "schema_version": 1,
    "artifact_kind": "mprlab.release.staging",
    "version": version,
    "source_commit": source_commit,
    "release_timestamp": release_timestamp,
}
Path(path).write_text(
    json.dumps(staging, indent=2, sort_keys=True) + "\n",
    encoding="utf-8",
)
PY
}

prepare_artifact() {
  artifact_directory="$1"
  prepare_staging "${artifact_directory}"
  RELEASE_VERSION="${version}" \
  RELEASE_TIMESTAMP="${release_timestamp}" \
  RELEASE_ARTIFACT_DIR="${artifact_directory}" \
    make -C "${repository_root}" --no-print-directory release-artifacts
}

prepare_artifact "${first_artifact_directory}"
prepare_artifact "${second_artifact_directory}"

package_name="download-your-data_${version}_darwin_arm64"
archive_name="${package_name}.tar.gz"
checksum_name="${archive_name}.sha256"
first_assets="${first_artifact_directory}/payloads/release-assets"
second_assets="${second_artifact_directory}/payloads/release-assets"

cmp "${first_assets}/${archive_name}" "${second_assets}/${archive_name}"
cmp "${first_assets}/${checksum_name}" "${second_assets}/${checksum_name}"
cmp "${first_assets}/pages.tar.gz" "${second_assets}/pages.tar.gz"

(
  cd "${first_assets}"
  shasum -a 256 -c "${checksum_name}"
)

tar -xzf "${first_assets}/${archive_name}" -C "${application_extract_directory}"
binary_path="${application_extract_directory}/${package_name}/download-your-data"
[[ -x "${binary_path}" ]] || {
  echo "error: extracted application binary is missing or not executable" >&2
  exit 1
}
[[ "$("${binary_path}" version)" == "${version}" ]] || {
  echo "error: extracted application version does not match the release" >&2
  exit 1
}

"${repository_root}/scripts/command-smoke.sh" "${binary_path}"
PLAYWRIGHT_CLI_VERSION="${PLAYWRIGHT_CLI_VERSION:?PLAYWRIGHT_CLI_VERSION is required}" \
  "${repository_root}/scripts/browser-smoke.sh" "${binary_path}"

tar -xzf "${first_assets}/pages.tar.gz" -C "${pages_extract_directory}"
python3 - \
  "${pages_extract_directory}/.mprlab-release.json" \
  "${version}" \
  "${source_commit}" <<'PY'
import json
import sys

path, version, source_commit = sys.argv[1:]
with open(path, "r", encoding="utf-8") as handle:
    marker = json.load(handle)
expected = {
    "schema_version": 1,
    "release_version": version,
    "source_commit": source_commit,
}
for key, expected_value in expected.items():
    if marker.get(key) != expected_value:
        raise SystemExit(
            f"Pages marker {key}={marker.get(key)!r}; expected {expected_value!r}"
        )
PY

set +e
rg -n -i \
  '<script|fetch\(|/api/|DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN|BEGIN [A-Z ]*PRIVATE KEY' \
  "${pages_extract_directory}"
pages_scan_status=$?
set -e
case "${pages_scan_status}" in
  0)
    echo "error: Pages artifact contains application runtime code or secret-shaped content" >&2
    exit 1
    ;;
  1) ;;
  *)
    echo "error: Pages artifact static scan failed" >&2
    exit 1
    ;;
esac

echo "Application release artifact contract passed."

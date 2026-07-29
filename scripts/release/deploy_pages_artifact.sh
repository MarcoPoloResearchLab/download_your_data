#!/usr/bin/env bash
set -euo pipefail

usage() {
  cat <<'USAGE'
Usage:
  deploy_pages_artifact.sh --url <public-url> [options]

Downloads manifest.json and pages.tar.gz from a published GitHub Release,
verifies them against the locally sealed release and remote tag, replaces the
configured Pages branch, and converges the Pages source, custom domain, and
HTTPS settings.

Options:
  --remote <name>       Git remote. Default: origin
  --branch <name>       Pages branch. Default: gh-pages
  --version <tag>       Published release tag. Default: exact v* tag at HEAD
  --url <url>           Public Pages URL used for post-deploy verification
  --dry-run             Validate the published Pages artifact without changing GitHub
  --skip-verify         Do not verify the public release marker
  --help                Show this help text
USAGE
}

remote="origin"
branch="gh-pages"
version=""
url=""
verify="true"
dry_run="false"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --remote) [[ $# -ge 2 ]] || { echo "error: --remote requires a value" >&2; exit 1; }; remote="$2"; shift 2 ;;
    --branch) [[ $# -ge 2 ]] || { echo "error: --branch requires a value" >&2; exit 1; }; branch="$2"; shift 2 ;;
    --version) [[ $# -ge 2 ]] || { echo "error: --version requires a value" >&2; exit 1; }; version="$2"; shift 2 ;;
    --url) [[ $# -ge 2 ]] || { echo "error: --url requires a value" >&2; exit 1; }; url="$2"; shift 2 ;;
    --dry-run) dry_run="true"; shift ;;
    --skip-verify) verify="false"; shift ;;
    --help|-h) usage; exit 0 ;;
    *) echo "error: unknown argument: $1" >&2; usage; exit 1 ;;
  esac
done

[[ -n "${url}" || "${verify}" == "false" ]] || {
  echo "error: --url is required unless --skip-verify is set" >&2
  exit 1
}
required_commands=(awk cp curl find gh git grep head mkdir mktemp python3 rm shasum sleep tar)
for command_name in "${required_commands[@]}"; do
  command -v "${command_name}" >/dev/null 2>&1 || {
    echo "error: ${command_name} is required" >&2
    exit 1
  }
done
configure_attempts="${PAGES_CONFIGURE_ATTEMPTS:-12}"
configure_delay_seconds="${PAGES_CONFIGURE_DELAY_SECONDS:-5}"
[[ "${configure_attempts}" =~ ^[1-9][0-9]*$ ]] || {
  echo "error: PAGES_CONFIGURE_ATTEMPTS must be a positive integer" >&2
  exit 1
}
[[ "${configure_delay_seconds}" =~ ^[0-9]+$ ]] || {
  echo "error: PAGES_CONFIGURE_DELAY_SECONDS must be a non-negative integer" >&2
  exit 1
}

git_directory="$(git rev-parse --absolute-git-dir)"
prepared_manifest="${git_directory}/mprlab-release/manifest.json"
[[ -f "${prepared_manifest}" ]] || {
  echo "error: locally prepared release manifest is missing; run make release" >&2
  exit 1
}
if [[ -z "${version}" ]]; then
  version="$(git tag --points-at HEAD --list 'v*' --sort=-version:refname | head -n 1)"
fi
[[ -n "${version}" ]] || {
  echo "error: no exact release tag at HEAD; pass --version after make publish" >&2
  exit 1
}

temporary_directory="$(mktemp -d)"
trap 'rm -rf "${temporary_directory}"' EXIT
download_directory="${temporary_directory}/download"
site_directory="${temporary_directory}/site"
checkout_directory="${temporary_directory}/checkout"
mkdir -p "${download_directory}" "${site_directory}"
gh release download "${version}" \
  --pattern manifest.json \
  --pattern pages.tar.gz \
  --dir "${download_directory}"
archive="${download_directory}/pages.tar.gz"
downloaded_manifest="${download_directory}/manifest.json"
prepared_manifest_sha256="$(shasum -a 256 "${prepared_manifest}" | awk '{print $1}')"
downloaded_manifest_sha256="$(shasum -a 256 "${downloaded_manifest}" | awk '{print $1}')"
[[ "${downloaded_manifest_sha256}" == "${prepared_manifest_sha256}" ]] || {
  echo "error: published release manifest does not match the locally prepared release" >&2
  exit 1
}
release_values="$(python3 - "${downloaded_manifest}" "${version}" <<'PY'
import json
import sys

manifest = json.load(open(sys.argv[1], encoding="utf-8"))
if manifest.get("schema_version") != 2 or manifest.get("artifact_kind") != "mprlab.release":
    raise SystemExit("published release manifest has an invalid contract")
if manifest.get("version") != sys.argv[2]:
    raise SystemExit("published release manifest has the wrong version")
asset = next(
    (
        item
        for item in manifest["payloads"]
        if item["path"] == "payloads/release-assets/pages.tar.gz"
    ),
    None,
)
if asset is None:
    raise SystemExit(
        "published release has no Pages payload; run make release and make publish"
    )
print(manifest["release_commit"])
print(manifest["source_commit"])
print(asset["sha256"])
PY
)"
release_commit="${release_values%%$'\n'*}"
remaining_values="${release_values#*$'\n'}"
source_commit="${remaining_values%%$'\n'*}"
expected_sha256="${remaining_values#*$'\n'}"
remote_tag_commit="$(
  git ls-remote --tags "${remote}" "refs/tags/${version}^{}" |
    awk 'NR == 1 {print $1}'
)"
if [[ -z "${remote_tag_commit}" ]]; then
  remote_tag_commit="$(
    git ls-remote --tags "${remote}" "refs/tags/${version}" |
      awk 'NR == 1 {print $1}'
  )"
fi
[[ "${remote_tag_commit}" == "${release_commit}" ]] || {
  echo "error: published release manifest does not match remote tag ${version}" >&2
  exit 1
}
actual_sha256="$(shasum -a 256 "${archive}" | awk '{print $1}')"
[[ "${actual_sha256}" == "${expected_sha256}" ]] || {
  echo "error: published Pages asset does not match make release" >&2
  exit 1
}
python3 - "${archive}" <<'PY'
import pathlib
import sys
import tarfile

with tarfile.open(sys.argv[1], "r:gz") as archive:
    for member in archive.getmembers():
        path = pathlib.PurePosixPath(member.name)
        if path.is_absolute() or ".." in path.parts or member.issym() or member.islnk():
            raise SystemExit(f"unsafe Pages archive member: {member.name}")
PY
tar -xzf "${archive}" -C "${site_directory}"
python3 - \
  "${site_directory}/.mprlab-release.json" \
  "${version}" \
  "${source_commit}" <<'PY'
import json
import sys

marker_path, version, source_commit = sys.argv[1:]
try:
    marker = json.load(open(marker_path, encoding="utf-8"))
except (OSError, json.JSONDecodeError):
    raise SystemExit(f"published Pages marker is invalid for source {source_commit}")
if marker.get("schema_version") != 1:
    raise SystemExit(
        f"published Pages marker has an invalid schema for source {source_commit}"
    )
if marker.get("release_version") != version:
    raise SystemExit(
        f"published Pages marker has the wrong version for source {source_commit}"
    )
if marker.get("source_commit") != source_commit:
    raise SystemExit(
        f"published Pages marker has the wrong source; expected source {source_commit}"
    )
PY
domain="$(python3 - "${site_directory}/CNAME" <<'PY'
import pathlib
import re
import sys

path = pathlib.Path(sys.argv[1])
try:
    value = path.read_text(encoding="utf-8")
except OSError:
    raise SystemExit("published Pages artifact has no CNAME")
lines = value.splitlines()
if len(lines) != 1 or value != f"{lines[0]}\n":
    raise SystemExit(
        "published Pages CNAME must contain exactly one newline-terminated domain"
    )
domain = lines[0]
label = r"[a-z0-9](?:[a-z0-9-]{0,61}[a-z0-9])?"
if len(domain) > 253 or re.fullmatch(rf"{label}(?:\.{label})+", domain) is None:
    raise SystemExit("published Pages CNAME is not a canonical lowercase domain")
print(domain)
PY
)"
if [[ -n "${url}" ]]; then
  python3 - "${url}" "${domain}" <<'PY'
import sys
import urllib.parse

url = urllib.parse.urlparse(sys.argv[1])
if url.scheme != "https" or url.hostname != sys.argv[2] or url.path not in ("", "/"):
    raise SystemExit(
        "Pages verification URL must be the HTTPS root of the released CNAME"
    )
if (
    url.params
    or url.query
    or url.fragment
    or url.username
    or url.password
    or url.port
):
    raise SystemExit(
        "Pages verification URL must be the HTTPS root of the released CNAME"
    )
PY
fi

if [[ "${dry_run}" == "true" ]]; then
  echo "Pages release ${version} preflight passed for ${domain}; GitHub state was not changed."
  exit 0
fi

remote_url="$(git remote get-url "${remote}")"
git clone --no-checkout "${remote_url}" "${checkout_directory}" >/dev/null
if git -C "${checkout_directory}" show-ref \
  --verify \
  --quiet \
  "refs/remotes/origin/${branch}"; then
  git -C "${checkout_directory}" checkout \
    -B "${branch}" \
    "origin/${branch}" >/dev/null
else
  git -C "${checkout_directory}" checkout --orphan "${branch}" >/dev/null
fi
find "${checkout_directory}" \
  -mindepth 1 \
  -maxdepth 1 \
  ! -name .git \
  -exec rm -rf {} +
cp -R "${site_directory}"/. "${checkout_directory}/"
git -C "${checkout_directory}" add -A
branch_changed="false"
if ! git -C "${checkout_directory}" diff --cached --quiet; then
  git -C "${checkout_directory}" \
    -c user.name="MPR Lab Pages Deployer" \
    -c user.email="pages-deployer@mprlab.invalid" \
    commit -m "Deploy Pages for ${version}" >/dev/null
  git -C "${checkout_directory}" push origin "HEAD:refs/heads/${branch}"
  branch_changed="true"
else
  echo "Pages branch already contains ${version} from source ${source_commit}."
fi

pages_api="repos/{owner}/{repo}/pages"
pages_state="${temporary_directory}/pages.json"
pages_error="${temporary_directory}/pages.error"
fetch_pages_state() {
  if gh api "${pages_api}" >"${pages_state}" 2>"${pages_error}"; then
    return 0
  fi
  if grep -Fq "(HTTP 404)" "${pages_error}"; then
    return 1
  fi
  cat "${pages_error}" >&2
  exit 1
}
pages_settings_match() {
  python3 - "${pages_state}" "${branch}" "${domain}" <<'PY'
import json
import sys

state = json.load(open(sys.argv[1], encoding="utf-8"))
source = state.get("source") or {}
if state.get("build_type") not in (None, "legacy"):
    raise SystemExit(1)
if source.get("branch") != sys.argv[2] or source.get("path") != "/":
    raise SystemExit(1)
if state.get("cname") != sys.argv[3]:
    raise SystemExit(1)
PY
}
pages_https_ready() {
  python3 - "${pages_state}" "${branch}" "${domain}" <<'PY'
import json
import sys

state = json.load(open(sys.argv[1], encoding="utf-8"))
source = state.get("source") or {}
certificate = state.get("https_certificate") or {}
ready = (
    state.get("build_type") in (None, "legacy")
    and source.get("branch") == sys.argv[2]
    and source.get("path") == "/"
    and state.get("cname") == sys.argv[3]
    and state.get("https_enforced") is True
    and certificate.get("state") == "approved"
)
raise SystemExit(0 if ready else 1)
PY
}
pages_certificate_state() {
  python3 - "${pages_state}" <<'PY'
import json
import sys

state = json.load(open(sys.argv[1], encoding="utf-8"))
certificate = state.get("https_certificate") or {}
print(certificate.get("state") or "unavailable")
PY
}

configuration_changed="false"
if ! fetch_pages_state; then
  gh api \
    --method POST \
    "${pages_api}" \
    -f build_type=legacy \
    -f "source[branch]=${branch}" \
    -f "source[path]=/" >/dev/null
  configuration_changed="true"
  fetch_pages_state || {
    echo "error: GitHub Pages site was not readable after creation" >&2
    exit 1
  }
fi
if ! pages_settings_match; then
  gh api \
    --method PUT \
    "${pages_api}" \
    -f build_type=legacy \
    -f "source[branch]=${branch}" \
    -f "source[path]=/" \
    -f "cname=${domain}" >/dev/null
  configuration_changed="true"
fi
if [[ "${branch_changed}" == "true" || "${configuration_changed}" == "true" ]]; then
  gh api --method POST "${pages_api}/builds" >/dev/null
fi

pages_ready="false"
certificate_state="unavailable"
for ((attempt = 1; attempt <= configure_attempts; attempt += 1)); do
  fetch_pages_state || {
    echo "error: GitHub Pages site disappeared during configuration" >&2
    exit 1
  }
  if pages_https_ready; then
    pages_ready="true"
    break
  fi
  if pages_settings_match; then
    certificate_state="$(pages_certificate_state)"
    if [[ "${certificate_state}" == "approved" ]]; then
      gh api --method PUT "${pages_api}" -F https_enforced=true >/dev/null
      fetch_pages_state || {
        echo "error: GitHub Pages site disappeared after enabling HTTPS" >&2
        exit 1
      }
      if pages_https_ready; then
        pages_ready="true"
        break
      fi
    fi
  else
    certificate_state="settings-pending"
  fi
  if [[ "${attempt}" -lt "${configure_attempts}" ]]; then
    sleep "${configure_delay_seconds}"
  fi
done
if [[ "${pages_ready}" != "true" ]]; then
  echo "error: GitHub Pages did not converge to ${branch}:/, ${domain}, and enforced HTTPS; certificate state: ${certificate_state}; rerun make deploy after GitHub provisions the certificate" >&2
  exit 1
fi
echo "Configured GitHub Pages from ${branch}:/ with ${domain} and enforced HTTPS."

if [[ "${verify}" == "true" ]]; then
  marker_url="${url%/}/.mprlab-release.json"
  attempts="${PAGES_VERIFY_ATTEMPTS:-12}"
  delay_seconds="${PAGES_VERIFY_DELAY_SECONDS:-5}"
  for ((attempt = 1; attempt <= attempts; attempt += 1)); do
    marker="$(curl --fail --silent --show-error "${marker_url}" 2>/dev/null || true)"
    if python3 - \
      "${version}" \
      "${source_commit}" \
      "${marker}" >/dev/null 2>&1 <<'PY'
import json
import sys

data = json.loads(sys.argv[3])
if data.get("schema_version") != 1:
    raise SystemExit(1)
if data.get("release_version") != sys.argv[1]:
    raise SystemExit(1)
if data.get("source_commit") != sys.argv[2]:
    raise SystemExit(1)
PY
    then
      echo "Verified ${url} at source ${source_commit}."
      exit 0
    fi
    sleep "${delay_seconds}"
  done
  echo "error: Pages marker did not reach source ${source_commit}: ${marker_url}" >&2
  exit 1
fi

echo "Deployed Pages release ${version} from source ${source_commit}."

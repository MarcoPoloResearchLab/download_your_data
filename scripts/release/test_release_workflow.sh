#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 0 ]] || {
  echo "usage: scripts/release/test_release_workflow.sh" >&2
  exit 2
}

for required_command in git make python3 tar; do
  command -v "${required_command}" >/dev/null 2>&1 || {
    echo "error: ${required_command} is required" >&2
    exit 1
  }
done

repository_root="$(git rev-parse --show-toplevel)"
temporary_root="$(mktemp -d)"
fixture_repository="${temporary_root}/repository"
fixture_remote="${temporary_root}/remote.git"
fake_binary_directory="${temporary_root}/bin"
published_pages_directory="${temporary_root}/published-pages"
pages_state="${temporary_root}/pages-state.json"
pages_api_log="${temporary_root}/pages-api.log"

cleanup() {
  rm -rf "${temporary_root}"
}
trap cleanup EXIT

mkdir -p \
  "${fixture_repository}/scripts/release" \
  "${fake_binary_directory}" \
  "${published_pages_directory}"

cp \
  "${repository_root}/scripts/release/prepare_release.sh" \
  "${repository_root}/scripts/release/publish_release.sh" \
  "${repository_root}/scripts/release/deploy_pages_artifact.sh" \
  "${repository_root}/scripts/release/release_helper.py" \
  "${fixture_repository}/scripts/release/"
chmod +x "${fixture_repository}/scripts/release/"*

cat >"${fixture_repository}/Makefile" <<'MAKEFILE'
.PHONY: ci deploy publish release release-artifacts

ci:
	@true

release:
	@/bin/bash scripts/release/prepare_release.sh

release-artifacts:
	@python3 scripts/release/fixture_artifacts.py

publish:
	@/bin/bash scripts/release/publish_release.sh

deploy:
	@/bin/bash scripts/release/deploy_pages_artifact.sh --branch gh-pages --url https://release.example.invalid/
MAKEFILE

cat >"${fixture_repository}/scripts/release/fixture_artifacts.py" <<'PY'
#!/usr/bin/env python3

import io
import json
import os
from pathlib import Path
import tarfile

artifact_directory = Path(os.environ["RELEASE_ARTIFACT_DIR"])
staging = json.loads(
    (artifact_directory / "staging.json").read_text(encoding="utf-8")
)
asset_directory = artifact_directory / "payloads" / "release-assets"
asset_directory.mkdir(parents=True, exist_ok=True)
(asset_directory / "application.txt").write_text(
    f"application {staging['version']}\n",
    encoding="utf-8",
)
marker = {
    "schema_version": 1,
    "release_version": staging["version"],
    "source_commit": staging["source_commit"],
    "release_timestamp": staging["release_timestamp"],
}
with tarfile.open(asset_directory / "pages.tar.gz", "w:gz") as archive:
    for name, contents in {
        ".mprlab-release.json": json.dumps(marker, sort_keys=True) + "\n",
        ".nojekyll": "",
        "CNAME": "release.example.invalid\n",
        "index.html": "<!doctype html><title>fixture</title>\n",
    }.items():
        encoded = contents.encode("utf-8")
        information = tarfile.TarInfo(name)
        information.size = len(encoded)
        information.mode = 0o644
        archive.addfile(information, io.BytesIO(encoded))
PY
chmod +x "${fixture_repository}/scripts/release/fixture_artifacts.py"

cat >"${fixture_repository}/CHANGELOG.md" <<'CHANGELOG'
# Changelog
CHANGELOG
printf '%s\n' "fixture source" >"${fixture_repository}/source.txt"

git init --bare "${fixture_remote}" >/dev/null
git -C "${fixture_remote}" symbolic-ref HEAD refs/heads/master
git -C "${fixture_repository}" init -b master >/dev/null
git -C "${fixture_repository}" config user.name "Release Contract"
git -C "${fixture_repository}" config user.email "release-contract@mprlab.invalid"
git -C "${fixture_repository}" remote add origin "${fixture_remote}"
git -C "${fixture_repository}" add .
git -C "${fixture_repository}" commit -m "Initial source" >/dev/null
git -C "${fixture_repository}" push -u origin master >/dev/null
git -C "${fixture_repository}" remote set-head origin master

make -C "${fixture_repository}" --no-print-directory release
first_release_commit="$(git -C "${fixture_repository}" rev-parse HEAD)"
first_release_tag="$(
  git -C "${fixture_repository}" tag --points-at HEAD --list "v*" --sort=-version:refname |
    head -n 1
)"
[[ "${first_release_tag}" == "v1.0.0" ]] || {
  echo "error: first release tag is ${first_release_tag}; expected v1.0.0" >&2
  exit 1
}
[[ "$(
  git -C "${fixture_repository}" diff-tree \
    --no-commit-id \
    --name-only \
    -r \
    "${first_release_commit}"
)" == "CHANGELOG.md" ]] || {
  echo "error: release commit must contain only CHANGELOG.md" >&2
  exit 1
}

make -C "${fixture_repository}" --no-print-directory release
[[ "$(git -C "${fixture_repository}" rev-parse HEAD)" == "${first_release_commit}" ]] || {
  echo "error: repeated make release created another commit" >&2
  exit 1
}
[[ "$(git -C "${fixture_repository}" tag --points-at HEAD --list "v*" | wc -l | tr -d ' ')" == "1" ]] || {
  echo "error: repeated make release changed the exact release tag set" >&2
  exit 1
}

cat >"${fake_binary_directory}/gh" <<'FAKE_GH'
#!/usr/bin/env bash
set -euo pipefail

if [[ "$1" == "pr" && "$2" == "list" ]]; then
  printf '%s\n' "[]"
  exit 0
fi

if [[ "$1" == "release" && "$2" == "download" ]]; then
  destination=""
  while [[ $# -gt 0 ]]; do
    if [[ "$1" == "--dir" ]]; then
      destination="$2"
      shift 2
    else
      shift
    fi
  done
  [[ -n "${destination}" ]] || {
    echo "fake gh release download requires --dir" >&2
    exit 2
  }
  cp \
    "${FAKE_RELEASE_ARTIFACT_DIR}/manifest.json" \
    "${FAKE_RELEASE_ARTIFACT_DIR}/payloads/release-assets/pages.tar.gz" \
    "${destination}/"
  exit 0
fi

if [[ "$1" != "api" ]]; then
  echo "unexpected fake gh command: $*" >&2
  exit 2
fi

shift
method="GET"
endpoint=""
arguments="$*"
while [[ $# -gt 0 ]]; do
  case "$1" in
    --method) method="$2"; shift 2 ;;
    -f|-F|-H) shift 2 ;;
    repos/*) endpoint="$1"; shift ;;
    *) shift ;;
  esac
done
printf '%s %s %s\n' "${method}" "${endpoint}" "${arguments}" \
  >>"${FAKE_PAGES_API_LOG}"
case "${method}:${endpoint}" in
  "GET:repos/{owner}/{repo}/pages")
    if [[ ! -f "${FAKE_PAGES_STATE}" ]]; then
      echo "gh: Not Found (HTTP 404)" >&2
      exit 1
    fi
    cat "${FAKE_PAGES_STATE}"
    ;;
  "POST:repos/{owner}/{repo}/pages")
    printf '%s\n' \
      "{\"source\":{\"branch\":\"${FAKE_PAGES_BRANCH}\",\"path\":\"/\"},\"cname\":null,\"https_enforced\":false,\"https_certificate\":{\"state\":\"approved\"}}" \
      >"${FAKE_PAGES_STATE}"
    ;;
  "PUT:repos/{owner}/{repo}/pages")
    case " ${arguments} " in
      *" https_enforced=true "*) https_enforced="true" ;;
      *) https_enforced="false" ;;
    esac
    printf '%s\n' \
      "{\"source\":{\"branch\":\"${FAKE_PAGES_BRANCH}\",\"path\":\"/\"},\"cname\":\"${FAKE_PAGES_DOMAIN}\",\"https_enforced\":${https_enforced},\"https_certificate\":{\"state\":\"approved\"}}" \
      >"${FAKE_PAGES_STATE}"
    ;;
  "POST:repos/{owner}/{repo}/pages/builds") ;;
  *)
    echo "unexpected fake gh api call: ${method} ${endpoint}" >&2
    exit 2
    ;;
esac
FAKE_GH

cat >"${fake_binary_directory}/curl" <<'FAKE_CURL'
#!/usr/bin/env bash
set -euo pipefail
cat "${FAKE_PAGES_MARKER}"
FAKE_CURL
chmod +x "${fake_binary_directory}/gh" "${fake_binary_directory}/curl"

release_artifact_directory="$(
  git -C "${fixture_repository}" rev-parse --absolute-git-dir
)/mprlab-release"
if ! (
  cd "${fixture_repository}"
  PATH="${fake_binary_directory}:${PATH}" \
    ./scripts/release/release_helper.py \
    publish-prepared-release \
    --dry-run
) >"${temporary_root}/publish-plan.json"; then
  cat "${temporary_root}/publish-plan.json" >&2
  echo "error: publish dry run failed" >&2
  exit 1
fi
python3 - "${temporary_root}/publish-plan.json" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    plan = json.load(handle)
if plan.get("ok") is not True or plan.get("dry_run") is not True:
    raise SystemExit("publish dry run did not produce a successful plan")
expected = {
    "push_branch": True,
    "push_tag": True,
    "publish_github_release": True,
}
for key, expected_value in expected.items():
    if plan.get("plan", {}).get(key) is not expected_value:
        raise SystemExit(f"publish plan {key} did not equal {expected_value}")
PY

git -C "${fixture_repository}" push origin master >/dev/null
git -C "${fixture_repository}" push origin "refs/tags/${first_release_tag}" >/dev/null
tar -xzf \
  "${release_artifact_directory}/payloads/release-assets/pages.tar.gz" \
  -C "${published_pages_directory}"

: >"${pages_api_log}"
dry_run_output="$(
  cd "${fixture_repository}"
  PATH="${fake_binary_directory}:${PATH}" \
  FAKE_RELEASE_ARTIFACT_DIR="${release_artifact_directory}" \
  FAKE_PAGES_API_LOG="${pages_api_log}" \
    /bin/bash \
    ./scripts/release/deploy_pages_artifact.sh \
    --branch gh-pages \
    --url https://release.example.invalid/ \
    --dry-run
)"
[[ "${dry_run_output}" == *"Pages release ${first_release_tag} preflight passed for release.example.invalid; GitHub state was not changed."* ]] || {
  echo "error: Pages dry run did not report the sealed custom domain" >&2
  exit 1
}
[[ ! -s "${pages_api_log}" ]] || {
  echo "error: Pages dry run contacted the Pages configuration API" >&2
  exit 1
}
if git --git-dir="${fixture_remote}" show-ref --verify --quiet refs/heads/gh-pages; then
  echo "error: Pages dry run created the deployment branch" >&2
  exit 1
fi

deploy_output="$(
  cd "${fixture_repository}"
  PATH="${fake_binary_directory}:${PATH}" \
  FAKE_RELEASE_ARTIFACT_DIR="${release_artifact_directory}" \
  FAKE_PAGES_MARKER="${published_pages_directory}/.mprlab-release.json" \
  FAKE_PAGES_STATE="${pages_state}" \
  FAKE_PAGES_API_LOG="${pages_api_log}" \
  FAKE_PAGES_BRANCH="gh-pages" \
  FAKE_PAGES_DOMAIN="release.example.invalid" \
  PAGES_CONFIGURE_ATTEMPTS=1 \
  PAGES_CONFIGURE_DELAY_SECONDS=0 \
  PAGES_VERIFY_ATTEMPTS=1 \
  PAGES_VERIFY_DELAY_SECONDS=0 \
    /bin/bash \
    ./scripts/release/deploy_pages_artifact.sh \
    --branch gh-pages \
    --url https://release.example.invalid/
)"
[[ "${deploy_output}" == *"Configured GitHub Pages from gh-pages:/ with release.example.invalid and enforced HTTPS."* ]] || {
  echo "error: Pages deployment did not converge the custom domain and HTTPS" >&2
  exit 1
}
[[ "${deploy_output}" == *"Verified https://release.example.invalid/ at source "* ]] || {
  echo "error: Pages deployment did not verify the public source marker" >&2
  exit 1
}

git --git-dir="${fixture_remote}" show \
  refs/heads/gh-pages:.mprlab-release.json \
  >"${temporary_root}/deployed-marker.json"
cmp \
  "${published_pages_directory}/.mprlab-release.json" \
  "${temporary_root}/deployed-marker.json"
[[ "$(
  git --git-dir="${fixture_remote}" show refs/heads/gh-pages:CNAME
)" == "release.example.invalid" ]] || {
  echo "error: deployed Pages branch does not own the sealed CNAME" >&2
  exit 1
}
python3 - "${pages_state}" <<'PY'
import json
import sys

state = json.load(open(sys.argv[1], encoding="utf-8"))
expected = {
    "cname": "release.example.invalid",
    "https_certificate": {"state": "approved"},
    "https_enforced": True,
    "source": {"branch": "gh-pages", "path": "/"},
}
if state != expected:
    raise SystemExit(f"Pages API settings did not converge: {state!r}")
PY
grep -Fq "POST repos/{owner}/{repo}/pages " "${pages_api_log}"
grep -Fq "PUT repos/{owner}/{repo}/pages " "${pages_api_log}"
grep -Fq "cname=release.example.invalid" "${pages_api_log}"
grep -Fq "https_enforced=true" "${pages_api_log}"

: >"${pages_api_log}"
idempotent_output="$(
  cd "${fixture_repository}"
  PATH="${fake_binary_directory}:${PATH}" \
  FAKE_RELEASE_ARTIFACT_DIR="${release_artifact_directory}" \
  FAKE_PAGES_MARKER="${published_pages_directory}/.mprlab-release.json" \
  FAKE_PAGES_STATE="${pages_state}" \
  FAKE_PAGES_API_LOG="${pages_api_log}" \
  FAKE_PAGES_BRANCH="gh-pages" \
  FAKE_PAGES_DOMAIN="release.example.invalid" \
  PAGES_CONFIGURE_ATTEMPTS=1 \
  PAGES_CONFIGURE_DELAY_SECONDS=0 \
  PAGES_VERIFY_ATTEMPTS=1 \
  PAGES_VERIFY_DELAY_SECONDS=0 \
    /bin/bash \
    ./scripts/release/deploy_pages_artifact.sh \
    --branch gh-pages \
    --url https://release.example.invalid/
)"
[[ "${idempotent_output}" == *"Pages branch already contains ${first_release_tag}"* ]] || {
  echo "error: repeated Pages deployment did not report branch convergence" >&2
  exit 1
}
if grep -Eq "^(POST|PUT) " "${pages_api_log}"; then
  echo "error: repeated Pages deployment mutated converged GitHub settings" >&2
  exit 1
fi

/bin/bash -n "${repository_root}/scripts/release/prepare_release.sh"
/bin/bash -n "${repository_root}/scripts/release/publish_release.sh"
/bin/bash -n "${repository_root}/scripts/release/deploy_pages_artifact.sh"
python3 - "${repository_root}/scripts/release/release_helper.py" <<'PY'
from pathlib import Path
import sys

source = Path(sys.argv[1]).read_text(encoding="utf-8")
compile(source, sys.argv[1], "exec")
PY

echo "Release, publish, and deploy workflow contract passed."

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

if [[ "$1" == "api" ]]; then
  exit 0
fi

echo "unexpected fake gh command: $*" >&2
exit 2
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

(
  cd "${fixture_repository}"
  PATH="${fake_binary_directory}:${PATH}" \
  FAKE_RELEASE_ARTIFACT_DIR="${release_artifact_directory}" \
  FAKE_PAGES_MARKER="${published_pages_directory}/.mprlab-release.json" \
  PAGES_VERIFY_ATTEMPTS=1 \
    /bin/bash \
    ./scripts/release/deploy_pages_artifact.sh \
    --branch gh-pages \
    --url https://release.example.invalid/
)

git --git-dir="${fixture_remote}" show \
  refs/heads/gh-pages:.mprlab-release.json \
  >"${temporary_root}/deployed-marker.json"
cmp \
  "${published_pages_directory}/.mprlab-release.json" \
  "${temporary_root}/deployed-marker.json"

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

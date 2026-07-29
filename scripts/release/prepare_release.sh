#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 0 ]] || {
  echo "error: make release accepts no arguments" >&2
  exit 2
}

command -v git >/dev/null 2>&1 || {
  echo "error: git is required" >&2
  exit 1
}
command -v python3 >/dev/null 2>&1 || {
  echo "error: python3 is required" >&2
  exit 1
}

repository_root="$(git rev-parse --show-toplevel)"
cd "${repository_root}"

helper="${repository_root}/scripts/release/release_helper.py"
[[ -x "${helper}" ]] || {
  echo "error: release helper is not executable: ${helper}" >&2
  exit 1
}

json_value() {
  python3 - "$1" "$2" <<'PY'
import json
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    value = json.load(handle)
for part in sys.argv[2].split("."):
    value = value.get(part) if isinstance(value, dict) else None
print("" if value is None else value)
PY
}

select_release() {
  python3 - "$1" <<'PY'
import json
import re
import sys

with open(sys.argv[1], "r", encoding="utf-8") as handle:
    data = json.load(handle)

info = data.get("version_info") or {}
effective_scheme = info.get("scheme_guess") or "none"


def next_semver(latest):
    if not latest:
        return "v1.0.0"
    match = re.match(
        r"^(v?)(\d+)\.(\d+)\.(\d+)(?:[-+].*)?$",
        latest,
    )
    if not match:
        raise SystemExit(f"latest SemVer tag is invalid: {latest}")
    prefix, major, minor, patch = match.groups()
    return f"{prefix or 'v'}{int(major)}.{int(minor)}.{int(patch) + 1}"


if effective_scheme in ("semver", "mixed"):
    selected = next_semver(info.get("latest_semver_tag") or "")
elif effective_scheme == "calver":
    candidate = info.get("calver_candidate") or {}
    if candidate.get("ok") is not True:
        raise SystemExit("CalVer candidate is not valid for this release timestamp")
    selected = info.get("next_calver") or ""
else:
    selected = next_semver("")

if effective_scheme == "calver":
    boundary = info.get("latest_calver_tag") or ""
elif effective_scheme in ("semver", "mixed"):
    boundary = info.get("latest_semver_tag") or ""
else:
    boundary = info.get("latest_tag") or ""

if not selected:
    raise SystemExit("release version selection returned an empty version")

print(selected)
print(boundary)
print(effective_scheme)
PY
}

require_unused_release_tag() {
  release_tag="$1"
  if ! git check-ref-format "refs/tags/${release_tag}"; then
    echo "error: invalid release tag: ${release_tag}" >&2
    exit 1
  fi
  if git show-ref --verify --quiet "refs/tags/${release_tag}"; then
    echo "error: release tag already exists: ${release_tag}" >&2
    exit 1
  fi
}

preflight_json="$(mktemp)"
notes_file="$(mktemp)"
prepared_artifact_json="$(mktemp)"
cleanup() {
  rm -f "${preflight_json}" "${notes_file}" "${prepared_artifact_json}"
}
trap cleanup EXIT

release_timestamp="$(date +%Y-%m-%dT%H:%M:%S%z)"
release_date="${release_timestamp%%T*}"

run_local_preflight() {
  if ! "${helper}" preflight \
    --local \
    --release-timestamp "${release_timestamp}" \
    >"${preflight_json}"; then
    cat "${preflight_json}"
    echo "error: local release preflight failed" >&2
    exit 1
  fi
  cat "${preflight_json}"
}

echo "==> [release] Checking local release state"
run_local_preflight
default_branch="$(json_value "${preflight_json}" "default_branch")"
source_commit="$(git rev-parse HEAD)"
selection="$(select_release "${preflight_json}")"
next_version="$(sed -n '1p' <<<"${selection}")"
boundary_tag="$(sed -n '2p' <<<"${selection}")"

reuse_current_release="false"
if [[ -n "${boundary_tag}" ]]; then
  boundary_commit="$(git rev-parse --verify "${boundary_tag}^{commit}")"
  if [[ "${boundary_commit}" == "${source_commit}" ]]; then
    reuse_current_release="true"
  fi
fi

if [[ "${reuse_current_release}" == "true" ]]; then
  if ! "${helper}" verify-release-artifact >"${prepared_artifact_json}"; then
    cat "${prepared_artifact_json}"
    echo "error: ${boundary_tag} points at HEAD but its sealed release artifact is unavailable" >&2
    exit 1
  fi
  prepared_version="$(json_value "${prepared_artifact_json}" "manifest.version")"
  prepared_source_commit="$(json_value "${prepared_artifact_json}" "manifest.source_commit")"
  prepared_release_commit="$(json_value "${prepared_artifact_json}" "manifest.release_commit")"
  prepared_default_branch="$(json_value "${prepared_artifact_json}" "manifest.default_branch")"
  expected_source_commit="$(git rev-parse --verify "${source_commit}^")"
  release_changed_files="$(git diff-tree --no-commit-id --name-only -r "${source_commit}")"
  [[ "${prepared_version}" == "${boundary_tag}" ]] || {
    echo "error: sealed release version ${prepared_version} does not match ${boundary_tag}" >&2
    exit 1
  }
  [[ "${prepared_source_commit}" == "${expected_source_commit}" ]] || {
    echo "error: sealed release source does not match ${boundary_tag} parent" >&2
    exit 1
  }
  [[ "${prepared_release_commit}" == "${source_commit}" ]] || {
    echo "error: sealed release commit does not match HEAD" >&2
    exit 1
  }
  [[ "${prepared_default_branch}" == "${default_branch}" ]] || {
    echo "error: sealed release default branch does not match ${default_branch}" >&2
    exit 1
  }
  [[ "${release_changed_files}" == "CHANGELOG.md" ]] || {
    echo "error: ${boundary_tag} release commit must contain only CHANGELOG.md" >&2
    exit 1
  }
  echo "Release ${boundary_tag} is already sealed at ${source_commit}; no changes are required."
  exit 0
fi

require_unused_release_tag "${next_version}"

echo "==> [release] Running make ci"
make ci

echo "==> [release] Rechecking local state after CI"
run_local_preflight
[[ "$(git rev-parse HEAD)" == "${source_commit}" ]] || {
  echo "error: HEAD changed while make ci was running" >&2
  exit 1
}
selection="$(select_release "${preflight_json}")"
next_version="$(sed -n '1p' <<<"${selection}")"
boundary_tag="$(sed -n '2p' <<<"${selection}")"
require_unused_release_tag "${next_version}"

"${helper}" initialize-release-artifact \
  --version "${next_version}" \
  --source-commit "${source_commit}" \
  --release-timestamp "${release_timestamp}"

artifact_directory="$(git rev-parse --git-path mprlab-release)"
if [[ "${artifact_directory}" != /* ]]; then
  artifact_directory="${repository_root}/${artifact_directory}"
fi

echo "==> [release] Preparing sealed application and Pages artifacts"
RELEASE_VERSION="${next_version}" \
RELEASE_TIMESTAMP="${release_timestamp}" \
RELEASE_ARTIFACT_DIR="${artifact_directory}" \
make --no-print-directory release-artifacts

echo "==> [release] Rechecking local state after artifact preparation"
run_local_preflight
[[ "$(git rev-parse HEAD)" == "${source_commit}" ]] || {
  echo "error: HEAD changed while preparing release artifacts" >&2
  exit 1
}
require_unused_release_tag "${next_version}"

echo "==> [release] Preparing ${next_version} from local Git history"
notes_arguments=(generate-notes --version "${next_version}" --release-date "${release_date}")
if [[ -n "${boundary_tag}" ]]; then
  notes_arguments+=(--since-tag "${boundary_tag}")
fi
"${helper}" "${notes_arguments[@]}" | tee "${notes_file}"
"${helper}" insert-changelog --notes-file "${notes_file}"

git add CHANGELOG.md
if git diff --cached --quiet -- CHANGELOG.md; then
  echo "error: CHANGELOG.md has no staged release changes" >&2
  exit 1
fi
staged_files="$(git diff --cached --name-only)"
if [[ "${staged_files}" != "CHANGELOG.md" ]]; then
  echo "error: release commit may contain only CHANGELOG.md" >&2
  printf '%s\n' "${staged_files}" >&2
  exit 1
fi
require_unused_release_tag "${next_version}"

git commit -m "Release ${next_version}"
release_commit="$(git rev-parse HEAD)"
git tag -a "${next_version}" -m "Release ${next_version}" "${release_commit}"
"${helper}" write-release-artifact \
  --version "${next_version}" \
  --source-commit "${source_commit}" \
  --release-commit "${release_commit}" \
  --notes-file "${notes_file}" \
  --default-branch "${default_branch}" \
  --release-timestamp "${release_timestamp}"

echo "Prepared ${next_version} at ${release_commit}. Run make publish to publish it."

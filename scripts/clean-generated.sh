#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 1 ]] || {
  echo "usage: scripts/clean-generated.sh <repository-directory>" >&2
  exit 2
}

readonly repository_directory="$1"
readonly expected_module="module github.com/MarcoPoloResearchLab/download_your_data"

[[ -f "${repository_directory}/go.mod" ]] || {
  echo "clean target is not a Download Your Data checkout: ${repository_directory}" >&2
  exit 1
}
grep -Fqx "${expected_module}" "${repository_directory}/go.mod" || {
  echo "clean target has an unexpected Go module: ${repository_directory}" >&2
  exit 1
}

rm -rf \
  "${repository_directory}/build" \
  "${repository_directory}/.playwright-cli"

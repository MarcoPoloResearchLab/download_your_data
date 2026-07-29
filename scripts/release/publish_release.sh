#!/usr/bin/env bash
set -euo pipefail

[[ $# -eq 0 ]] || {
  echo "error: make publish accepts no arguments" >&2
  exit 2
}

repository_root="$(git rev-parse --show-toplevel)"
helper="${repository_root}/scripts/release/release_helper.py"
[[ -x "${helper}" ]] || {
  echo "error: release helper is not executable: ${helper}" >&2
  exit 1
}

exec "${helper}" publish-prepared-release

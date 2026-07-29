#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scripts/local-server.sh <up|down> <absolute-binary-path> [state-directory]" >&2
  exit 2
}

fail() {
  echo "local server lifecycle error: $*" >&2
  exit 1
}

[[ $# -ge 2 && $# -le 3 ]] || usage

readonly action="$1"
readonly binary_path="$2"
readonly expected_command="${binary_path} serve"
readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

[[ "${action}" == "up" || "${action}" == "down" ]] || usage
[[ "${binary_path}" == /* ]] || fail "binary path must be absolute: ${binary_path}"

if [[ $# -eq 3 ]]; then
  state_directory="$3"
else
  readonly git_directory="$(git -C "${repository_root}" rev-parse --absolute-git-dir)"
  state_directory="${git_directory}/mprlab-local"
fi
readonly state_directory
readonly state_file="${state_directory}/download-your-data-server.state"

recorded_pid=""
recorded_start=""

process_start() {
  LC_ALL=C ps -p "$1" -o lstart= 2>/dev/null |
    sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

process_command() {
  LC_ALL=C ps -ww -p "$1" -o command= 2>/dev/null |
    sed -e 's/^[[:space:]]*//' -e 's/[[:space:]]*$//'
}

read_state() {
  local line_count

  line_count="$(wc -l <"${state_file}" | tr -d '[:space:]')"
  [[ "${line_count}" == "2" ]] ||
    fail "invalid ownership state at ${state_file}; inspect it before removing it"

  recorded_pid="$(sed -n '1p' "${state_file}")"
  recorded_start="$(sed -n '2p' "${state_file}")"

  [[ "${recorded_pid}" =~ ^[1-9][0-9]*$ ]] ||
    fail "invalid process ID in ${state_file}; inspect it before removing it"
  [[ -n "${recorded_start}" ]] ||
    fail "missing process start identity in ${state_file}; inspect it before removing it"
}

state_matches_process() {
  local actual_command
  local actual_start

  kill -0 "${recorded_pid}" >/dev/null 2>&1 || return 1
  actual_start="$(process_start "${recorded_pid}")"
  actual_command="$(process_command "${recorded_pid}")"

  [[ "${actual_start}" == "${recorded_start}" &&
    "${actual_command}" == "${expected_command}" ]]
}

remove_owned_state() {
  local current_pid
  local current_start

  [[ -f "${state_file}" ]] || return 0
  current_pid="$(sed -n '1p' "${state_file}")"
  current_start="$(sed -n '2p' "${state_file}")"
  if [[ "${current_pid}" == "$1" && "${current_start}" == "$2" ]]; then
    rm -f "${state_file}"
  fi
}

bring_down() {
  local attempt

  if [[ ! -e "${state_file}" ]]; then
    echo "Local server is not running."
    return 0
  fi
  [[ -f "${state_file}" && ! -L "${state_file}" ]] ||
    fail "ownership state is not a regular file: ${state_file}"

  read_state
  if ! kill -0 "${recorded_pid}" >/dev/null 2>&1; then
    remove_owned_state "${recorded_pid}" "${recorded_start}"
    echo "Local server is not running; removed its stale ownership state."
    return 0
  fi

  state_matches_process ||
    fail "PID ${recorded_pid} does not match the server started by make up; no process was stopped"

  kill -TERM "${recorded_pid}"
  for attempt in $(seq 1 50); do
    if ! kill -0 "${recorded_pid}" >/dev/null 2>&1; then
      remove_owned_state "${recorded_pid}" "${recorded_start}"
      echo "Local server stopped."
      return 0
    fi
    sleep 0.1
  done

  fail "server PID ${recorded_pid} did not stop after SIGTERM"
}

bring_up() {
  local wait_status

  [[ -x "${binary_path}" && ! -d "${binary_path}" ]] ||
    fail "server binary is not executable: ${binary_path}"

  umask 077
  mkdir -p "${state_directory}"
  [[ -d "${state_directory}" && ! -L "${state_directory}" ]] ||
    fail "state directory is not a regular directory: ${state_directory}"
  chmod 700 "${state_directory}"

  if [[ -e "${state_file}" ]]; then
    [[ -f "${state_file}" && ! -L "${state_file}" ]] ||
      fail "ownership state is not a regular file: ${state_file}"
    read_state
    if kill -0 "${recorded_pid}" >/dev/null 2>&1; then
      state_matches_process ||
        fail "PID ${recorded_pid} does not match this local server; no process was started"
      fail "local server is already running as PID ${recorded_pid}"
    fi
    remove_owned_state "${recorded_pid}" "${recorded_start}"
  fi

  child_pid=""
  child_start=""
  state_temp="${state_file}.tmp.$$"

  cleanup() {
    rm -f "${state_temp}"
    if [[ -n "${child_pid}" ]] && kill -0 "${child_pid}" >/dev/null 2>&1; then
      recorded_pid="${child_pid}"
      recorded_start="${child_start}"
      if state_matches_process; then
        kill -TERM "${child_pid}" >/dev/null 2>&1 || true
      fi
    fi
    if [[ -n "${child_pid}" ]]; then
      wait "${child_pid}" >/dev/null 2>&1 || true
      remove_owned_state "${child_pid}" "${child_start}"
    fi
  }
  trap cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  "${binary_path}" serve &
  child_pid="$!"
  child_start="$(process_start "${child_pid}")"
  [[ -n "${child_start}" ]] ||
    fail "server exited before ownership could be recorded"

  printf '%s\n%s\n' "${child_pid}" "${child_start}" >"${state_temp}"
  chmod 600 "${state_temp}"
  mv "${state_temp}" "${state_file}"

  echo "Local server running as PID ${child_pid}; use make down from this checkout to stop it."

  set +e
  wait "${child_pid}"
  wait_status="$?"
  set -e

  case "${wait_status}" in
    0 | 130 | 143)
      return 0
      ;;
    *)
      return "${wait_status}"
      ;;
  esac
}

case "${action}" in
  up)
    bring_up
    ;;
  down)
    bring_down
    ;;
esac

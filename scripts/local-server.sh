#!/usr/bin/env bash
set -euo pipefail

usage() {
  echo "usage: scripts/local-server.sh <up|down> <absolute-binary-path> [state-directory] [environment-file]" >&2
  exit 2
}

fail() {
  echo "local stack lifecycle error: $*" >&2
  exit 1
}

[[ $# -ge 2 && $# -le 4 ]] || usage

readonly action="$1"
readonly binary_path="$2"
readonly expected_command="${binary_path} serve"
readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly compose_file="${repository_root}/configs/docker-compose.local.yml"
readonly tauth_config_file="${repository_root}/configs/tauth.local.yml"
readonly signing_key_marker="GENERATE_ON_FIRST_MAKE_UP"
readonly compose_project="$(
  printf '%s' "${repository_root}" |
    cksum |
    awk '{print "download-your-data-" $1}'
)"

[[ "${action}" == "up" || "${action}" == "down" ]] || usage
[[ "${binary_path}" == /* ]] || fail "binary path must be absolute: ${binary_path}"

if [[ $# -ge 3 ]]; then
  state_directory="$3"
else
  readonly git_directory="$(git -C "${repository_root}" rev-parse --absolute-git-dir)"
  state_directory="${git_directory}/mprlab-local"
fi
readonly state_directory
readonly state_file="${state_directory}/download-your-data-server.state"

if [[ $# -eq 4 ]]; then
  local_environment_path="$4"
else
  local_environment_path="${repository_root}/configs/.env"
fi
readonly local_environment_path

recorded_pid=""
recorded_start=""

is_local_environment_name() {
  case "$1" in
    DOWNLOAD_YOUR_DATA_ADDRESS | \
      DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM | \
      DOWNLOAD_YOUR_DATA_DATA_DIR | \
      DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN | \
      DOWNLOAD_YOUR_DATA_API_ORIGIN | \
      DOWNLOAD_YOUR_DATA_TAUTH_URL | \
      DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID | \
      DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY | \
      DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME | \
      DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME | \
      DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID | \
      DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN | \
      DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL | \
      DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY)
      return 0
      ;;
    *)
      return 1
      ;;
  esac
}

clear_local_environment() {
  unset \
    DOWNLOAD_YOUR_DATA_ADDRESS \
    DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM \
    DOWNLOAD_YOUR_DATA_DATA_DIR \
    DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN \
    DOWNLOAD_YOUR_DATA_API_ORIGIN \
    DOWNLOAD_YOUR_DATA_TAUTH_URL \
    DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID \
    DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY \
    DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME \
    DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME \
    DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID \
    DOWNLOAD_YOUR_DATA_TMDB_READ_TOKEN \
    DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL \
    DOWNLOAD_YOUR_DATA_INFERENCE_BOUNDARY
}

initialize_local_signing_key() {
  local environment_line
  local environment_name
  local environment_value
  local marker_count
  local signing_key
  local temporary_environment

  marker_count="$(
    awk -F= \
      -v expected_name="DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY" \
      -v expected_value="${signing_key_marker}" \
      '
        $1 == expected_name &&
          substr($0, length($1) + 2) == expected_value {
          count += 1
        }
        END { print count + 0 }
      ' \
      "${local_environment_path}"
  )"
  [[ "${marker_count}" -le 1 ]] ||
    fail "${local_environment_path} contains more than one signing-key bootstrap marker"
  [[ "${marker_count}" -eq 1 ]] || return 0

  command -v openssl >/dev/null 2>&1 ||
    fail "openssl is required to initialize the private local signing key"
  signing_key="$(openssl rand -hex 48)"
  [[ "${#signing_key}" -eq 96 ]] ||
    fail "could not initialize the private local signing key"

  umask 077
  temporary_environment="$(mktemp "${local_environment_path}.tmp.XXXXXX")"
  while IFS= read -r environment_line || [[ -n "${environment_line}" ]]; do
    environment_name="${environment_line%%=*}"
    environment_value="${environment_line#*=}"
    if [[ "${environment_name}" == "DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY" &&
      "${environment_value}" == "${signing_key_marker}" ]]; then
      printf '%s=%s\n' "${environment_name}" "${signing_key}" \
        >>"${temporary_environment}"
      continue
    fi
    printf '%s\n' "${environment_line}" >>"${temporary_environment}"
  done <"${local_environment_path}"
  chmod 600 "${temporary_environment}"
  mv "${temporary_environment}" "${local_environment_path}"
}

load_local_environment() {
  local environment_line
  local environment_line_number
  local environment_name
  local environment_value
  local loaded_names
  local required_name

  [[ -f "${local_environment_path}" && ! -L "${local_environment_path}" ]] ||
    fail "missing private local environment at ${local_environment_path}"
  chmod 600 "${local_environment_path}" ||
    fail "could not protect private local environment at ${local_environment_path}"
  initialize_local_signing_key

  clear_local_environment
  environment_line_number=0
  loaded_names=" "
  while IFS= read -r environment_line || [[ -n "${environment_line}" ]]; do
    environment_line_number=$((environment_line_number + 1))
    if [[ "${environment_line}" =~ ^[[:space:]]*$ ||
      "${environment_line}" =~ ^[[:space:]]*# ]]; then
      continue
    fi
    [[ "${environment_line}" == *=* ]] ||
      fail "${local_environment_path}:${environment_line_number} must use NAME=VALUE"
    environment_name="${environment_line%%=*}"
    environment_value="${environment_line#*=}"
    [[ "${environment_name}" =~ ^[A-Z][A-Z0-9_]*$ ]] ||
      fail "${local_environment_path}:${environment_line_number} has an invalid variable name"
    is_local_environment_name "${environment_name}" ||
      fail "${local_environment_path}:${environment_line_number} defines unsupported variable ${environment_name}"
    case "${loaded_names}" in
      *" ${environment_name} "*)
        fail "${local_environment_path}:${environment_line_number} duplicates ${environment_name}"
        ;;
    esac
    [[ -n "${environment_value}" ]] ||
      fail "${local_environment_path}:${environment_line_number} leaves ${environment_name} empty"
    export "${environment_name}=${environment_value}"
    loaded_names="${loaded_names}${environment_name} "
  done <"${local_environment_path}"

  for required_name in \
    DOWNLOAD_YOUR_DATA_ADDRESS \
    DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM \
    DOWNLOAD_YOUR_DATA_DATA_DIR \
    DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN \
    DOWNLOAD_YOUR_DATA_API_ORIGIN \
    DOWNLOAD_YOUR_DATA_TAUTH_URL \
    DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID \
    DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY \
    DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME \
    DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME \
    DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID; do
    case "${loaded_names}" in
      *" ${required_name} "*) ;;
      *)
        fail "${local_environment_path} must define ${required_name}"
        ;;
    esac
  done

  [[ "${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN}" == "${DOWNLOAD_YOUR_DATA_API_ORIGIN}" &&
    "${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN}" == "${DOWNLOAD_YOUR_DATA_TAUTH_URL}" ]] ||
    fail "local public, API, and TAuth origins must use one same-origin front door"
  case "${DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM}" in
    http://*) ;;
    *)
      fail "DOWNLOAD_YOUR_DATA_LOCAL_APP_UPSTREAM must use HTTP inside the local Docker network"
      ;;
  esac
}

require_stack_tools() {
  command -v curl >/dev/null 2>&1 ||
    fail "curl is required to verify local stack readiness"
  command -v docker >/dev/null 2>&1 ||
    fail "Docker is required to run the local TAuth stack"
  docker compose version >/dev/null 2>&1 ||
    fail "Docker Compose is required to run the local TAuth stack"
  docker info --format '{{.ServerVersion}}' >/dev/null 2>&1 ||
    fail "the Docker engine is not available"
  [[ -f "${compose_file}" && ! -L "${compose_file}" ]] ||
    fail "local Compose contract is not a regular file: ${compose_file}"
  [[ -f "${tauth_config_file}" && ! -L "${tauth_config_file}" ]] ||
    fail "local TAuth contract is not a regular file: ${tauth_config_file}"
}

compose_with_profile() {
  docker compose \
    --project-name "${compose_project}" \
    --file "${compose_file}" \
    --env-file "${local_environment_path}" \
    "$@"
}

compose_without_profile() {
  docker compose \
    --project-name "${compose_project}" \
    --file "${compose_file}" \
    --env-file /dev/null \
    "$@"
}

stop_local_dependencies() {
  compose_without_profile down --remove-orphans
}

application_health_url() {
  local address
  local port

  address="${DOWNLOAD_YOUR_DATA_ADDRESS}"
  port="${address##*:}"
  case "${address}" in
    0.0.0.0:*)
      printf 'http://127.0.0.1:%s/api/health\n' "${port}"
      ;;
    127.*:*)
      printf 'http://%s/api/health\n' "${address}"
      ;;
    \[::\]:* | \[::1\]:*)
      printf 'http://[::1]:%s/api/health\n' "${port}"
      ;;
    *)
      fail "DOWNLOAD_YOUR_DATA_ADDRESS must bind loopback or all interfaces for the local gateway"
      ;;
  esac
}

wait_for_application_health() {
  local attempt
  local health_url="$1"

  for attempt in $(seq 1 100); do
    if curl --fail --silent --show-error --max-time 1 "${health_url}" 2>/dev/null |
      grep -F '"status":"ready"' >/dev/null; then
      return 0
    fi
    kill -0 "${child_pid}" >/dev/null 2>&1 ||
      fail "application exited before becoming ready at ${health_url}"
    sleep 0.1
  done
  fail "application did not become ready at ${health_url}"
}

wait_for_front_door() {
  local attempt
  local auth_status
  local browser_config
  local front_ready
  local front_health_url
  local me_status

  front_health_url="${DOWNLOAD_YOUR_DATA_API_ORIGIN%/}/api/health"
  front_ready="false"
  for attempt in $(seq 1 300); do
    if curl --fail --silent --show-error --max-time 1 \
      "${front_health_url}" 2>/dev/null |
      grep -F '"status":"ready"' >/dev/null; then
      front_ready="true"
      break
    fi
    kill -0 "${child_pid}" >/dev/null 2>&1 ||
      fail "application exited before the local front door became ready"
    sleep 0.1
  done
  [[ "${front_ready}" == "true" ]] ||
    fail "local front door did not become ready at ${front_health_url}"

  auth_status="$(
    curl --silent --output /dev/null --write-out '%{http_code}' \
      --max-time 2 \
      --header "Origin: ${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN}" \
      "${DOWNLOAD_YOUR_DATA_TAUTH_URL%/}/auth/session" 2>/dev/null ||
      true
  )"
  [[ "${auth_status}" == "204" ]] ||
    fail "anonymous TAuth session boundary returned HTTP ${auth_status:-unreachable}; expected 204"

  me_status="$(
    curl --silent --output /dev/null --write-out '%{http_code}' \
      --max-time 2 \
      --header "Origin: ${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN}" \
      "${DOWNLOAD_YOUR_DATA_TAUTH_URL%/}/me" 2>/dev/null ||
      true
  )"
  [[ "${me_status}" == "401" ]] ||
    fail "anonymous TAuth profile boundary returned HTTP ${me_status:-unreachable}; expected 401"

  browser_config="$(
    curl --fail --silent --show-error --max-time 2 \
      "${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN%/}/config-ui.yaml"
  )"
  grep -F "${DOWNLOAD_YOUR_DATA_TAUTH_URL}" <<<"${browser_config}" >/dev/null ||
    fail "browser configuration does not expose the same-origin TAuth URL"
  grep -F "${DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID}" <<<"${browser_config}" >/dev/null ||
    fail "browser configuration does not expose the configured Google client ID"
}

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
  if ! {
    IFS= read -r current_pid
    IFS= read -r current_start
  } <"${state_file}" 2>/dev/null; then
    return 0
  fi
  if [[ "${current_pid}" == "$1" && "${current_start}" == "$2" ]]; then
    rm -f "${state_file}"
  fi
}

bring_down() {
  local attempt

  if [[ ! -e "${state_file}" ]]; then
    require_stack_tools
    stop_local_dependencies >/dev/null
    echo "Local stack is not running."
    return 0
  fi
  [[ -f "${state_file}" && ! -L "${state_file}" ]] ||
    fail "ownership state is not a regular file: ${state_file}"

  read_state
  if ! kill -0 "${recorded_pid}" >/dev/null 2>&1; then
    remove_owned_state "${recorded_pid}" "${recorded_start}"
    require_stack_tools
    stop_local_dependencies >/dev/null
    echo "Local stack is not running; removed its stale ownership state."
    return 0
  fi

  state_matches_process ||
    fail "PID ${recorded_pid} does not match the application started by make up; no process was stopped"

  require_stack_tools
  kill -TERM "${recorded_pid}"
  for attempt in $(seq 1 50); do
    if ! kill -0 "${recorded_pid}" >/dev/null 2>&1; then
      remove_owned_state "${recorded_pid}" "${recorded_start}"
      stop_local_dependencies >/dev/null
      echo "Local stack stopped."
      return 0
    fi
    sleep 0.1
  done

  fail "application PID ${recorded_pid} did not stop after SIGTERM"
}

bring_up() {
  local backend_health_url
  local child_pid
  local child_start
  local stack_started
  local state_temp
  local wait_status

  load_local_environment
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

  require_stack_tools

  child_pid=""
  child_start=""
  stack_started="false"
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
    if [[ "${stack_started}" == "true" ]]; then
      if ! stop_local_dependencies >/dev/null 2>&1; then
        echo "local stack lifecycle error: could not stop checkout-owned Docker dependencies" >&2
      fi
    fi
    return 0
  }
  trap cleanup EXIT
  trap 'exit 129' HUP
  trap 'exit 130' INT
  trap 'exit 143' TERM

  compose_with_profile config --quiet
  stack_started="true"
  compose_with_profile up --detach --wait tauth

  "${binary_path}" serve &
  child_pid="$!"
  child_start="$(process_start "${child_pid}")"
  [[ -n "${child_start}" ]] ||
    fail "server exited before ownership could be recorded"

  printf '%s\n%s\n' "${child_pid}" "${child_start}" >"${state_temp}"
  chmod 600 "${state_temp}"
  mv "${state_temp}" "${state_file}"

  backend_health_url="$(application_health_url)"
  wait_for_application_health "${backend_health_url}"
  compose_with_profile up --detach --no-deps gateway
  wait_for_front_door

  echo "Local stack ready at ${DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN} as application PID ${child_pid}; TAuth and the same-origin gateway are healthy. Use make down from this checkout to stop it."

  set +e
  wait "${child_pid}"
  wait_status="$?"
  set -e

  trap - EXIT HUP INT TERM
  cleanup

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

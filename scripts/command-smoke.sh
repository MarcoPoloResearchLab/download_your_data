#!/usr/bin/env bash
set -euo pipefail

binary_path="${1:-./build/download-your-data}"
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
working_directory="$(mktemp -d)"
export DOWNLOAD_YOUR_DATA_DATA_DIR="${working_directory}/data"
server_pid=""
cleanup() {
  if [[ -n "${server_pid}" ]]; then
    kill "${server_pid}" >/dev/null 2>&1 || true
    wait "${server_pid}" >/dev/null 2>&1 || true
  fi
  rm -rf "${working_directory}"
}
trap cleanup EXIT

"${binary_path}" inspect "${repository_root}/testdata/synthetic-openai-export.zip"
"${binary_path}" import \
  "${repository_root}/testdata/synthetic-openai-export.zip"
"${binary_path}" status
"${binary_path}" definitions \
  --semantic=false \
  --since 2026-01-01 \
  --until 2026-12-31 \
  --output "reports/definitions.csv"

grep -q "incredulous" "${DOWNLOAD_YOUR_DATA_DATA_DIR}/reports/definitions.csv"
grep -q "stoppage time" "${DOWNLOAD_YOUR_DATA_DATA_DIR}/reports/definitions.csv"
grep -q "berth" "${DOWNLOAD_YOUR_DATA_DATA_DIR}/reports/definitions.csv"

embedding_port="$((18900 + RANDOM % 500))"
go run "${repository_root}/scripts/fake-embedding-server.go" \
  --port "${embedding_port}" \
  >"${working_directory}/fake-embedding-server.log" 2>&1 &
server_pid="$!"
for _ in {1..50}; do
  if curl --silent --fail "http://127.0.0.1:${embedding_port}/health" >/dev/null; then
    break
  fi
  sleep 0.1
done
curl --silent --fail "http://127.0.0.1:${embedding_port}/health" >/dev/null

export DOWNLOAD_YOUR_DATA_INFERENCE_BASE_URL="http://127.0.0.1:${embedding_port}/v1"
"${binary_path}" index build \
  --provider smoke \
  --model smoke-embedder \
  --dimensions 3 \
  --batch-size 2
"${binary_path}" search \
  --query berth \
  --output "reports/berth-search.json"

grep -q "berth" "${DOWNLOAD_YOUR_DATA_DATA_DIR}/reports/berth-search.json"
grep -q '"query": "berth"' "${DOWNLOAD_YOUR_DATA_DATA_DIR}/reports/berth-search-audit.json"

kill "${server_pid}" >/dev/null 2>&1 || true
wait "${server_pid}" >/dev/null 2>&1 || true
server_pid=""

application_port="$((19400 + RANDOM % 500))"
DOWNLOAD_YOUR_DATA_ADDRESS="127.0.0.1:${application_port}" \
  "${binary_path}" serve >"${working_directory}/application.log" 2>&1 &
server_pid="$!"
for _ in {1..50}; do
  if curl --silent --fail "http://127.0.0.1:${application_port}/api/health" >/dev/null; then
    break
  fi
  if ! kill -0 "${server_pid}" >/dev/null 2>&1; then
    sed -n '1,120p' "${working_directory}/application.log" >&2
    exit 1
  fi
  sleep 0.1
done
curl --silent --fail "http://127.0.0.1:${application_port}/api/health" |
  grep -q '"status":"ready"'

echo "Product command smoke test passed."

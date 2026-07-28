#!/usr/bin/env bash
set -euo pipefail

binary_path="${1:-./build/download-your-data-archive}"
repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
working_directory="$(mktemp -d)"
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
  --db "${working_directory}/archive.db" \
  "${repository_root}/testdata/synthetic-openai-export.zip"
"${binary_path}" status --db "${working_directory}/archive.db"
"${binary_path}" definitions \
  --db "${working_directory}/archive.db" \
  --semantic=false \
  --since 2026-01-01 \
  --until 2026-12-31 \
  --output "${working_directory}/definitions.csv"

grep -q "incredulous" "${working_directory}/definitions.csv"
grep -q "stoppage time" "${working_directory}/definitions.csv"
grep -q "berth" "${working_directory}/definitions.csv"

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

"${binary_path}" index build \
  --db "${working_directory}/archive.db" \
  --provider smoke \
  --model smoke-embedder \
  --dimensions 3 \
  --base-url "http://127.0.0.1:${embedding_port}/v1" \
  --batch-size 2
"${binary_path}" search \
  --db "${working_directory}/archive.db" \
  --query berth \
  --mode lexical \
  --output "${working_directory}/berth-search.json"

grep -q "berth" "${working_directory}/berth-search.json"
grep -q '"query": "berth"' "${working_directory}/berth-search-audit.json"
echo "Smoke test passed."

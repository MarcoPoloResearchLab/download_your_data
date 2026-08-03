#!/usr/bin/env bash
set -euo pipefail

readonly repository_root="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
readonly working_directory="$(mktemp -d -t download-your-data-production-artifacts.XXXXXX)"
readonly pages_output="${working_directory}/pages"
readonly image_tag="download-your-data-production-ci:local"
readonly container_name="download-your-data-production-ci-$$"
readonly container_data="/var/lib/download-your-data"

container_started=false

cleanup() {
  if [[ "${container_started}" == true ]]; then
    docker stop "${container_name}" >/dev/null
  fi
  rm -rf "${working_directory}"
}
trap cleanup EXIT

docker buildx build \
  --platform linux/amd64 \
  --target pages \
  --output "type=local,dest=${pages_output}" \
  "${repository_root}"

for required_file in \
  index.html \
  config-ui.yaml \
  robots.txt \
  sitemap.xml \
  resources/index.html \
  application/app.js \
  application/auth-lifecycle.js \
  styles/application.css; do
  [[ -f "${pages_output}/${required_file}" ]] || {
    echo "Pages artifact is missing ${required_file}" >&2
    exit 1
  }
done

grep -F 'https://dyd.mprlab.com' "${pages_output}/index.html" >/dev/null
grep -F 'https://dyd-api.mprlab.com' "${pages_output}/index.html" >/dev/null
grep -F 'http-equiv="Content-Security-Policy"' \
  "${pages_output}/index.html" >/dev/null
if grep -F 'frame-ancestors' "${pages_output}/index.html" >/dev/null; then
  echo 'Pages artifact uses a response-header-only CSP directive' >&2
  exit 1
fi
grep -F 'tauthUrl: https://dyd-api.mprlab.com' \
  "${pages_output}/config-ui.yaml" >/dev/null
grep -F 'tenantId: download-your-data' \
  "${pages_output}/config-ui.yaml" >/dev/null

if grep -R -F \
  -e '__DOWNLOAD_YOUR_DATA_API_ORIGIN__' \
  -e '__DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN__' \
  -e '__DOWNLOAD_YOUR_DATA_CONTENT_SECURITY_POLICY__' \
  "${pages_output}" >/dev/null; then
  echo 'Pages artifact contains an unresolved production marker' >&2
  exit 1
fi
if find "${pages_output}" -type f \
  \( -name '.env' -o -name 'production.yml' -o -name 'resources.yml' \) \
  | grep -q .; then
  echo 'Pages artifact contains a deployment-only input' >&2
  exit 1
fi

docker buildx build \
  --platform linux/amd64 \
  --target api \
  --load \
  --tag "${image_tag}" \
  "${repository_root}"

docker run \
  --detach \
  --rm \
  --name "${container_name}" \
  --platform linux/amd64 \
  --read-only \
  --tmpfs /tmp:rw,noexec,nosuid,size=16m \
  --tmpfs "${container_data}:rw,noexec,nosuid,uid=65532,gid=65532,mode=0700,size=64m" \
  --publish 127.0.0.1::8787 \
  --env DOWNLOAD_YOUR_DATA_ADDRESS=0.0.0.0:8787 \
  --env DOWNLOAD_YOUR_DATA_API_ORIGIN=https://dyd-api.mprlab.com \
  --env "DOWNLOAD_YOUR_DATA_DATA_DIR=${container_data}" \
  --env DOWNLOAD_YOUR_DATA_GOOGLE_CLIENT_ID=283383931996-582q1pholigban5bueqfq4g470hlrfpf.apps.googleusercontent.com \
  --env DOWNLOAD_YOUR_DATA_PUBLIC_ORIGIN=https://dyd.mprlab.com \
  --env DOWNLOAD_YOUR_DATA_TAUTH_JWT_SIGNING_KEY=production-artifact-test-signing-key-0123456789abcdef \
  --env DOWNLOAD_YOUR_DATA_TAUTH_REFRESH_COOKIE_NAME=download_your_data_refresh \
  --env DOWNLOAD_YOUR_DATA_TAUTH_SESSION_COOKIE_NAME=download_your_data_session \
  --env DOWNLOAD_YOUR_DATA_TAUTH_TENANT_ID=download-your-data \
  --env DOWNLOAD_YOUR_DATA_TAUTH_URL=https://dyd-api.mprlab.com \
  "${image_tag}" >/dev/null
container_started=true

readonly published_address="$(docker port "${container_name}" 8787/tcp)"
readonly health_url="http://${published_address}/api/health"
health_ready=false
for _ in $(seq 1 100); do
  if curl --fail --silent "${health_url}" \
    | grep -F '"status":"ready"' >/dev/null; then
    health_ready=true
    break
  fi
  if ! docker inspect --format '{{.State.Running}}' "${container_name}" \
    | grep -Fqx true; then
    docker logs "${container_name}" >&2
    exit 1
  fi
  sleep 0.1
done
if [[ "${health_ready}" != true ]]; then
  docker logs "${container_name}" >&2
  echo 'production API container did not become healthy' >&2
  exit 1
fi

readonly runtime_user="$(docker inspect --format '{{.Config.User}}' "${container_name}")"
[[ "${runtime_user}" == '65532:65532' ]] || {
  echo "production API container user is ${runtime_user}; want 65532:65532" >&2
  exit 1
}

curl --fail --silent --show-error "${health_url}" >/dev/null

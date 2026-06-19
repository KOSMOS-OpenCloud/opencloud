#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || { echo "ERROR: DIST not found"; exit 1; }

IMAGE="docker.io/flash7777pods/opencloud-kosmos"
TAG="${1:-latest}"

echo "=== Deploy kosmos:${TAG} to ${HOST} ==="

ssh "root@${HOST}" "
  cd /data/opencloud_podman &&
  podman pull ${IMAGE}:${TAG} &&
  sed -i 's|OC_DOCKER_TAG=.*|OC_DOCKER_TAG=${TAG}|' .env &&
  podman compose down opencloud &&
  podman compose up -d opencloud
" 2>&1 | tail -5

echo ""
sleep 10
echo "=== Checking ==="
ssh "root@${HOST}" "podman exec opencloud_full-opencloud-1 /usr/bin/opencloud version 2>/dev/null" | head -3
echo ""
curl -s -k -w "HTTP %{http_code}\n" "https://${HOST}/" 2>/dev/null | tail -1

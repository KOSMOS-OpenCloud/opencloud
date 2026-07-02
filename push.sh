#!/bin/bash
set -euo pipefail

# Push the built OpenCloud image to Codeberg registry.
# Called by build_pod executor after build_kosmos.sh.
# Requires PUSH_TOKEN and TAG env vars.

IMAGE="codeberg.org/kosmos-opencloud/opencloud-kosmos"
TAG="${TAG:?TAG required}"
CREDS="flash7777:${PUSH_TOKEN:?PUSH_TOKEN required}"

PUSH="podman push"
command -v podman &>/dev/null || PUSH="buildah push"

echo "=== Push ${IMAGE}:${TAG} ==="
$PUSH --creds="${CREDS}" "${IMAGE}:${TAG}"
echo "=== Pushed ${IMAGE}:${TAG} ==="

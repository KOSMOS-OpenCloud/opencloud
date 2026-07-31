#!/bin/bash
set -euo pipefail

# Push the built OpenCloud image to container registry.
# All config from DIST: PUSH_REGISTRY, PUSH_NS, PUSH_TOKEN, APP.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/DIST" ] && . "$SCRIPT_DIR/DIST"

TAG="${TAG:?TAG required}"
REGISTRY="${PUSH_REGISTRY:?PUSH_REGISTRY required in DIST}"
NS="${PUSH_NS:?PUSH_NS required in DIST}"
APP="${APP:?APP required in DIST}"
PUSH_TOKEN="${PUSH_TOKEN:?PUSH_TOKEN required in DIST}"

IMAGE="${REGISTRY}/${NS}/${APP}"
CREDS="flash7777:${PUSH_TOKEN}"

PUSH="podman push"
command -v podman &>/dev/null || PUSH="buildah push"

# Re-tag if build used a different image name
BUILD_IMAGE="codeberg.org/kosmos-opencloud/${APP}"
if [ "$IMAGE" != "$BUILD_IMAGE" ]; then
    TAG_CMD="podman tag"
    command -v podman &>/dev/null || TAG_CMD="buildah tag"
    $TAG_CMD "${BUILD_IMAGE}:${TAG}" "${IMAGE}:${TAG}" 2>/dev/null || true
fi

echo "=== Push ${IMAGE}:${TAG} ==="
$PUSH --creds="${CREDS}" "${IMAGE}:${TAG}"
echo "=== Pushed ${IMAGE}:${TAG} ==="

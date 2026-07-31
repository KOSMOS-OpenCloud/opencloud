#!/bin/bash
set -euo pipefail

# Push the built OpenCloud image to container registry.
# All config via ENV (from DIST → job.py → worker): PUSH_REGISTRY, PUSH_NS, PUSH_TOKEN, APP.

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
[ -f "$SCRIPT_DIR/DIST" ] && . "$SCRIPT_DIR/DIST"

TAG="${TAG:?TAG required}"
REGISTRY="${PUSH_REGISTRY:?PUSH_REGISTRY required}"
NS="${PUSH_NS:?PUSH_NS required}"
APP="${APP:?APP required}"
PUSH_TOKEN="${PUSH_TOKEN:?PUSH_TOKEN required}"
PUSH_USER="${PUSH_USER:-flash7777}"

IMAGE="${REGISTRY}/${NS}/${APP}"

PUSH="podman push"
LOGIN="podman login"
TAG_CMD="podman tag"
if ! command -v podman &>/dev/null; then
    PUSH="buildah push"
    LOGIN="buildah login"
    TAG_CMD="buildah tag"
fi

# Login to registry
echo "${PUSH_TOKEN}" | $LOGIN -u "${PUSH_USER}" --password-stdin "${REGISTRY}"

# Re-tag if build used a different image name
BUILD_IMAGE="${REGISTRY}/${NS}/${APP}"
# Check common alternative names from older builds
for ALT in "codeberg.org/kosmos-opencloud/${APP}" "ghcr.io/kosmos-opencloud/${APP}"; do
    if [ "$ALT" != "$IMAGE" ] && $TAG_CMD "${ALT}:${TAG}" "${IMAGE}:${TAG}" 2>/dev/null; then
        break
    fi
done

echo "=== Push ${IMAGE}:${TAG} ==="
$PUSH "${IMAGE}:${TAG}"
echo "=== Pushed ${IMAGE}:${TAG} ==="

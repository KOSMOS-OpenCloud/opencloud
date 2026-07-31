#!/bin/bash
set -euo pipefail

# Push the built OpenCloud image to container registry.
# Called by build_pod executor after build_kosmos.sh.
# Registry determined by GIT_BASE (github → ghcr.io, codeberg → codeberg.org).

TAG="${TAG:?TAG required}"
PUSH_TOKEN="${PUSH_TOKEN:?PUSH_TOKEN required}"

if [[ "${GIT_BASE:-}" == *github.com* ]]; then
    IMAGE="ghcr.io/kosmos-opencloud/opencloud-kosmos"
    CREDS="flash7777:${PUSH_TOKEN}"
else
    IMAGE="codeberg.org/kosmos-opencloud/opencloud-kosmos"
    CREDS="flash7777:${PUSH_TOKEN}"
fi

PUSH="podman push"
command -v podman &>/dev/null || PUSH="buildah push"

# Tag from build name to push name (build uses codeberg.org/... as IMAGE)
BUILD_IMAGE="codeberg.org/kosmos-opencloud/opencloud-kosmos"
if [ "$IMAGE" != "$BUILD_IMAGE" ]; then
    TAG_CMD="podman tag"
    command -v podman &>/dev/null || TAG_CMD="buildah tag"
    $TAG_CMD "${BUILD_IMAGE}:${TAG}" "${IMAGE}:${TAG}"
fi

echo "=== Push ${IMAGE}:${TAG} ==="
$PUSH --creds="${CREDS}" "${IMAGE}:${TAG}"
echo "=== Pushed ${IMAGE}:${TAG} ==="

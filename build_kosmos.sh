#!/bin/bash
set -euo pipefail

IMAGE="docker.io/flash7777pods/opencloud-kosmos"
TAG="$(date +%Y%m%d-%H%M)"
DOCKERFILE="Dockerfile.test"

echo "=== Build kosmos: ${IMAGE}:${TAG} ==="

# Build with date tag — never overwrite :latest directly
podman build -f "$DOCKERFILE" -t "${IMAGE}:${TAG}" .

echo ""
echo "=== Built: ${IMAGE}:${TAG} ==="
echo ""
echo "Next steps:"
echo "  1. Test locally:  podman run --rm ${IMAGE}:${TAG} version"
echo "  2. Push:          podman push ${IMAGE}:${TAG}"
echo "  3. Promote:       podman tag ${IMAGE}:${TAG} ${IMAGE}:latest && podman push ${IMAGE}:latest"
echo "  4. Deploy:        ./deploy_kosmos.sh"

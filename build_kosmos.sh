#!/bin/bash
set -euo pipefail

IMAGE="codeberg.org/kosmos-eu/opencloud-kosmos"
TAG="$(date +%Y%m%d-%H%M)"
DOCKERFILE="Dockerfile.test"

echo "=== Build kosmos: ${IMAGE}:${TAG} ==="

# Verify kosmos branches are active in all repos
REVA_DIR="/data/source/gitapps/opencloud_reva"
WEB_DIR="/data/source/gitapps/opencloud_web"

REVA_BRANCH="$(cd "$REVA_DIR" && git branch --show-current 2>/dev/null || echo '?')"
WEB_BRANCH="$(cd "$WEB_DIR" && git branch --show-current 2>/dev/null || echo '?')"

echo "  reva branch:  ${REVA_BRANCH}"
echo "  web branch:   ${WEB_BRANCH}"

if [ "$REVA_BRANCH" != "kosmos" ]; then
    echo "ERROR: opencloud_reva is on '${REVA_BRANCH}', expected 'kosmos'. Aborting."
    exit 1
fi
if [ "$WEB_BRANCH" != "kosmos" ]; then
    echo "ERROR: opencloud_web is on '${WEB_BRANCH}', expected 'kosmos'. Aborting."
    exit 1
fi

# Sync reva-src from opencloud_reva kosmos
echo "  Syncing reva-src from ${REVA_DIR} ..."
rsync -a --delete --exclude='.git' "$REVA_DIR/" reva-src/

# Generate kosmos revision from reva-src git state
REVA_REV="$(cd reva-src && git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
KOSMOS_REV="kosmos-${TAG}-reva-${REVA_REV}"
cat > reva-src/internal/http/services/owncloud/ocdav/kosmos_revision.go <<GOEOF
package ocdav

import "fmt"

const KosmosRevision = "${KOSMOS_REV}"

func init() {
	fmt.Println("reva " + KosmosRevision)
}
GOEOF
echo "  Revision: ${KOSMOS_REV}"

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

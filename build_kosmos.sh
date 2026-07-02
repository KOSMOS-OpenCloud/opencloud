#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="codeberg.org/kosmos-opencloud/opencloud-kosmos"
TAG="${TAG:-$(date +%Y%m%d-%H%M)}"
DOCKERFILE="Dockerfile.test"

# Override expected branch (default: kosmos)
EXPECT_BRANCH="${BRANCH:-kosmos}"
GIT_BASE="${GIT_BASE:-https://codeberg.org/kosmos-opencloud}"

echo "=== Build kosmos: ${IMAGE}:${TAG} (branch: ${EXPECT_BRANCH}) ==="

# Repo directories — use local if available, clone from git otherwise
REVA_DIR="${SCRIPT_DIR}/../opencloud_reva"
WEB_DIR="${SCRIPT_DIR}/../opencloud_web"
CS3_DIR="${SCRIPT_DIR}/go-cs3apis-src"

# Clone repos if not present (build-worker mode)
echo "=== Stage: clone ==="
if [ ! -d "$REVA_DIR" ]; then
    echo "  Cloning opencloud_reva (kosmos)..."
    git clone --depth 1 -b kosmos "${GIT_BASE}/opencloud_reva.git" "$REVA_DIR" 2>&1 | tail -2
fi

if [ ! -d "$WEB_DIR" ]; then
    echo "  Cloning opencloud_web (${EXPECT_BRANCH})..."
    git clone --depth 1 -b "${EXPECT_BRANCH}" "${GIT_BASE}/opencloud_web.git" "$WEB_DIR" 2>/dev/null || \
    git clone --depth 1 -b kosmos "${GIT_BASE}/opencloud_web.git" "$WEB_DIR" 2>&1 | tail -2
fi

if [ ! -d "$CS3_DIR" ] || [ ! -f "$CS3_DIR/go.mod" ]; then
    echo "  Cloning go-cs3apis..."
    rm -rf "$CS3_DIR"
    git clone --depth 1 -b feat/gateway-immutable "https://github.com/flash7777/go-cs3apis.git" "$CS3_DIR" 2>&1 | tail -2
fi

OC_BRANCH="$(git branch --show-current 2>/dev/null || echo '?')"
REVA_BRANCH="$(cd "$REVA_DIR" && git branch --show-current 2>/dev/null || echo '?')"
WEB_BRANCH="$(cd "$WEB_DIR" && git branch --show-current 2>/dev/null || echo '?')"

echo "  opencloud branch: ${OC_BRANCH}"
echo "  reva branch:      ${REVA_BRANCH}"
echo "  web branch:       ${WEB_BRANCH}"

# Sync reva-src
echo "=== Stage: prepare ==="
echo "  Syncing reva-src from ${REVA_DIR} ..."
rsync -a --delete --exclude='.git' "$REVA_DIR/" reva-src/

# Build web-dist
echo "=== Stage: build-web ==="
echo "  Building web-dist from ${WEB_DIR} ..."
"$SCRIPT_DIR/build_web.sh" build

# Generate kosmos revision
OC_REV="$(git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
REVA_REV="$(cd "$REVA_DIR" && git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
WEB_REV="$(cd "$WEB_DIR" && git rev-parse --short HEAD 2>/dev/null || echo 'unknown')"
KOSMOS_REV="kosmos-${TAG}-oc-${OC_REV}-reva-${REVA_REV}-web-${WEB_REV}"
cat > reva-src/internal/http/services/owncloud/ocdav/kosmos_revision.go <<GOEOF
package ocdav

import "fmt"

const KosmosRevision = "${KOSMOS_REV}"

func init() {
	fmt.Println("reva " + KosmosRevision)
}
GOEOF
echo "  Revision: ${KOSMOS_REV}"

# Build container image
echo "=== Stage: build-image ==="
TMPDIR=${TMPDIR:-/tmp} podman build --network=host --security-opt label=disable -f "$DOCKERFILE" -t "${IMAGE}:${TAG}" .

echo ""
echo "=== Built: ${IMAGE}:${TAG} ==="
echo ""
echo "Next steps:"
echo "  1. Test locally:  podman run --rm ${IMAGE}:${TAG} version"
echo "  2. Push:          podman push ${IMAGE}:${TAG}"
echo "  3. Promote:       podman tag ${IMAGE}:${TAG} ${IMAGE}:latest && podman push ${IMAGE}:latest"
echo "  4. Deploy:        ./deploy_kosmos.sh"

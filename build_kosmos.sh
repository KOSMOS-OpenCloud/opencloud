#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
IMAGE="codeberg.org/kosmos-opencloud/opencloud-kosmos"
TAG="${TAG:-$(date +%Y%m%d-%H%M)}"
DOCKERFILE="Dockerfile.test"

# Override expected branch (default: kosmos)
EXPECT_BRANCH="${BRANCH:-kosmos}"
GIT_BASE="${GIT_BASE:-https://github.com/KOSMOS-OpenCloud}"

echo "=== Build kosmos: ${IMAGE}:${TAG} (branch: ${EXPECT_BRANCH}) ==="

# Repo directories — use local if available, clone from git otherwise
REVA_DIR="${SCRIPT_DIR}/../opencloud_reva"
WEB_DIR="${SCRIPT_DIR}/../opencloud_web"
CS3_DIR="${SCRIPT_DIR}/go-cs3apis-src"
PIPEWORX_DIR="${SCRIPT_DIR}/../openworks-pipeworx"

# Clone or update repos (build-worker mode)
echo "=== Stage: clone ==="
clone_or_update() {
    local dir="$1" repo="$2" branch="$3" base="${4:-$GIT_BASE}"
    local expected_url="${base}/${repo}.git"
    if [ -d "$dir/.git" ]; then
        local current_url
        current_url="$(cd "$dir" && git remote get-url origin 2>/dev/null || echo '')"
        if [ "$current_url" != "$expected_url" ]; then
            echo "  Remote changed (${current_url} → ${expected_url}), re-cloning..."
            rm -rf "$dir"
            git clone --depth 1 -b "${branch}" "${expected_url}" "$dir" 2>&1 | tail -2
        else
            echo "  Updating ${repo} (${branch})..."
            (cd "$dir" && git fetch origin && git reset --hard "origin/${branch}") 2>&1 | tail -2
        fi
    else
        echo "  Cloning ${repo} (${branch})..."
        rm -rf "$dir"
        git clone --depth 1 -b "${branch}" "${expected_url}" "$dir" 2>&1 | tail -2
    fi
}

clone_or_update "$REVA_DIR" "opencloud_reva" "${EXPECT_BRANCH}"
clone_or_update "$WEB_DIR" "opencloud_web" "${EXPECT_BRANCH}"
clone_or_update "$CS3_DIR" "go-cs3apis" "${EXPECT_BRANCH}"

# Pipeworx — from kosmos-openworks org, main branch
PIPEWORX_GIT="${PIPEWORX_GIT:-https://codeberg.org/kosmos-openworks}"
clone_or_update "$PIPEWORX_DIR" "openworks-pipeworx" "main" "$PIPEWORX_GIT"

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
echo "  Syncing pipeworx-src from ${PIPEWORX_DIR} ..."
rsync -a --delete --exclude='.git' "$PIPEWORX_DIR/" pipeworx-src/

# Build or fetch web-dist
echo "=== Stage: build-web ==="
WEB_ZIP="${WEB_ZIP:-}"
if [ -n "$WEB_ZIP" ]; then
    echo "  Fetching pre-built web-dist: ${WEB_ZIP}"
    rm -rf web-dist && mkdir -p web-dist
    curl -sfL "$WEB_ZIP" -o /tmp/web-dist.zip && unzip -qo /tmp/web-dist.zip -d web-dist/ && rm -f /tmp/web-dist.zip
    echo "  Unpacked: $(find web-dist -type f | wc -l) files"
elif [ -f "$SCRIPT_DIR/build_web.sh" ] && [ -d "$SCRIPT_DIR/../kosmos-cloud-deploy" ]; then
    echo "  Building web-dist from ${WEB_DIR} ..."
    "$SCRIPT_DIR/build_web.sh" build
else
    # Fetch latest from Codeberg Generic Packages
    echo "  Fetching latest web-dist from Codeberg..."
    WEB_PKG_VERSION=$(curl -sf "https://codeberg.org/api/v1/packages/kosmos-opencloud?type=generic&q=opencloud-web" \
        | python3 -c "import json,sys; pkgs=[p for p in json.load(sys.stdin) if p['name']=='opencloud-web']; print(pkgs[0]['version'])" 2>/dev/null)
    if [ -z "$WEB_PKG_VERSION" ]; then
        echo "  ERROR: no opencloud-web package found on Codeberg" >&2; exit 1
    fi
    WEB_ZIP_URL="https://codeberg.org/api/packages/kosmos-opencloud/generic/opencloud-web/${WEB_PKG_VERSION}/opencloud-web.zip"
    echo "  Downloading: ${WEB_ZIP_URL}"
    rm -rf web-dist && mkdir -p web-dist
    curl -sfL "$WEB_ZIP_URL" -o /tmp/web-dist.zip && unzip -qo /tmp/web-dist.zip -d web-dist/ && rm -f /tmp/web-dist.zip
    echo "  Unpacked: $(find web-dist -type f | wc -l) files (version: ${WEB_PKG_VERSION})"
fi

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
if command -v buildah &>/dev/null; then
    TMPDIR=${TMPDIR:-/tmp} buildah bud --network=host --security-opt label=disable -f "$DOCKERFILE" -t "${IMAGE}:${TAG}" .
else
    TMPDIR=${TMPDIR:-/tmp} podman build --network=host --security-opt label=disable -f "$DOCKERFILE" -t "${IMAGE}:${TAG}" .
fi

echo ""
echo "=== Built: ${IMAGE}:${TAG} ==="
echo ""

if [ -n "${PUSH_TOKEN:-}" ]; then
    buildah push --creds="token:${PUSH_TOKEN}" "${IMAGE}:${TAG}"
    buildah tag "${IMAGE}:${TAG}" "${IMAGE}:latest"
    buildah push --creds="token:${PUSH_TOKEN}" "${IMAGE}:latest"
fi

echo "=== Built: ${IMAGE}:${TAG} ==="

#!/bin/bash
set -euo pipefail

# Build only the web UI and deploy to brandis
# without rebuilding the entire OpenCloud image.
#
# Usage:
#   ./build_web.sh              # Build + deploy
#   ./build_web.sh build        # Build only
#   ./build_web.sh deploy       # Deploy only
#   BRANCH=openworks ./build_web.sh build  # Build from feature branch

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || { echo "ERROR: DIST not found"; exit 1; }

WEB_SRC="${SCRIPT_DIR}/../opencloud_web"
WEB_DIST="${SCRIPT_DIR}/web-dist"
CONTAINER="${INSTANCE:-opencloud_full-opencloud-1}"

# Override expected branch (default: kosmos)
EXPECT_BRANCH="${BRANCH:-kosmos}"

if [ ! -d "$WEB_SRC" ]; then
    echo "ERROR: opencloud_web not found at $WEB_SRC"
    exit 1
fi

build_web() {
    WEB_BRANCH="$(cd "$WEB_SRC" && git branch --show-current 2>/dev/null || echo '?')"
    echo "=== Building web UI (branch: ${WEB_BRANCH}, expected: ${EXPECT_BRANCH}) ==="

    if [ "$WEB_BRANCH" != "$EXPECT_BRANCH" ] && [ "$WEB_BRANCH" != "kosmos" ]; then
        echo "ERROR: opencloud_web is on '${WEB_BRANCH}', expected '${EXPECT_BRANCH}' or 'kosmos'. Aborting."
        exit 1
    fi

    # Build in container via Dockerfile (same approach as build_kosmos.sh)
    cat > /tmp/Dockerfile.web << 'DEOF'
FROM quay.io/opencloudeu/nodejs-ci:24
ENV CI=true
COPY opencloud_web/ /build/
WORKDIR /build
RUN pnpm install && pnpm build
DEOF

    # Build context = parent dir containing opencloud_web
    TMPDIR=/data3/tmp podman build --no-cache --security-opt label=disable -f /tmp/Dockerfile.web -t opencloud-web-builder "$(dirname "$WEB_SRC")" 2>&1 | tail -5

    # Extract dist
    rm -rf "$WEB_DIST"
    mkdir -p "$WEB_DIST"
    CID=$(podman create opencloud-web-builder)
    podman cp "$CID:/build/dist/." "$WEB_DIST/"
    podman rm "$CID" > /dev/null

    rm /tmp/Dockerfile.web
    echo ""
    echo "=== Built: $WEB_DIST ==="
    echo "Files: $(find "$WEB_DIST" -type f | wc -l)"
}

deploy_web() {
    echo "=== Deploying web UI to $HOST ==="

    if [ ! -f "$WEB_DIST/index.html" ]; then
        echo "ERROR: web-dist not built. Run: ./build_web.sh build"
        exit 1
    fi

    rsync -az --delete "$WEB_DIST/" "root@${HOST}:/tmp/web-dist-new/"

    ssh "root@${HOST}" "
        podman cp /tmp/web-dist-new/. ${CONTAINER}:/var/lib/opencloud/web/assets/core/
        rm -rf /tmp/web-dist-new
        echo 'Web deployed. Reload browser.'
    "

    echo "=== Done ==="
}

case "${1:-}" in
    build)  build_web ;;
    deploy) deploy_web ;;
    -h|--help|help)
        echo "Usage: $0 [build|deploy]  (default: both)"
        echo "  BRANCH=openworks $0 build  — build from feature branch"
        ;;
    *)      build_web; deploy_web ;;
esac

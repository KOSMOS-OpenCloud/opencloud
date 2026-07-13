#!/bin/bash
set -euo pipefail

# Build OpenCloud Web from opencloud_web repo.
# Called by build-web worker (runs in nodejs-ci:24 container).
#
# With WEB_ZIP env: fetch pre-built web archive (fast, for pod builds)
# Without WEB_ZIP:  clone opencloud_web and build from source

SCRIPT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || true

GIT_BASE="${GIT_BASE:-https://codeberg.org/kosmos-opencloud}"
BRANCH="${BRANCH:-kosmos}"
DIST_OUT="${SCRIPT_DIR}/dist"

WEB_ZIP="${WEB_ZIP:-}"

if [ -n "$WEB_ZIP" ]; then
    echo "=== Fetching pre-built web: ${WEB_ZIP} ==="
    rm -rf "$DIST_OUT" && mkdir -p "$DIST_OUT"
    curl -sfL "$WEB_ZIP" -o /tmp/web-dist.zip
    (cd "$DIST_OUT" && unzip -qo /tmp/web-dist.zip)
    rm -f /tmp/web-dist.zip
    echo "  Unpacked: $(find "$DIST_OUT" -type f | wc -l) files"
else
    echo "=== Building web from opencloud_web (${BRANCH}) ==="

    WEB_DIR="${SCRIPT_DIR}/../opencloud_web"

    # Clone or update
    if [ -d "$WEB_DIR/.git" ]; then
        echo "  Updating opencloud_web..."
        (cd "$WEB_DIR" && git fetch origin && git reset --hard "origin/${BRANCH}") 2>&1 | tail -2
    else
        echo "  Cloning opencloud_web..."
        rm -rf "$WEB_DIR"
        if [ -n "${PUSH_TOKEN:-}" ]; then
            git clone --depth 1 -b "${BRANCH}" "https://token:${PUSH_TOKEN}@${GIT_BASE#https://}/opencloud_web.git" "$WEB_DIR" 2>&1 | tail -2
        else
            git clone --depth 1 -b "${BRANCH}" "${GIT_BASE}/opencloud_web.git" "$WEB_DIR" 2>&1 | tail -2
        fi
    fi

    echo "  Installing dependencies..."
    (cd "$WEB_DIR" && pnpm install)

    echo "  Building..."
    (cd "$WEB_DIR" && pnpm build)

    rm -rf "$DIST_OUT"
    cp -r "$WEB_DIR/dist" "$DIST_OUT"
    echo "  Built: $(find "$DIST_OUT" -type f | wc -l) files"
fi

echo "=== Web build complete ==="

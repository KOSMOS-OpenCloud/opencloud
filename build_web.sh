#!/bin/bash
set -euo pipefail

# Build OpenCloud Web UI via job.py and deploy to brandis.eu.
#
# Usage:
#   ./build_web.sh              # Build + Push + Deploy
#   ./build_web.sh build        # Build + Push only (via job.py)
#   ./build_web.sh deploy       # Deploy only (latest from Codeberg)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
DEPLOY_DIR="$(cd "$SCRIPT_DIR/../kosmos-cloud-deploy" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || true

# Load tokens from kosmos-cloud-deploy/DIST
if [ -f "$DEPLOY_DIR/DIST" ]; then
    eval "$(grep '^OPENWORKS_TOKEN=' "$DEPLOY_DIR/DIST")"
    eval "$(grep '^CODEBERG_TOKEN=' "$DEPLOY_DIR/DIST")"
fi

: "${OPENWORKS_TOKEN:?Set OPENWORKS_TOKEN}"

build_web() {
    echo "=== Building Web UI via job.py build-oc-web ==="
    cd "$DEPLOY_DIR"
    OPENWORKS_TOKEN="$OPENWORKS_TOKEN" \
    OPENWORKS_PUSH="${CODEBERG_TOKEN:-}" \
    python3 job.py build-oc-web
    echo ""
    echo "=== Web built and pushed to Codeberg ==="
}

deploy_web() {
    echo "=== Deploying Web UI ==="
    "$SCRIPT_DIR/deploy_web.sh" "$@"
}

case "${1:-}" in
    build)  build_web ;;
    deploy) shift; deploy_web "$@" ;;
    -h|--help|help)
        echo "Usage: $0 [build|deploy]  (default: both)"
        ;;
    *)      build_web; deploy_web ;;
esac

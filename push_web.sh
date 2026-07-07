#!/bin/bash
set -euo pipefail

# Push the built web-dist/ as ZIP to Codeberg Generic Packages.
#
# Assumes web-dist/ was already built (via build_web.sh build or job.py build-oc-web).
#
# Usage:
#   ./push_web.sh                    # auto-tag with timestamp
#   TAG=v1.0.0 ./push_web.sh        # explicit tag
#
# Requires: CODEBERG_TOKEN env var (or in ~/.codeberg-token)

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
WEB_DIST="$SCRIPT_DIR/web-dist"

REGISTRY="codeberg.org"
OWNER="kosmos-opencloud"
PACKAGE="opencloud-web"
TAG="${TAG:-$(date +%Y%m%d-%H%M)}"

# Token
if [ -z "${CODEBERG_TOKEN:-}" ] && [ -f ~/.codeberg-token ]; then
    CODEBERG_TOKEN="$(cat ~/.codeberg-token)"
fi
: "${CODEBERG_TOKEN:?Set CODEBERG_TOKEN or create ~/.codeberg-token}"

# Verify
if [ ! -f "$WEB_DIST/index.html" ]; then
    echo "ERROR: web-dist/index.html not found. Run: ./build_web.sh build"
    exit 1
fi

echo "=== Push: ${PACKAGE}:${TAG} ==="
echo "  Files: $(find "$WEB_DIST" -type f | wc -l)"

# Create ZIP
TMPZIP="/tmp/${PACKAGE}-${TAG}.zip"
rm -f "$TMPZIP"
(cd "$WEB_DIST" && zip -qr "$TMPZIP" .)
ZIP_SIZE="$(du -h "$TMPZIP" | cut -f1)"
echo "  ZIP: $ZIP_SIZE"

# Push to Codeberg Generic Packages
UPLOAD_URL="https://${REGISTRY}/api/packages/${OWNER}/generic/${PACKAGE}/${TAG}/${PACKAGE}.zip"
echo "  [push] $UPLOAD_URL"
curl -sf -X PUT "$UPLOAD_URL" \
    -H "Authorization: token ${CODEBERG_TOKEN}" \
    --upload-file "$TMPZIP"

rm -f "$TMPZIP"

echo ""
echo "=== Pushed: ${PACKAGE}:${TAG} ==="
echo ""
echo "Deploy with:"
echo "  ./deploy_web.sh --tag ${TAG}"

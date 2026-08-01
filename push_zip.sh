#!/bin/bash
set -euo pipefail

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || true

OWNER="${PUSH_ORG:-kosmos-opencloud}"
PACKAGE="${PACKAGE_NAME:-opencloud-web}"
TAG="${TAG:-$(date +%Y%m%d-%H%M)}"
GIT_BASE="${GIT_BASE:-https://codeberg.org/kosmos-opencloud}"

# Token: PACKAGES_TOKEN > PUSH_TOKEN > CODEBERG_TOKEN > ~/.codeberg-token
TOKEN="${PACKAGES_TOKEN:-${PUSH_TOKEN:-${CODEBERG_TOKEN:-}}}"
if [ -z "$TOKEN" ] && [ -f ~/.codeberg-token ]; then
    TOKEN="$(cat ~/.codeberg-token)"
fi
: "${TOKEN:?Set PACKAGES_TOKEN, PUSH_TOKEN, or CODEBERG_TOKEN}"
# Keep CODEBERG_TOKEN for backwards compat with build_web.sh
CODEBERG_TOKEN="${TOKEN}"

# Build if not already done
if [ -z "${SKIP_BUILD:-}" ]; then
    bash "$SCRIPT_DIR/build_web.sh"
fi

# ZIP
TMPZIP="/tmp/${PACKAGE}-${TAG}.zip"
rm -f "$TMPZIP"
(cd "$SCRIPT_DIR/dist" && zip -qr "$TMPZIP" .)
echo "[zip] $(du -h "$TMPZIP" | cut -f1)"

# Push — detect target from GIT_BASE (GitHub Releases vs Codeberg/Gitea Generic Packages)
case "$GIT_BASE" in
  *github.com*)
    # GitHub Release upload
    GH_REPO="${OWNER}/${REPO:-$(basename "$SCRIPT_DIR")}"
    echo "[push] GitHub Release ${GH_REPO} tag=${TAG}"

    # Create release (ignore if exists)
    curl -sf -X POST "https://api.github.com/repos/${GH_REPO}/releases" \
        -H "Authorization: token ${TOKEN}" \
        -H "Content-Type: application/json" \
        -d "{\"tag_name\":\"${TAG}\",\"name\":\"${PACKAGE} ${TAG}\"}" 2>/dev/null || true

    # Get release upload URL
    UPLOAD_BASE=$(curl -sf "https://api.github.com/repos/${GH_REPO}/releases/tags/${TAG}" \
        -H "Authorization: token ${TOKEN}" \
        | python3 -c "import sys,json; print(json.load(sys.stdin)['upload_url'].split('{')[0])")

    UPLOAD_URL="${UPLOAD_BASE}?name=${PACKAGE}.zip&label=${PACKAGE}-${TAG}.zip"
    echo "[push] $UPLOAD_URL"
    curl -sf -X POST "$UPLOAD_URL" \
        -H "Authorization: token ${TOKEN}" \
        -H "Content-Type: application/zip" \
        --data-binary "@${TMPZIP}"
    ;;
  *)
    # Codeberg / Gitea Generic Packages
    REGISTRY_HOST="${GIT_BASE#https://}"
    REGISTRY_HOST="${REGISTRY_HOST%%/*}"
    UPLOAD_URL="https://${REGISTRY_HOST}/api/packages/${OWNER}/generic/${PACKAGE}/${TAG}/${PACKAGE}.zip"
    echo "[push] $UPLOAD_URL"
    curl -sf -X PUT "$UPLOAD_URL" \
        -H "Authorization: token ${TOKEN}" \
        --upload-file "$TMPZIP"

    # Also push as latest
    LATEST_URL="https://${REGISTRY_HOST}/api/packages/${OWNER}/generic/${PACKAGE}/latest/${PACKAGE}.zip"
    curl -sf -X DELETE "$LATEST_URL" -H "Authorization: token ${TOKEN}" -o /dev/null 2>/dev/null || true
    curl -sf -X PUT "$LATEST_URL" -H "Authorization: token ${TOKEN}" --upload-file "$TMPZIP"
    ;;
esac
echo "=== Pushed: ${PACKAGE}:${TAG} ==="

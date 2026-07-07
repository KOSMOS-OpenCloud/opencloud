#!/bin/bash
set -euo pipefail

# Deploy OpenCloud Web UI from Codeberg Generic Packages.
#
# Downloads the web ZIP from Codeberg and deploys it into the
# running OpenCloud container on the target host.
#
# Usage:
#   ./deploy_web.sh                              # latest
#   ./deploy_web.sh --tag 20260707-2102          # specific version
#   ./deploy_web.sh --host cloud.brandis.eu      # specific host

SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
. "$SCRIPT_DIR/DIST" 2>/dev/null || true

REGISTRY="codeberg.org"
OWNER="kosmos-opencloud"
PACKAGE="opencloud-web"
TAG="latest"
HOST="${HOST:-cloud.brandis.eu}"
CONTAINER="${INSTANCE:-opencloud_full-opencloud-1}"

while [[ $# -gt 0 ]]; do
  case $1 in
    --tag)      TAG="$2"; shift 2 ;;
    --host)     HOST="$2"; shift 2 ;;
    --no-restart) NO_RESTART=1; shift ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

# Resolve "latest" tag
if [ "$TAG" = "latest" ]; then
  echo "[resolve] Fetching latest version of ${PACKAGE}..."

  if [ -z "${CODEBERG_TOKEN:-}" ] && [ -f ~/.codeberg-token ]; then
    CODEBERG_TOKEN="$(cat ~/.codeberg-token)"
  fi

  TAG=$(curl -sf "https://${REGISTRY}/api/v1/packages/${OWNER}?type=generic&q=${PACKAGE}" \
    ${CODEBERG_TOKEN:+-H "Authorization: token ${CODEBERG_TOKEN}"} \
    | python3 -c "
import sys, json
pkgs = [p for p in json.load(sys.stdin) if p['name'] == '${PACKAGE}']
if not pkgs:
    print('NOT_FOUND', file=sys.stderr); sys.exit(1)
print(pkgs[0]['version'])
")
  echo "[resolve] Latest: ${TAG}"
fi

ZIP_URL="https://${REGISTRY}/api/packages/${OWNER}/generic/${PACKAGE}/${TAG}/${PACKAGE}.zip"

echo "=== Deploy ${PACKAGE}:${TAG} ==="
echo "  from: ${ZIP_URL}"
echo "  to:   ${HOST}:${CONTAINER}"
echo ""

ssh "root@${HOST}" bash -s <<REMOTE
set -euo pipefail
TMPDIR=\$(mktemp -d)
trap 'rm -rf \$TMPDIR' EXIT

echo "[download] ${ZIP_URL}"
curl -sfL -o "\$TMPDIR/web.zip" "${ZIP_URL}"

echo "[deploy] -> ${CONTAINER}:/var/lib/opencloud/web/assets/core/"
podman exec ${CONTAINER} rm -rf /var/lib/opencloud/web/assets/core/*
podman cp "\$TMPDIR/web.zip" ${CONTAINER}:/tmp/web.zip
podman exec ${CONTAINER} sh -c "cd /var/lib/opencloud/web/assets/core && unzip -qo /tmp/web.zip && rm /tmp/web.zip"

echo "[done] \$(podman exec ${CONTAINER} find /var/lib/opencloud/web/assets/core -type f | wc -l) files deployed"
REMOTE

if [ -z "${NO_RESTART:-}" ]; then
  echo "[restart] ${CONTAINER}"
  ssh "root@${HOST}" "podman restart ${CONTAINER} 2>&1 | tail -1"
fi

echo ""
echo "=== ${PACKAGE}:${TAG} deployed to ${HOST} ==="

#!/bin/bash
set -uo pipefail

# reorg_space.sh — Ensure folder metadata matches their _type_ viewtype schemas.
#
# Pure WebDAV — no SSH, no podman exec, no direct filesystem access.
# Scans via PROPFIND, reads schemas via GET, sets metadata via PROPPATCH.
#
# Usage:
#   ./reorg_space.sh --space-url <dav-space-url> --user <user> --password <pw>
#   ./reorg_space.sh --space-url "https://host/dav/spaces/STORAGEID\$SPACEID" --user admin --password pw
#   ./reorg_space.sh ... --dry-run

SPACE_URL=""
USER=""
PASSWORD=""
DRY_RUN=false

while [[ $# -gt 0 ]]; do
  case $1 in
    --space-url) SPACE_URL="$2"; shift 2 ;;
    --user)      USER="$2"; shift 2 ;;
    --password)  PASSWORD="$2"; shift 2 ;;
    --dry-run)   DRY_RUN=true; shift ;;
    *) echo "Unknown option: $1"; exit 1 ;;
  esac
done

: "${SPACE_URL:?--space-url required}"
: "${USER:?--user required}"
: "${PASSWORD:?--password required}"

SPACE_URL="${SPACE_URL%/}"
AUTH="${USER}:${PASSWORD}"
TMPDIR=$(mktemp -d)
trap 'rm -rf $TMPDIR' EXIT

echo "=== reorg_space ==="
echo "Space: ${SPACE_URL}"
[ "$DRY_RUN" = true ] && echo "(dry-run mode)"
echo ""

enc() { python3 -c "import urllib.parse,sys; print(urllib.parse.quote(sys.argv[1], safe='/:@\$!'))" "$1"; }

# Step 1: Load schemas from .views/
echo "[scan] Loading schemas from .views/..."
SCHEMAS="$TMPDIR/schemas"
mkdir -p "$SCHEMAS"

curl -s -u "$AUTH" -X PROPFIND -H "Depth: 1" \
  -d '<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><oc:name/></d:prop></d:propfind>' \
  "$(enc "${SPACE_URL}/.views/")" 2>/dev/null | grep -oP '<oc:name>[^<]+</oc:name>' | sed 's/<[^>]*>//g' | \
while read -r name; do
  [[ "$name" == *.viewtype ]] || continue
  type_name="${name%.viewtype}"
  json=$(curl -s -u "$AUTH" "$(enc "${SPACE_URL}/.views/${name}")" 2>/dev/null)
  if echo "$json" | grep -q '"isLeaf"' 2>/dev/null; then
    app=$(echo "$json" | grep -o '"app"[[:space:]]*:[[:space:]]*"[^"]*"' | head -1 | sed 's/.*: *"//;s/"//')
    if [ -n "$app" ]; then
      echo "$app" > "$SCHEMAS/$type_name"
      echo "  ${type_name} → app=${app}"
    fi
  fi
done

schema_count=$(ls "$SCHEMAS" 2>/dev/null | wc -l)
if [ "$schema_count" -eq 0 ]; then
  echo "No schemas with isLeaf+app found."
  echo "=== reorg complete ==="
  exit 0
fi

# Step 2: Recursively scan for _type_* and PROPPATCH where needed
echo ""
echo "[scan] Scanning folders for _type_* markers..."
changes=0

scan_folder() {
  local path="$1"
  local url=$(enc "${SPACE_URL}${path}")

  # PROPFIND Depth 1 with oy.app
  local xml
  xml=$(curl -s -u "$AUTH" -X PROPFIND -H "Depth: 1" \
    -d '<?xml version="1.0"?><d:propfind xmlns:d="DAV:" xmlns:oc="http://owncloud.org/ns"><d:prop><oc:name/><oc:oy.app/></d:prop></d:propfind>' \
    "$url" 2>/dev/null) || return

  # Parse all child names
  local names
  names=$(echo "$xml" | grep -oP '<oc:name>[^<]+</oc:name>' | sed 's/<[^>]*>//g')
  local self_name=$(echo "$names" | head -1)
  local children=$(echo "$names" | tail -n +2)

  # Find _type_* among children
  local type_name=""
  while IFS= read -r n; do
    if [[ "$n" == _type_* && "$n" != "_type_views" ]]; then
      type_name="${n#_type_}"
      break
    fi
  done <<< "$children"

  # If this folder's type has a schema with app, check/set oy.app
  if [ -n "$type_name" ] && [ -f "$SCHEMAS/$type_name" ]; then
    local expected=$(cat "$SCHEMAS/$type_name")
    # Get current oy.app from PROPFIND (look in 200 OK section for this folder)
    local current=$(echo "$xml" | python3 -c "
import sys, re
text = sys.stdin.read()
# First response is self
m = re.search(r'<d:response>(.*?)</d:response>', text, re.S)
if m:
    chunk = m.group(1)
    # oy.app in 200 OK propstat (not 404)
    ok = re.search(r'<d:status>HTTP/1.1 200 OK</d:status>', chunk[:chunk.find('</d:propstat>')+20])
    app = re.search(r'<oy\.app[^>]*>([^<]+)</oy\.app>', chunk)
    if app and ok: print(app.group(1))
" 2>/dev/null)

    if [ "$current" != "$expected" ]; then
      local folder_name=$(basename "$path")
      [ "$path" = "/" ] && folder_name="(root)"
      echo "  SET oy.app=${expected} on ${folder_name} (was: ${current:-<unset>})"
      changes=$((changes+1))
      if [ "$DRY_RUN" != true ]; then
        curl -s -u "$AUTH" -X PROPPATCH \
          -d "<?xml version=\"1.0\"?><d:propertyupdate xmlns:d=\"DAV:\" xmlns:oc=\"http://owncloud.org/ns\"><d:set><d:prop><oc:oy.app>${expected}</oc:oy.app></d:prop></d:set></d:propertyupdate>" \
          "$url" > /dev/null 2>&1
      fi
    fi
  fi

  # Recurse into child folders (skip hidden, Archive, _type_* files)
  while IFS= read -r n; do
    [[ -z "$n" ]] && continue
    [[ "$n" == .* ]] && continue
    [[ "$n" == Archive ]] && continue
    [[ "$n" == _type_* ]] && continue
    # Check if child is a collection (has trailing / in href)
    local enc_name=$(python3 -c "import urllib.parse; print(urllib.parse.quote('$n'))" 2>/dev/null)
    echo "$xml" | grep -q "${enc_name}/" 2>/dev/null && scan_folder "${path%/}/${n}"
  done <<< "$children"
}

scan_folder "/"

echo ""
echo "Changes: ${changes}"
echo "=== reorg complete ==="

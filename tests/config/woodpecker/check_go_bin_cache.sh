#!/usr/bin/env bash

#
# $1 - root path where .bingo resides
# $2 - name of the cache item
#

ROOT_PATH="$1"
if [ -z "$1" ]; then
    ROOT_PATH="/drone/src"
fi
BINGO_DIR="$ROOT_PATH/.bingo"

# generate hash of a .bingo folder
BINGO_HASH=$(cat "$BINGO_DIR"/* | sha256sum | cut -d ' ' -f 1)

URL="$CACHE_ENDPOINT/$CACHE_BUCKET/opencloud/go-bin/$BINGO_HASH/$2"

mc alias set s3 "$MC_HOST" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY"

if mc ls --json s3/"$CACHE_BUCKET"/opencloud/go-bin/"$BINGO_HASH"/$2 | grep "\"status\":\"success\""; then
    echo "[INFO] Go bin cache with has '$BINGO_HASH' exists."
    ENV="BIN_CACHE_FOUND=true\n"
else
    # stored hash of a .bingo folder to '.bingo_hash' file
    echo "$BINGO_HASH" >"$ROOT_PATH/.bingo_hash"
    echo "[INFO] Go bin cache with has '$BINGO_HASH' does not exist."
    ENV="BIN_CACHE_FOUND=false\n"
fi

echo -e $ENV >> .env

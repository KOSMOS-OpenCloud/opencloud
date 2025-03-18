#!/bin/bash
source .woodpecker.env

# if no $1 is supplied end the script
# Can be web, acceptance or e2e
if [ -z "$1" ]; then
  echo "No cache item is supplied."
  exit 1
fi

echo "Checking web version - $WEB_COMMITID in cache"

mc alias set s3 "$MC_HOST" "$AWS_ACCESS_KEY_ID" "$AWS_SECRET_ACCESS_KEY"

if mc ls --json s3/"$CACHE_BUCKET"/opencloud/web-test-runner/"$WEB_COMMITID"/"$1".tar.gz | grep "\"status\":\"success\"";
then
	echo "$1 cache with commit id $WEB_COMMITID already available."
	ENV="WEB_CACHE_FOUND=true\n"
else
	echo "$1 cache with commit id $WEB_COMMITID was not available."
	ENV="WEB_CACHE_FOUND=false\n"
fi

echo -e $ENV >> .woodpecker.env

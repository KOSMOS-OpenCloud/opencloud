#!/bin/bash
CLIENTS=${CLIENTS:-2}
COUNT=${COUNT:-100}
OC_URL=${OC_URL:-https://localhost:9200}
ENDPOINT=${ENDPOINT:-/webdav}
FOLDER=${FOLDER:-c$CLIENTS x i$COUNT files}
USER=${USER:-einstein}
PASSWORD=${PASSWORD:-relativity}
CURL_OPTS=${CURL_OPTS:--k}

curl -X MKCOL "$OC_URL$ENDPOINT/$FOLDER/" -u $USER:$PASSWORD $CURL_OPTS || { echo "could not create collection '$OC_URL$ENDPOINT/$FOLDER/'" >&2; exit 1; }
for c in $(seq 1 $CLIENTS);
do
{
    for i in $(seq 1 $COUNT);
    do
    curl -X PUT -d "$c,$i" "$OC_URL$ENDPOINT/$FOLDER/file c$c i$i.txt" -u $USER:$PASSWORD $CURL_OPTS
    done
} &
done
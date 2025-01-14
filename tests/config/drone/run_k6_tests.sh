#!/bin/sh

set -e

if [ "$1" = "--opencloud-log" ]; then
    sshpass -p "$SSH_OC_PASSWORD" ssh -o StrictHostKeyChecking=no "$SSH_OC_USERNAME@$SSH_OC_REMOTE" "bash ~/scripts/opencloud.sh log"
    exit 0
fi

# start OpenCloud server
sshpass -p "$SSH_OC_PASSWORD" ssh -o StrictHostKeyChecking=no "$SSH_OC_USERNAME@$SSH_OC_REMOTE" \
    "OC_URL=${TEST_SERVER_URL} \
    OC_COMMIT_ID=${DRONE_COMMIT} \
    bash ~/scripts/opencloud.sh start"

# start k6 tests
sshpass -p "$SSH_K6_PASSWORD" ssh -o StrictHostKeyChecking=no "$SSH_K6_USERNAME@$SSH_K6_REMOTE" \
    "TEST_SERVER_URL=${TEST_SERVER_URL} \
    bash ~/scripts/k6-tests.sh"

# stop OpenCloud server
sshpass -p "$SSH_OC_PASSWORD" ssh -o StrictHostKeyChecking=no "$SSH_OC_USERNAME@$SSH_OC_REMOTE" "bash ~/scripts/opencloud.sh stop"

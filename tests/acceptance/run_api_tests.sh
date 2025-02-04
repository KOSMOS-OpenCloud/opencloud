#!/bin/bash

# Set required environment variables
export LOCAL_TEST=true
export START_EMAIL=true
export GRAPH_AVAILABLE_ROLES="b1e2218d-eef8-4d4c-b82d-0f1a1b48f3b5,a8d5fe5e-96e3-418d-825b-534dbdf22b99,fb6c3e19-e378-47e5-b277-9732f9de6e21,58c63c02-1d89-4572-916a-870abc5a1b7d,2d00ce52-1fc2-4dbc-8b95-a73b73395f5a,1c996275-f1c9-4e71-abdf-a42f6495e960,312c0871-5ef7-4b3a-85b6-0e4074c64049"

# LOCAL TEST WITHOUT EXTRA ENVS
TEST_SERVER_URL="https://opencloud-server:9200"
OC_WRAPPER_URL="http://opencloud-server:5200"
EXPECTED_FAILURES_FILE="tests/acceptance/expected-failures-localAPI-on-decomposed-storage.md"
EXPECTED_FAILURES_FILE_FROM_CORE="tests/acceptance/expected-failures-API-on-decomposed-storage.md"

# Start server
make -C tests/acceptance/docker start-server

# Wait until the server responds with HTTP 200
echo "Waiting for server to start..."
for i in {1..60}; do
    response_code=$(curl -sk -u admin:admin "${TEST_SERVER_URL}/graph/v1.0/users/admin" -w "%{http_code}" -o /dev/null)
    
    echo "Attempt $i: Received response code $response_code"  # Debugging line to see the status

    if [ "$response_code" == "200" ]; then
        echo "✅ Server is up and running!"
        break
    fi
    sleep 1
done

if [ "$response_code" != "200" ]; then
    echo "❌ Server is not up after 60 attempts."
    exit 1
fi

# List of suites to run
SUITES=(
    "apiArchiver"
    "coreApiAuth"
    "apiContract"
    "apiCors"
    "apiAsyncUpload"
    "apiDownloads"
    "apiDepthInfinity"
    "apiLocks"
    "apiActivities"
    "apiSettings"
    "apiGraph"
    "apiServiceAvailability"
    "apiGraphUserGroup"
    "apiSpaces"
    "apiSpacesShares"
    "apiSpacesDavOperation"
    "apiSearch1"
    "apiSearch2"
    "apiReshare"
    "apiSharingNg1"
    "apiSharingNg2"
    "apiSharingNgShareInvitation"
    "apiSharingNgLinkSharePermission"
    "apiSharingNgLinkShareRoot"
    "apiAccountsHashDifficulty"
    "apiSearchContent"
    "apiNotification"
)

# List of suites from core
CORE_SUITES=(
    "coreApiAuth"
    "coreApiCapabilities"
    "coreApiFavorites"
    "coreApiMain"
    "coreApiShareCreateSpecialToShares1"
    "coreApiShareCreateSpecialToShares2"
    "coreApiSharees"
    "coreApiShareManagementBasicToShares"
    "coreApiShareManagementToShares"
    "coreApiShareOperationsToShares1"
    "coreApiShareOperationsToShares2"
    "coreApiSharePublicLink1"
    "coreApiSharePublicLink2"
    "coreApiShareUpdateToShares"
    "coreApiTrashbin"
    "coreApiTrashbinRestore"
    "coreApiVersions"
    "coreApiWebdavDelete"
    "coreApiWebdavEtagPropagation1"
    "coreApiWebdavEtagPropagation2"
    "coreApiWebdavMove1"
    "coreApiWebdavMove2"
    "coreApiWebdavOperations"
    "coreApiWebdavPreviews"
    "coreApiWebdavProperties"
    "coreApiWebdavUpload"
    "coreApiWebdavUploadTUS"
)

EXTRA_SUITE=(
    "apiAntivirus"
    "apiOcm"
    "apiCollaboration"
    "apiAuthApp"
    "cliCommands"
)

# Create log directory
LOG_DIR="./suite-logs"
mkdir -p "$LOG_DIR"

SUCCESS_COUNT=0
FAILURE_COUNT=0

for SUITE in "${SUITES[@]}"; do
    echo "=============================================="
    echo "Running suite: $SUITE"
    echo "=============================================="

    LOG_FILE="$LOG_DIR/${SUITE}.log"

    # Run suite
    make test-acceptance-api TEST_SERVER_URL=$TEST_SERVER_URL OC_WRAPPER_URL=$OC_WRAPPER_URL EXPECTED_FAILURES_FILE=$EXPECTED_FAILURES_FILE BEHAT_SUITE=$SUITE > "$LOG_FILE" 2>&1
    
    # Check if suite was successful
    if [ $? -eq 0 ]; then
        echo "✅ Suite $SUITE completed successfully."
        ((SUCCESS_COUNT++))
    else
        echo "❌ Suite $SUITE failed. Check log: $LOG_FILE"
        ((FAILURE_COUNT++))
    fi
done

for SUITE in "${CORE_SUITES[@]}"; do
    echo "=============================================="
    echo "Running suite: $SUITE"
    echo "=============================================="

    LOG_FILE="$LOG_DIR/${SUITE}.log"

    # Run suite
    make test-acceptance-api TEST_SERVER_URL=$TEST_SERVER_URL EXPECTED_FAILURES_FILE=$EXPECTED_FAILURES_FILE_FROM_CORE BEHAT_SUITE=$SUITE > "$LOG_FILE" 2>&1
    
    # Check if suite was successful
    if [ $? -eq 0 ]; then
        echo "✅ Suite $SUITE completed successfully."
        ((SUCCESS_COUNT++))
    else
        echo "❌ Suite $SUITE failed. Check log: $LOG_FILE"
        ((FAILURE_COUNT++))
    fi
done

# Report summary
echo "=============================================="
echo "Test Summary:"
echo "✅ Successful suites: $SUCCESS_COUNT"
echo "❌ Failed suites: $FAILURE_COUNT"
echo "Logs saved in: $LOG_DIR"
echo "=============================================="

#!/bin/bash

# LOCAL TEST WITHOUT EXTRA ENVS
TEST_SERVER_URL="https://localhost:9200"
EXPECTED_FAILURES_FILE="tests/acceptance/expected-failures-localAPI-on-OCIS-storage.md"
EXPECTED_FAILURES_FILE_FROM_CORE="tests/acceptance/expected-failures-API-on-OCIS-storage.md"

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
    make test-acceptance-api TEST_SERVER_URL=$TEST_SERVER_URL EXPECTED_FAILURES_FILE=$EXPECTED_FAILURES_FILE BEHAT_SUITE=$SUITE > "$LOG_FILE" 2>&1
    
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

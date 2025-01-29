#!/bin/bash

# LOCAL TEST WITHOUT EXTRA ENVS
TEST_SERVER_URL="https://opencloud-server:9200"
OC_WRAPPER_URL="http://opencloud-server:5200"
EXPECTED_FAILURES_FILE="tests/acceptance/expected-failures-localAPI-on-decomposed-storage.md"
EXPECTED_FAILURES_FILE_FROM_CORE="tests/acceptance/expected-failures-API-on-decomposed-storage.md"

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

E2E_SUITES=(
    "admin-settings"
    "file-action"
    "journeys"
    "navigation"
    "search"
    "shares"
    "spaces"
    "user-settings"
)

EXTRA_E2E_SUITE="app-providerapp-store,keycloak,ocm,oidc"

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

# Run e2e suites
for SUITE in "${E2E_SUITES[@]}"; do
    echo "=============================================="
    echo "Running e2e suite: $SUITE"
    echo "=============================================="

    LOG_FILE="$LOG_DIR/${SUITE}.log"

    # Run suite
    (
        cd services/web/_web/tests/e2e || exit 1
        BASE_URL_OPEN_CLOUD=$TEST_SERVER_URL RETRY=1 HEADLESS=true ./run-e2e.sh --suites $SUITE > "../../../../../$LOG_FILE" 2>&1
    )
    
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

#!/bin/bash

# Set required environment variables
export LOCAL_TEST=true
export WITH_WRAPPER=false
export GRAPH_AVAILABLE_ROLES="b1e2218d-eef8-4d4c-b82d-0f1a1b48f3b5,a8d5fe5e-96e3-418d-825b-534dbdf22b99,fb6c3e19-e378-47e5-b277-9732f9de6e21,58c63c02-1d89-4572-916a-870abc5a1b7d,2d00ce52-1fc2-4dbc-8b95-a73b73395f5a,1c996275-f1c9-4e71-abdf-a42f6495e960,312c0871-5ef7-4b3a-85b6-0e4074c64049,aa97fe03-7980-45ac-9e50-b325749fd7e6,63e64e19-8d43-42ec-a738-2b6af2610efa"
export OC_PASSWORD_POLICY_BANNED_PASSWORDS_LIST="/drone/src/tests/config/drone/banned-password-list.txt"

TEST_SERVER_URL="https://opencloud-server:9200"

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

# Clone the repository and install dependencies
git clone https://github.com/opencloud-eu/web
cd web || exit 1
pnpm i
echo "Installation complete, moving to tests/e2e directory..."

# Run e2e suites
for SUITE in "${E2E_SUITES[@]}"; do
    echo "=============================================="
    echo "Running e2e suite: $SUITE"
    echo "=============================================="

    LOG_FILE="$LOG_DIR/${SUITE}.log"

    # Run suite
    (
        cd tests/e2e || exit 1
        OC_BASE_URL=$TEST_SERVER_URL RETRY=1 HEADLESS=true PARALLEL=4 ./run-e2e.sh --suites $SUITE > "../../../$LOG_FILE" 2>&1
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

# Cleanup: Remove the cloned web directory
echo "🧹 Cleaning up..."
cd ..
rm -rf web
echo "✅ Cleanup complete."

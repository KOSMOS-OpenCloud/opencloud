#!/bin/bash

#mkdir -p /drone/src/vendor-bin/behat
#cp /tmp/vendor-bin/behat/composer.json /drone/src/vendor-bin/behat/composer.json

git config --global advice.detachedHead false

## CONFIGURE TEST

if [ "$TEST_SOURCE" = "core" ]; then
    export ACCEPTANCE_TEST_TYPE='core-api'
    if [ "$STORAGE_DRIVER" = "decomposed" ]; then
        export OC_REVA_DATA_ROOT=''
        export BEHAT_FILTER_TAGS='~@skipOnOpencloud-decomposed-Storage'
        export EXPECTED_FAILURES_FILE='/drone/src/tests/acceptance/expected-failures-API-on-decomposed-storage.md'
    elif [ "$STORAGE_DRIVER" = "decomposeds3" ]; then
        export BEHAT_FILTER_TAGS='~@skip&&~@skipOnOpencloud-decomposeds3-Storage'
        export OC_REVA_DATA_ROOT=''
    else
        echo "non existing STORAGE selected"
        exit 1
    fi

    unset BEHAT_SUITE

elif [ "$TEST_SOURCE" = "opencloud" ]; then
    if [ "$STORAGE_DRIVER" = "decomposed" ]; then
        export BEHAT_FILTER_TAGS='~@skip&&~@skipOnOpencloud-decomposed-Storage'
        export OC_REVA_DATA_ROOT=''
    elif [ "$STORAGE_DRIVER" = "decomposeds3" ]; then
        export BEHAT_FILTER_TAGS='~@skip&&~@skipOnOpencloud-decomposeds3-Storage'
        export OC_REVA_DATA_ROOT=''
    elif [ "$STORAGE_DRIVER" = "posix" ]; then
        export BEHAT_FILTER_TAGS='~@skip&&~@skipOnOpencloud-posix-Storage'
        export OC_REVA_DATA_ROOT=''
    else
        echo "non existing storage selected"
        exit 1
    fi

    unset DIVIDE_INTO_NUM_PARTS
    unset RUN_PART
else
    echo "non existing TEST_SOURCE selected"
    exit 1
fi

if [ ! -z "$BEHAT_FEATURE" ]; then
    echo "feature selected: " + $BEHAT_FEATURE
    # allow running without filters if its a feature

    unset BEHAT_FILTER_TAGS
    unset DIVIDE_INTO_NUM_PARTS
    unset RUN_PART
    unset EXPECTED_FAILURES_FILE
else
    unset BEHAT_FEATURE
fi

## RUN TEST

if [[ -z "$TEST_SOURCE" ]]; then
    echo "non existing TEST_SOURCE selected"
    exit 1
else
    sleep 10
    make -C $OC_ROOT test-acceptance-api
fi

chmod -R 777 vendor-bin/**/vendor vendor-bin/**/composer.lock tests/acceptance/output

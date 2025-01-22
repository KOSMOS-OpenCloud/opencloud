#!/bin/sh
set -e

# init OpenCloud
opencloud init

if [ "$WITH_WRAPPER" = "true" ]; then
    ocwrapper serve --bin=opencloud
else
    opencloud server
fi

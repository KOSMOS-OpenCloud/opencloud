#!/bin/sh

while true; do
  echo -e "HTTP/1.1 200 OK\n\n$(cat /woodpecker/src/github.com/opencloud-eu/opencloud/tests/config/woodpecker/hosting-discovery.xml)" | nc -l -k -p 8080
done

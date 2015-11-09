#!/bin/sh
docker run --rm \
  -v "$(pwd):/src" \
  -v /var/run/docker.sock:/var/run/docker.sock \
  centurylink/golang-builder docker-registry.made.com/varnish-purge-proxy:3

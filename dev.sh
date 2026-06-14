#!/bin/sh
# Wrapper for running Go toolchain commands via the golang Docker image
#
# Usage:
#   ./dev.sh test ./...
#   ./dev.sh build ./...
#   ./dev.sh mod tidy
#   ./dev.sh run ./cmd/iptv2hdhr -config testdata/sample-config.yaml
set -e

# Only bind the HTTP port for `run` - `test`/`build`/etc. don't serve
# anything and binding it would conflict with a server already running for
# manual verification.
PORT_ARGS=""
if [ "$1" = "run" ]; then
	PORT_ARGS="-p 8080:8080"
fi

exec docker run --rm \
	-v "$PWD":/src \
	-w /src \
	$PORT_ARGS \
	-e GOFLAGS="-mod=mod -buildvcs=false" \
	-e GOCACHE=/src/.gocache \
	golang:1.22 go "$@"

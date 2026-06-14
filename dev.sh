#!/bin/sh
# Wrapper for running Go toolchain commands via the golang Docker image
#
# Usage:
#   ./dev.sh test ./...
#   ./dev.sh build ./...
#   ./dev.sh mod tidy
#   ./dev.sh run ./cmd/iptv2hdhr -config testdata/sample-config.yaml
set -e
exec docker run --rm \
	-v "$PWD":/src \
	-w /src \
	-p 8080:8080 \
	-e GOFLAGS="-mod=mod -buildvcs=false" \
	-e GOCACHE=/src/.gocache \
	golang:1.22 go "$@"

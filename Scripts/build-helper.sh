#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec go build -o resources/darwin/ytreader-helper ./helper

#!/bin/sh
set -eu
cd "$(dirname "$0")/.."
exec "${GO:-go}" build -o resources/darwin/ytreader-helper ./helper

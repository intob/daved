#!/bin/bash
set -e
echo "Building for os $1 arch $2..."
rm commit
git rev-parse HEAD > commit
GOOS=$1 GOARCH=$2 go build -o daved

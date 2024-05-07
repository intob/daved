#!/bin/bash
echo "BUILDING FOR LINUX $1"
rm commit
git rev-parse HEAD > commit
GOARCH=$1 GOOS=linux go build -o daved

#!/bin/sh
set -e
rm -rf manpages
mkdir manpages
go run cmd/sprout/main.go man | gzip -c -9 >manpages/sprout.1.gz
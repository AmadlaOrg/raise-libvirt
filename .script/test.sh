#!/bin/bash
set -euo pipefail

mkdir -p .reports

echo "---> Running tests with coverage"
go test -v -coverprofile=.reports/coverage.out ./... 2>&1 | tee .reports/test-output.txt

echo "---> Coverage summary"
go tool cover -func=.reports/coverage.out

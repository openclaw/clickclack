#!/usr/bin/env bash
set -euo pipefail

min_coverage="${CLICKCLACK_GO_COVERAGE_MIN:-85}"
if ! [[ "$min_coverage" =~ ^[0-9]+([.][0-9]+)?$ ]] ||
	awk -v min="$min_coverage" 'BEGIN { exit !(min < 0 || min > 100) }'; then
	echo "CLICKCLACK_GO_COVERAGE_MIN must be a number between 0 and 100" >&2
	exit 2
fi

go test ./... -coverprofile=coverage.out
# Keep the aggregate gate focused on request/business logic. Storage and upload
# adapters are still exercised by `go test ./...`, but their generated SQL and
# external I/O branches make the total package percentage noisy.
awk 'NR == 1 || (/\/apps\/api\/internal\// && !/\/(store|storedb|uploadstore)\//)' coverage.out > coverage.filtered.out
go tool cover -func=coverage.filtered.out | tee coverage.txt

awk -v min="$min_coverage" '
  BEGIN {
    found = 0
  }
  /^total:/ {
    found = 1
    sub(/%/, "", $3)
    if ($3 + 0 < min + 0) {
      printf "go coverage %.1f%% is below %.1f%%\n", $3 + 0, min + 0 > "/dev/stderr"
      exit 1
    }
  }
  END {
    if (!found) {
      print "coverage total not found" > "/dev/stderr"
      exit 1
    }
  }
' coverage.txt

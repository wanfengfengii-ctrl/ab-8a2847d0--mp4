#!/bin/sh
# One-shot verification for the fMP4 audit service.
# Runs build checks and unit tests, waits for the API to be healthy, then
# exercises it with continuous and broken timelines over HTTP.
# Exit code 0 = everything passed; non-zero = failure.
set -eu

API_URL="${API_URL:-http://api:8080}"

echo "== [1/4] build check =="
go build ./...
go vet ./...

echo "== [2/4] unit tests =="
go test ./... -count=1

echo "== [3/4] waiting for API health at $API_URL =="
i=0
until wget -q -O /dev/null "$API_URL/healthz" 2>/dev/null; do
	i=$((i + 1))
	if [ "$i" -ge 60 ]; then
		echo "ERROR: API did not become healthy within 60s" >&2
		exit 1
	fi
	sleep 1
done
echo "API is healthy"

echo "== [4/4] HTTP smoke tests (continuous + broken timelines) =="
go run ./cmd/smoke -api "$API_URL"

echo "VERIFY OK"

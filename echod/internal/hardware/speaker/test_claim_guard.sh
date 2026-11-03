#!/bin/sh
# Native race regression for the real driver without ALSA/Linux dependencies.
set -eu
here=$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)
module=$(CDPATH= cd -- "$here/../../.." && pwd)
# Stay beneath the module for Go's internal-import rule; remove only this exact directory.
tmp=$(mktemp -d "$module/.claim-guard-08ba6230-XXXXXX")
trap 'rm -rf -- "$tmp"' EXIT HUP INT TERM
cp "$here/driver.go" "$tmp/driver.go"
cp "$here/claim_guard_test.go" "$tmp/claim_guard_test.go"
cp "$here/accepted_claim_guard_test.go" "$tmp/accepted_claim_guard_test.go"
cp "$here/testdata/claim_guard_stubs.go" "$tmp/stubs_test.go"
cd "$module"
go test -race -count=5 "$tmp/driver.go" "$tmp/claim_guard_test.go" "$tmp/accepted_claim_guard_test.go" "$tmp/stubs_test.go"

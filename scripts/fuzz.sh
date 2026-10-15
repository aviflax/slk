#!/bin/sh
# Run every native Go fuzz target in the repo, one after another.
#
#   scripts/fuzz.sh [fuzztime] [package-pattern]
#
# fuzztime defaults to 30s per target; the pattern defaults to ./...
# A failure stops the run and leaves the crashing input under the
# package's testdata/fuzz/<Target>/, where plain `go test` replays it
# from then on. Commit that file with the fix.
set -eu

fuzztime=${1:-30s}
pattern=${2:-./...}

for pkg in $(go list "$pattern"); do
	dir=$(go list -f '{{.Dir}}' "$pkg")
	targets=$(grep -ho '^func Fuzz[A-Za-z0-9_]*' "$dir"/*_test.go 2>/dev/null | sed 's/^func //' | sort -u)
	for target in $targets; do
		echo "== $pkg $target ($fuzztime)"
		go test "$pkg" -run '^$' -fuzz "^$target\$" -fuzztime "$fuzztime"
	done
done

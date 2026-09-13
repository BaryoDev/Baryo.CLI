#!/bin/sh
# Run the gates CI runs, locally, before pushing.
#
# This exists because every contribution to Baryo arrives from a fork, and a fork's
# pull request cannot see anything the maintainer has not already merged. Finding out
# that gofmt objects costs a push, a wait, and a round trip through a reviewer. The
# same check here costs two seconds.
#
# CI is still the authority. This is the same list in the same order, so a clean run
# here means the pipeline has nothing new to tell you.
#
# Usage:
#   sh scripts/ci-local.sh           # everything
#   sh scripts/ci-local.sh --quick   # skip the slow passes (race, cgo, release build)
set -eu

quick=0
for arg in "$@"; do
	case "$arg" in
	--quick) quick=1 ;;
	-h | --help)
		sed -n '2,17p' "$0" | sed 's/^# \{0,1\}//'
		exit 0
		;;
	*)
		echo "unknown argument: $arg" >&2
		exit 2
		;;
	esac
done

cd "$(dirname "$0")/.."

failed=''
step() { printf '\n\033[1m== %s\033[0m\n' "$1"; }
# Records the failure and keeps going. A contributor wants the whole list of what is
# wrong, not the first item: one fix-and-rerun cycle per problem is the thing this
# script is meant to remove.
run() {
	name="$1"
	shift
	if "$@"; then
		return 0
	fi
	failed="$failed
  - $name"
	return 0
}
skip() { printf '   skipped: %s\n' "$1"; }

step "Conflict markers"
run "conflict markers" sh scripts/check-conflict-markers.sh

step "Formatting (gofmt)"
unformatted=$(gofmt -l .)
if [ -n "$unformatted" ]; then
	echo "not formatted:"
	echo "$unformatted" | sed 's/^/  /'
	echo "fix with: gofmt -w ."
	failed="$failed
  - gofmt"
else
	echo "clean"
fi

step "go vet"
run "go vet" go vet ./...

step "Tests (CGO_ENABLED=0)"
run "tests (no cgo)" env CGO_ENABLED=0 go test -count=1 ./...

if [ "$quick" -eq 1 ]; then
	step "Slow passes"
	skip "--quick: cgo tests, race detector, staticcheck, govulncheck, release build"
else
	step "Tests (CGO_ENABLED=1)"
	# The tree-sitter parsers in internal/index have a cgo and a pure-Go path, and the
	# two are separate code. A pass on one says nothing about the other.
	if command -v gcc >/dev/null 2>&1 || command -v cc >/dev/null 2>&1; then
		run "tests (cgo)" env CGO_ENABLED=1 go test -count=1 ./...
	else
		skip "no C compiler found; CI will still run this pass"
	fi

	step "Tests (race detector)"
	run "tests (race)" go test -race -count=1 ./...

	step "staticcheck"
	run "staticcheck" go run honnef.co/go/tools/cmd/staticcheck@v0.7.0 ./...

	step "govulncheck"
	# Pinned to match CI. The version floor matters: govulncheck analyses with the
	# toolchain it was built with, so one older than go.mod's `go` directive refuses every
	# package with "requires newer Go version".
	#
	# A blocked database fetch is reported as a skip, not a failure. "Could not check" and
	# "checked, nothing found" are different answers, and behind a restrictive proxy the
	# second one would be a lie. CI has network access and is the authority here.
	if vulnout=$(go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./... 2>&1); then
		echo "no known vulnerabilities"
	elif printf '%s' "$vulnout" | grep -q "fetching vulnerabilities"; then
		skip "cannot reach vuln.go.dev from here; CI will still run this"
	else
		printf '%s\n' "$vulnout"
		failed="$failed
  - govulncheck"
	fi

	step "Build (CGO_ENABLED=0)"
	run "build" env CGO_ENABLED=0 go build -ldflags "-s -w" -o /tmp/baryo-ci-local .
fi

step "install.sh checksum verification"
run "install.sh test" sh scripts/install_test.sh

step "NOTICE is current"
# Attribution is generated from the modules actually linked in, and the set differs
# between the cgo and pure-Go builds, so both are checked. A released binary is built
# with CGO_ENABLED=0, which is the configuration most likely to be forgotten.
if command -v python3 >/dev/null 2>&1; then
	run "NOTICE" sh scripts/check-notice.sh
else
	skip "python3 not installed; CI will still check this"
fi

if [ -n "$failed" ]; then
	printf '\n\033[1mFailed:\033[0m%s\n' "$failed"
	exit 1
fi

printf '\n\033[1mAll gates passed.\033[0m CI runs the same list.\n'

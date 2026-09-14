#!/bin/sh
# Fail if NOTICE does not match what the generator produces.
#
# Extracted from the CI job so a contributor can run the same check before pushing,
# and so ci-local.sh and the pipeline cannot drift apart.
#
# gen_notice.py resolves dependencies for both cgo settings and merges them, because a
# released binary is built with CGO_ENABLED=0 while a developer machine with a C
# toolchain defaults to cgo on. Attribution generated from one configuration omits
# whatever only the other one links.
set -eu

cd "$(dirname "$0")/.."

go mod download

# mktemp rather than a fixed /tmp path: a predictable name in a shared directory can be
# pre-created as a symlink by another local user, and two concurrent runs would otherwise
# overwrite each other's output and compare against the wrong file.
generated=$(mktemp "${TMPDIR:-/tmp}/NOTICE.generated.XXXXXX")
trap 'rm -f "$generated"' EXIT HUP INT TERM

python3 scripts/gen_notice.py >"$generated"

if diff -u NOTICE "$generated"; then
	echo "NOTICE is current."
	exit 0
fi

echo
echo "NOTICE is stale. Regenerate it with:"
echo "    python3 scripts/gen_notice.py > NOTICE"
exit 1

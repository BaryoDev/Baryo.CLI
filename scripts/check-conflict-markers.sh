#!/bin/sh
# Fail if a conflict marker reached a tracked file.
#
# Go and YAML have compilers and parsers to catch a botched merge. Markdown does not:
# a README or CHANGELOG full of <<<<<<< passes every other gate in this pipeline and
# ships. This is the gate for the files nothing else reads.
#
# Only the opening and closing markers are matched. The middle marker of a conflict is
# seven equals signs, which is also how Markdown underlines a heading, so matching it
# would fail on ordinary prose. A conflict always carries the other two.
#
# git grep does the walking rather than a shell loop over `git ls-files`. That loop
# word-split its input, so a tracked file named "release notes.md" became two paths that do
# not exist and the gate passed over the one file it was meant to read. git grep takes
# filenames from git directly and never goes through the shell.
set -eu

cd "$(dirname "$0")/.."

# This script and the CI job that proves it works both contain the marker strings as data.
# Excluded by pathspec, which keeps the gate honest about its exclusions rather than
# loosening the pattern to avoid matching itself.
matches=$(git grep -nIE '^(<{7}|>{7})( |$)' -- \
	':(exclude)scripts/check-conflict-markers.sh' \
	':(exclude).github/workflows/ci.yml' || true)

if [ -n "$matches" ]; then
	echo "Conflict markers in tracked files:"
	printf '%s\n' "$matches" | sed 's/^/  /'
	echo
	echo "A merge was committed unresolved. Fix the files above and commit again."
	exit 1
fi

echo "No conflict markers in tracked files."

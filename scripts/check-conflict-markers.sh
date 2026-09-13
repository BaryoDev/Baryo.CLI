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
set -eu

# This script and the CI job that proves it works both contain the marker strings as
# data. Listing them here keeps the gate honest about its own exclusions instead of
# loosening the pattern.
excluded='scripts/check-conflict-markers.sh
.github/workflows/ci.yml'

is_excluded() {
	printf '%s\n' "$excluded" | grep -qxF "$1"
}

found=0
for file in $(git ls-files); do
	is_excluded "$file" && continue
	# -I skips binary files. A match anywhere at line start is enough.
	if grep -qIE '^(<{7}|>{7})( |$)' "$file" 2>/dev/null; then
		echo "conflict marker in $file:"
		grep -nIE '^(<{7}|>{7})( |$)' "$file" | sed 's/^/  /'
		found=1
	fi
done

if [ "$found" -ne 0 ]; then
	echo
	echo "A merge was committed unresolved. Fix the files above and commit again."
	exit 1
fi

echo "No conflict markers in tracked files."
